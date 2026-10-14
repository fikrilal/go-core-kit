// Package push sends Firebase Cloud Messaging HTTP v1 notifications. It
// exchanges a service-account assertion for an access token (cached until
// shortly before expiry), then posts to the v1 messages endpoint.
package push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// Queue and job names for the push lane.
const (
	Queue   = "push"
	JobSend = "push.send"
)

// Payload is the push.send job payload, scoped to a session.
type Payload struct {
	SessionID    string            `json:"sessionId"`
	Notification *Notification     `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	RequestedAt  string            `json:"requestedAt"`
}

// Notification is the visible part of a push.
type Notification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// Message is one send request.
type Message struct {
	Token        string
	Notification *Notification
	Data         map[string]string
}

// ErrorCode mirrors the reference's failure classes.
type ErrorCode string

// Failure classes.
const (
	CodeNotConfigured ErrorCode = "push/not-configured"
	CodeInvalidToken  ErrorCode = "push/invalid-token"
	CodeSendFailed    ErrorCode = "push/send-failed"
)

// SendError is a provider failure with its retryability and provider code.
type SendError struct {
	Provider     string
	Code         ErrorCode
	ProviderCode string
	Retryable    bool
	Detail       string
}

// Error renders the provider, code, and detail.
func (e *SendError) Error() string {
	detail := e.Detail
	if detail == "" {
		detail = string(e.Code)
	}
	return fmt.Sprintf("push: %s %s: %s", e.Provider, e.Code, detail)
}

// Sender sends one push and reports the provider message id.
type Sender interface {
	Enabled() bool
	SendToToken(ctx context.Context, message Message) (string, error)
}

// Provider names this adapter in errors.
const Provider = "fcm"

// Options configure the FCM client. Credentials accept raw service-account
// JSON, base64 of it, or a path to it.
type Options struct {
	ProjectID   string
	Credentials string
	// BaseURL and TokenURL override the Google endpoints; tests point them at stubs.
	BaseURL  string
	TokenURL string
}

const (
	defaultBaseURL  = "https://fcm.googleapis.com"
	defaultTokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // endpoint URL, not a credential
	messagingScope  = "https://www.googleapis.com/auth/firebase.messaging"
	tokenLeeway     = time.Minute
)

// Client is the FCM HTTP v1 client.
type Client struct {
	options Options
	clock   clock.Clock
	http    *http.Client

	mutex      sync.Mutex
	parsed     *serviceAccount
	access     string
	accessTill time.Time
}

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// New builds the client; a project id and credentials are required for Enabled.
func New(options Options, clk clock.Clock) *Client {
	baseURL := strings.TrimSpace(options.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	tokenURL := strings.TrimSpace(options.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	options.BaseURL = strings.TrimSuffix(baseURL, "/")
	options.TokenURL = tokenURL
	return &Client{options: options, clock: clk, http: &http.Client{Timeout: 15 * time.Second}}
}

// Enabled reports whether the project id and credentials are present.
func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.options.ProjectID) != "" && strings.TrimSpace(c.options.Credentials) != ""
}

// SendToToken sends one message and returns the provider message id.
func (c *Client) SendToToken(ctx context.Context, message Message) (string, error) {
	if !c.Enabled() {
		return "", &SendError{Provider: Provider, Code: CodeNotConfigured, Detail: "push is not configured (set FCM_PROJECT_ID and FCM_SERVICE_ACCOUNT_JSON)"}
	}
	token := strings.TrimSpace(message.Token)
	if token == "" {
		return "", &SendError{Provider: Provider, Code: CodeInvalidToken, Detail: "registration token is required"}
	}
	access, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{"message": map[string]any{"token": token}}
	envelope := body["message"].(map[string]any)
	if message.Notification != nil {
		envelope["notification"] = message.Notification
	}
	if len(message.Data) > 0 {
		envelope["data"] = message.Data
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("push: marshal message: %w", err)
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", c.options.BaseURL, url.PathEscape(c.options.ProjectID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("push: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+access)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("push: send request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return "", fmt.Errorf("push: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", providerError(response.StatusCode, raw)
	}
	var decoded struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("push: decode response: %w", err)
	}
	if decoded.Name == "" {
		return "", &SendError{Provider: Provider, Code: CodeSendFailed, Detail: "response is missing the message name"}
	}
	return decoded.Name[strings.LastIndex(decoded.Name, "/")+1:], nil
}

// accessToken returns a cached access token, minting one when needed.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	now := c.clock.Now()
	if c.access != "" && now.Before(c.accessTill) {
		return c.access, nil
	}
	account, err := c.account()
	if err != nil {
		return "", err
	}
	assertion, err := serviceAssertion(account, now)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	tokenURL := c.options.TokenURL
	if strings.TrimSpace(account.TokenURI) != "" {
		tokenURL = account.TokenURI
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("push: build token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("push: token request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return "", fmt.Errorf("push: read token response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("push: token endpoint status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("push: decode token response: %w", err)
	}
	if decoded.AccessToken == "" {
		return "", fmt.Errorf("push: token response is missing the access token")
	}
	ttl := time.Duration(decoded.ExpiresIn) * time.Second
	if ttl > tokenLeeway {
		ttl -= tokenLeeway
	}
	c.access = decoded.AccessToken
	c.accessTill = now.Add(ttl)
	return c.access, nil
}

// account parses the service-account credentials once.
func (c *Client) account() (*serviceAccount, error) {
	if c.parsed != nil {
		return c.parsed, nil
	}
	raw, err := readCredentials(c.options.Credentials)
	if err != nil {
		return nil, err
	}
	var account serviceAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return nil, fmt.Errorf("push: decode service account: %w", err)
	}
	if account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("push: service account needs client_email and private_key")
	}
	c.parsed = &account
	return c.parsed, nil
}

// readCredentials accepts raw JSON, base64 of it, or a file path.
func readCredentials(credentials string) ([]byte, error) {
	value := strings.TrimSpace(credentials)
	if strings.HasPrefix(value, "{") {
		return []byte(value), nil
	}
	if !strings.ContainsAny(value, "/\\") {
		if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && json.Valid(decoded) {
			return decoded, nil
		}
	}
	raw, err := os.ReadFile(value) //nolint:gosec // path comes from deployment configuration
	if err != nil {
		return nil, fmt.Errorf("push: read credentials: %w", err)
	}
	return raw, nil
}

// serviceAssertion signs the RS256 assertion the token endpoint expects.
func serviceAssertion(account *serviceAccount, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("push: service account key is not PEM")
	}
	key, err := jwk.ParseKey([]byte(account.PrivateKey), jwk.WithPEM(true))
	if err != nil {
		return "", fmt.Errorf("push: parse service account key: %w", err)
	}
	claims := map[string]any{
		"iss":   account.ClientEmail,
		"scope": messagingScope,
		"aud":   account.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	if account.TokenURI == "" {
		claims["aud"] = defaultTokenURL
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("push: marshal assertion: %w", err)
	}
	signed, err := jws.Sign(payload, jws.WithKey(jwa.RS256(), key))
	if err != nil {
		return "", fmt.Errorf("push: sign assertion: %w", err)
	}
	return string(signed), nil
}

// providerError maps a provider failure onto the failure classes.
func providerError(status int, raw []byte) *SendError {
	detail := strings.TrimSpace(string(raw))
	var decoded struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	providerCode := ""
	if err := json.Unmarshal(raw, &decoded); err == nil {
		providerCode = decoded.Error.Status
		if decoded.Error.Message != "" {
			detail = decoded.Error.Message
		}
	}
	failure := &SendError{Provider: Provider, ProviderCode: providerCode, Detail: detail}
	switch {
	case isInvalidToken(providerCode, detail):
		failure.Code = CodeInvalidToken
	case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		failure.Code = CodeSendFailed
		failure.Retryable = true
	default:
		failure.Code = CodeSendFailed
	}
	return failure
}

// isInvalidToken recognizes the unregistered family of failures.
func isInvalidToken(providerCode, detail string) bool {
	switch providerCode {
	case "UNREGISTERED", "INVALID_ARGUMENT", "NOT_FOUND", "SENDER_ID_MISMATCH":
		return true
	}
	lower := strings.ToLower(detail)
	for _, needle := range []string{"not a valid fcm registration token", "requested entity was not found", "registration token is not valid"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
