package redis_test

import (
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	_, err := redis.Open("not-a-url")
	if err == nil || !strings.Contains(err.Error(), "parse redis url") {
		t.Fatalf("Open() error = %v, want parse error", err)
	}
}

func TestKeyJoinsWithColon(t *testing.T) {
	if got := redis.Key("idempotency", "abc"); got != "idempotency:abc" {
		t.Errorf("Key() = %q, want idempotency:abc", got)
	}
}

func TestPingAgainstMiniredis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + server.Addr() + "/0")

	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("Ping() error = %v, want nil", err)
	}

	server.SetError("simulated outage")
	if err := client.Ping(t.Context()); err == nil {
		t.Fatal("Ping() = nil after simulated outage, want error")
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestKeyValueOperations(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + server.Addr() + "/0")
	ctx := t.Context()

	if _, ok, err := client.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) ok=%v err=%v, want not found", ok, err)
	}
	if err := client.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if value, ok, err := client.Get(ctx, "k"); err != nil || !ok || value != "v" {
		t.Fatalf("Get(k) = %q ok=%v err=%v, want v", value, ok, err)
	}
	if stored, err := client.SetNX(ctx, "k", "other", time.Minute); err != nil || stored {
		t.Fatalf("SetNX(existing) = %v err=%v, want false", stored, err)
	}
	if stored, err := client.SetNX(ctx, "k2", "v2", time.Minute); err != nil || !stored {
		t.Fatalf("SetNX(new) = %v err=%v, want true", stored, err)
	}
	if err := client.Del(ctx, "k", "k2"); err != nil {
		t.Fatalf("Del() error = %v", err)
	}
	if _, ok, _ := client.Get(ctx, "k"); ok {
		t.Error("key k still present after Del")
	}
}
