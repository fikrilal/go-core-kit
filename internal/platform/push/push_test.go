package push_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
)

var moment = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// stub is a fake Google: one token endpoint and one messaging endpoint.
type stub struct {
	*httptest.Server
	tokenCalls int
	sendCalls  int
	lastAuth   string
	lastBody   map[string]any
	sendStatus int
	sendBody   string
}

func newStub(t *testing.T) *stub {
	t.Helper()
	fake := &stub{sendStatus: http.StatusOK, sendBody: `{"name":"projects/demo/messages/message-1"}`}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			fake.tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
			}
			if parts := strings.Split(r.PostForm.Get("assertion"), "."); len(parts) != 3 {
				t.Errorf("assertion = %q, want a signed JWT", r.PostForm.Get("assertion"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-1","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/messages:send"):
			fake.sendCalls++
			fake.lastAuth = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &fake.lastBody); err != nil {
				t.Errorf("decode send body: %v", err)
			}
			w.WriteHeader(fake.sendStatus)
			_, _ = w.Write([]byte(fake.sendBody))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// serviceAccountJSON builds a credentials document with a fresh RSA key.
func serviceAccountJSON(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key)})
	document, err := json.Marshal(map[string]string{
		"client_email": "worker@demo.iam.gserviceaccount.com",
		"private_key":  string(pemKey),
		"token_uri":    tokenURL,
	})
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	return string(document)
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return encoded
}

func newClient(t *testing.T, fake *stub, credentials string) *push.Client {
	t.Helper()
	return push.New(push.Options{
		ProjectID:   "demo",
		Credentials: credentials,
		BaseURL:     fake.URL,
		TokenURL:    fake.URL + "/token",
	}, clock.Fixed{Moment: moment})
}

func TestSendToTokenCachesAccessToken(t *testing.T) {
	fake := newStub(t)
	client := newClient(t, fake, serviceAccountJSON(t, fake.URL+"/token"))
	if !client.Enabled() {
		t.Fatal("Enabled() = false with credentials")
	}

	id, err := client.SendToToken(t.Context(), push.Message{
		Token:        "device-token-1",
		Notification: &push.Notification{Title: "Hello", Body: "World"},
		Data:         map[string]string{"kind": "test"},
	})
	if err != nil {
		t.Fatalf("SendToToken() error = %v", err)
	}
	if id != "message-1" {
		t.Errorf("id = %q, want message-1", id)
	}
	if fake.lastAuth != "Bearer access-1" {
		t.Errorf("authorization = %q, want the bearer token", fake.lastAuth)
	}
	message, _ := fake.lastBody["message"].(map[string]any)
	if message["token"] != "device-token-1" {
		t.Errorf("message = %v, want the registration token", message)
	}
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Hello" {
		t.Errorf("notification = %v, want the title", notification)
	}
	data, _ := message["data"].(map[string]any)
	if data["kind"] != "test" {
		t.Errorf("data = %v, want the payload", data)
	}

	if _, err := client.SendToToken(t.Context(), push.Message{Token: "device-token-1"}); err != nil {
		t.Fatalf("second SendToToken() error = %v", err)
	}
	if fake.tokenCalls != 1 {
		t.Errorf("token calls = %d, want the access token cached", fake.tokenCalls)
	}
}

func TestSendToTokenAcceptsBase64Credentials(t *testing.T) {
	fake := newStub(t)
	document := serviceAccountJSON(t, fake.URL+"/token")
	client := newClient(t, fake, base64.StdEncoding.EncodeToString([]byte(document)))
	if !client.Enabled() {
		t.Fatal("Enabled() = false with base64 credentials")
	}
	if _, err := client.SendToToken(t.Context(), push.Message{Token: "device-token-1"}); err != nil {
		t.Fatalf("SendToToken() error = %v", err)
	}
	if fake.sendCalls != 1 {
		t.Errorf("send calls = %d, want 1", fake.sendCalls)
	}
}

func TestFailureMapping(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantCode     push.ErrorCode
		wantRetry    bool
		providerCode string
	}{
		{name: "unregistered token", status: http.StatusNotFound, body: `{"error":{"status":"UNREGISTERED","message":"Requested entity was not found."}}`, wantCode: push.CodeInvalidToken, wantRetry: false, providerCode: "UNREGISTERED"},
		{name: "invalid argument", status: http.StatusBadRequest, body: `{"error":{"status":"INVALID_ARGUMENT","message":"not a valid FCM registration token"}}`, wantCode: push.CodeInvalidToken, wantRetry: false, providerCode: "INVALID_ARGUMENT"},
		{name: "server error", status: http.StatusServiceUnavailable, body: `{"error":{"status":"UNAVAILABLE","message":"try later"}}`, wantCode: push.CodeSendFailed, wantRetry: true, providerCode: "UNAVAILABLE"},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED"}}`, wantCode: push.CodeSendFailed, wantRetry: true, providerCode: "RESOURCE_EXHAUSTED"},
		{name: "other client error", status: http.StatusForbidden, body: `{"error":{"status":"PERMISSION_DENIED"}}`, wantCode: push.CodeSendFailed, wantRetry: false, providerCode: "PERMISSION_DENIED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newStub(t)
			fake.sendStatus = tc.status
			fake.sendBody = tc.body
			client := newClient(t, fake, serviceAccountJSON(t, fake.URL+"/token"))
			_, err := client.SendToToken(t.Context(), push.Message{Token: "device-token-1"})
			if err == nil {
				t.Fatal("SendToToken() = nil, want a failure")
			}
			var sendErr *push.SendError
			if !errors.As(err, &sendErr) {
				t.Fatalf("error = %v, want a SendError", err)
			}
			if sendErr.Code != tc.wantCode || sendErr.Retryable != tc.wantRetry {
				t.Errorf("error = %+v, want %s retryable=%t", sendErr, tc.wantCode, tc.wantRetry)
			}
			if tc.providerCode != "" && sendErr.ProviderCode != tc.providerCode {
				t.Errorf("provider code = %q, want %q", sendErr.ProviderCode, tc.providerCode)
			}
		})
	}
}

func TestDisabledClient(t *testing.T) {
	client := push.New(push.Options{}, clock.Fixed{Moment: moment})
	if client.Enabled() {
		t.Fatal("Enabled() = true without options")
	}
	_, err := client.SendToToken(t.Context(), push.Message{Token: "device-token-1"})
	var sendErr *push.SendError
	if !errors.As(err, &sendErr) || sendErr.Code != push.CodeNotConfigured || sendErr.Retryable {
		t.Fatalf("error = %v, want a non-retryable not-configured failure", err)
	}
}
