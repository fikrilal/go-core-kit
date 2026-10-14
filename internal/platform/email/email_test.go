package email_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/email"
)

func TestClientDisabledWithoutConfiguration(t *testing.T) {
	client := email.New(email.Options{})
	if client.Enabled() {
		t.Fatal("Enabled() = true without an API key and sender")
	}
	if _, err := client.Send(t.Context(), email.Message{To: "user@example.com", Subject: "s", Text: "t"}); err == nil {
		t.Error("Send() = nil for a disabled client")
	}
}

func TestClientSendsThroughResend(t *testing.T) {
	var received map[string]any
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emails" {
			t.Errorf("path = %q, want /emails", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"email-1"}`))
	}))
	t.Cleanup(server.Close)

	client := email.New(email.Options{
		APIKey:  "resend-key",
		From:    "auth@example.com",
		ReplyTo: "support@example.com",
		BaseURL: server.URL,
	})
	id, err := client.Send(t.Context(), email.Message{
		To: "user@example.com", Subject: "Verify your email", Text: "body", HTML: "<p>body</p>",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "email-1" {
		t.Errorf("id = %q, want email-1", id)
	}
	if authHeader != "Bearer resend-key" {
		t.Errorf("authorization = %q, want the bearer key", authHeader)
	}
	if received["from"] != "auth@example.com" || received["subject"] != "Verify your email" || received["reply_to"] != "support@example.com" {
		t.Errorf("payload = %v, want the configured sender and reply-to", received)
	}
	to, _ := received["to"].([]any)
	if len(to) != 1 || to[0] != "user@example.com" {
		t.Errorf("to = %v, want the recipient list", received["to"])
	}
}

func TestClientReportsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid from"}`))
	}))
	t.Cleanup(server.Close)

	client := email.New(email.Options{APIKey: "key", From: "auth@example.com", BaseURL: server.URL})
	_, err := client.Send(t.Context(), email.Message{To: "user@example.com", Subject: "s", Text: "t"})
	if err == nil {
		t.Fatal("Send() = nil, want a provider error")
	}
	if !strings.Contains(err.Error(), "resend") || !strings.Contains(err.Error(), "422") {
		t.Errorf("error = %v, want the provider and status named", err)
	}
	if !strings.Contains(err.Error(), "invalid from") {
		t.Errorf("error = %v, want the provider detail carried", err)
	}
}

func TestMessageValidation(t *testing.T) {
	client := email.New(email.Options{APIKey: "key", From: "auth@example.com", BaseURL: "http://127.0.0.1:1"})
	if _, err := client.Send(t.Context(), email.Message{Subject: "s", Text: "t"}); err == nil {
		t.Error("Send() without a recipient = nil, want an error")
	}
	if _, err := client.Send(t.Context(), email.Message{To: "user@example.com", Subject: "s"}); err == nil {
		t.Error("Send() without a body = nil, want an error")
	}
}

func TestTemplates(t *testing.T) {
	expiresAt := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	message := email.VerificationEmail("user@example.com", email.BrandName("https://app.example.com"), "tok-123", email.VerifyEmailURL("https://app.example.com", "tok-123"), expiresAt)
	if message.Subject != "Verify your email" {
		t.Errorf("subject = %q", message.Subject)
	}
	for _, want := range []string{"tok-123", "https://app.example.com/verify-email?token=tok-123", "2026-10-14T12:00:00Z"} {
		if !strings.Contains(message.Text, want) {
			t.Errorf("text = %q, want %q", message.Text, want)
		}
	}
	if !strings.Contains(message.HTML, "app.example.com") || !strings.Contains(message.HTML, "tok-123") {
		t.Errorf("html = %q, want the brand and token", message.HTML)
	}

	reset := email.PasswordResetEmail("user@example.com", "app.example.com", "tok-456", email.ResetPasswordURL("https://app.example.com", "tok-456"), expiresAt)
	if reset.Subject != "Reset your password" || !strings.Contains(reset.Text, "https://app.example.com/reset-password?token=tok-456") {
		t.Errorf("reset = %q/%q, want the reset link", reset.Subject, reset.Text)
	}

	if url := email.VerifyEmailURL("", "tok"); url != "" {
		t.Errorf("VerifyEmailURL(empty) = %q, want empty", url)
	}
	if brand := email.BrandName("not a url"); brand != "go-core-kit" {
		t.Errorf("BrandName(invalid) = %q, want the default", brand)
	}
}
