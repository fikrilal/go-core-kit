package redis_test

import (
	"strings"
	"testing"

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
