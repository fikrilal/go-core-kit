// Package email sends transactional email through Resend and renders the
// message bodies the auth and users features share.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Queue is the dedicated queue for outbound email.
const Queue = "emails"

// Message is one outbound email.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender sends one message and reports the provider id.
type Sender interface {
	Enabled() bool
	Send(ctx context.Context, message Message) (string, error)
}

// ProviderError names the provider that failed.
type ProviderError struct {
	Provider string
	Status   int
	Detail   string
}

// Error renders the provider and detail.
func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("email: %s send failed with status %d: %s", e.Provider, e.Status, e.Detail)
	}
	return fmt.Sprintf("email: %s send failed: %s", e.Provider, e.Detail)
}

// Options configure the Resend client.
type Options struct {
	APIKey  string
	From    string
	ReplyTo string
	// BaseURL overrides the Resend endpoint; tests point it at a stub.
	BaseURL string
}

// Client is the Resend HTTP client.
type Client struct {
	apiKey  string
	from    string
	replyTo string
	baseURL string
	http    *http.Client
}

// Provider names this adapter in errors.
const Provider = "resend"

// New builds the client. An empty API key or sender leaves it disabled,
// mirroring the reference's configured check.
func New(options Options) *Client {
	baseURL := strings.TrimSpace(options.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}
	return &Client{
		apiKey:  strings.TrimSpace(options.APIKey),
		from:    strings.TrimSpace(options.From),
		replyTo: strings.TrimSpace(options.ReplyTo),
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the API key and sender are set.
func (c *Client) Enabled() bool { return c.apiKey != "" && c.from != "" }

// Send posts the message to Resend and returns the provider id.
func (c *Client) Send(ctx context.Context, message Message) (string, error) {
	if !c.Enabled() {
		return "", &ProviderError{Provider: Provider, Detail: "email is not configured (set RESEND_API_KEY and EMAIL_FROM)"}
	}
	to := strings.TrimSpace(message.To)
	if to == "" {
		return "", &ProviderError{Provider: Provider, Detail: "Email recipient is required"}
	}
	if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.HTML) == "" {
		return "", &ProviderError{Provider: Provider, Detail: "Email body is required"}
	}
	payload := map[string]any{
		"from":    c.from,
		"to":      []string{to},
		"subject": message.Subject,
	}
	if message.Text != "" {
		payload["text"] = message.Text
	}
	if message.HTML != "" {
		payload["html"] = message.HTML
	}
	if c.replyTo != "" {
		payload["reply_to"] = c.replyTo
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("email: marshal message: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/emails", bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("email: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("email: send request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return "", fmt.Errorf("email: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", &ProviderError{Provider: Provider, Status: response.StatusCode, Detail: strings.TrimSpace(string(body))}
	}
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("email: decode response: %w", err)
	}
	if decoded.ID == "" {
		return "", &ProviderError{Provider: Provider, Status: response.StatusCode, Detail: "response is missing the email id"}
	}
	return decoded.ID, nil
}
