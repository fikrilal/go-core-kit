package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
	"github.com/fikrilal/go-core-kit/internal/platform/storage/storagetest"
)

var moment = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func newClient(t *testing.T, endpoint string, forcePathStyle bool) *storage.Client {
	t.Helper()
	return storage.New(storage.Options{
		Endpoint:        endpoint,
		Region:          "auto",
		Bucket:          "profile-images",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
		ForcePathStyle:  forcePathStyle,
	}, clock.Fixed{Moment: moment})
}

func TestClientEnabled(t *testing.T) {
	disabled := storage.New(storage.Options{}, clock.Fixed{Moment: moment})
	if disabled.Enabled() {
		t.Fatal("Enabled() = true without options")
	}
	if _, err := disabled.PresignGet("key", time.Minute); err == nil {
		t.Error("PresignGet() = nil for a disabled client")
	}

	enabled := newClient(t, "https://s3.example.test", false)
	if !enabled.Enabled() {
		t.Fatal("Enabled() = false with all options set")
	}
	if enabled.Bucket() != "profile-images" {
		t.Errorf("Bucket() = %q", enabled.Bucket())
	}
}

func TestPresignPutSignsContentType(t *testing.T) {
	client := newClient(t, "https://s3.example.test", true)
	plan, err := client.PresignPut("users/u1/profile-images/f1", "image/webp", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if plan.Method != http.MethodPut {
		t.Errorf("method = %q, want PUT", plan.Method)
	}
	if plan.Headers["Content-Type"] != "image/webp" {
		t.Errorf("headers = %v, want the declared Content-Type", plan.Headers)
	}
	parsed, err := url.Parse(plan.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.Path != "/profile-images/users/u1/profile-images/f1" {
		t.Errorf("path = %q, want the path-style object path", parsed.Path)
	}
	query := parsed.Query()
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || query.Get("X-Amz-Signature") == "" {
		t.Errorf("query = %v, want a signed request", query)
	}
	if query.Get("X-Amz-Expires") != "600" {
		t.Errorf("expires = %q, want 600", query.Get("X-Amz-Expires"))
	}
	if !strings.Contains(query.Get("X-Amz-SignedHeaders"), "content-type") {
		t.Errorf("signed headers = %q, want the content type included", query.Get("X-Amz-SignedHeaders"))
	}
}

func TestPresignGetUsesVirtualHostStyle(t *testing.T) {
	client := newClient(t, "https://s3.example.test", false)
	plan, err := client.PresignGet("users/u1/profile-images/f1", 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet() error = %v", err)
	}
	parsed, err := url.Parse(plan.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.Host != "profile-images.s3.example.test" {
		t.Errorf("host = %q, want the virtual-hosted bucket", parsed.Host)
	}
	if parsed.Query().Get("X-Amz-SignedHeaders") != "host" {
		t.Errorf("signed headers = %q, want host only", parsed.Query().Get("X-Amz-SignedHeaders"))
	}
}

func TestPresignRejectsOutOfRangeTTL(t *testing.T) {
	client := newClient(t, "https://s3.example.test", false)
	for _, ttl := range []time.Duration{0, 500 * time.Millisecond, 901 * time.Second} {
		if _, err := client.PresignGet("key", ttl); err == nil {
			t.Errorf("PresignGet(%s) = nil, want a ttl error", ttl)
		}
	}
}

func TestObjectLifecycleAgainstFakeS3(t *testing.T) {
	fake := storagetest.New("profile-images")
	t.Cleanup(fake.Close)
	client := newClient(t, fake.Endpoint(), true)
	ctx := t.Context()
	const key = "users/u1/profile-images/f1"
	const body = "profile-bytes"

	plan, err := client.PresignPut(key, "image/png", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, plan.Method, plan.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for name, value := range plan.Headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", response.StatusCode)
	}

	head, err := client.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head() error = %v", err)
	}
	if !head.Exists || head.ContentType == nil || *head.ContentType != "image/png" {
		t.Fatalf("head = %+v, want the stored content type", head)
	}
	if head.ContentLength == nil || *head.ContentLength != int64(len(body)) {
		t.Fatalf("head length = %v, want %d", head.ContentLength, len(body))
	}

	missing, err := client.Head(ctx, "users/u1/profile-images/missing")
	if err != nil {
		t.Fatalf("Head(missing) error = %v", err)
	}
	if missing.Exists {
		t.Error("missing object reported as existing")
	}

	read, err := client.PresignGet(key, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet() error = %v", err)
	}
	readRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, read.URL, nil)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	readResponse, err := http.DefaultClient.Do(readRequest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer func() { _ = readResponse.Body.Close() }()
	got, err := io.ReadAll(readResponse.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != body {
		t.Errorf("read body = %q, want %q", got, body)
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := client.Delete(ctx, key); err != nil {
		t.Errorf("Delete(missing) error = %v, want nil", err)
	}
	if fake.Count() != 0 {
		t.Errorf("objects = %d, want 0 after delete", fake.Count())
	}
}

func TestSignSurfacesTransportErrors(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1", true)
	if _, err := client.Head(context.Background(), "key"); err == nil {
		t.Error("Head() = nil against a dead endpoint, want a transport error")
	} else if errors.Is(err, context.Canceled) {
		t.Errorf("Head() error = %v, want a wrapped transport error", err)
	}
}
