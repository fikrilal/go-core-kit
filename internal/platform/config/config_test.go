package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_PATH", "./.data/test.db")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
}

func unsetenv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		old, ok := os.LookupEnv(key)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
				return
			}
			_ = os.Unsetenv(key)
		})
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

func TestLoadUsesDefaults(t *testing.T) {
	unsetenv(t, "APP_ENV", "HTTP_PORT", "LOG_LEVEL", "LOG_FORMAT")
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != config.EnvDev {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, config.EnvDev)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("HTTPPort = %d, want 8080", cfg.HTTPPort)
	}
	if cfg.LogLevel != config.LogLevelInfo {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, config.LogLevelInfo)
	}
	if cfg.LogFormat != config.LogFormatJSON {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, config.LogFormatJSON)
	}
}

func TestLoadParsesValues(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "text")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != config.EnvStaging || cfg.HTTPPort != 9090 || cfg.LogLevel != config.LogLevelWarn || cfg.LogFormat != config.LogFormatText {
		t.Errorf("Load() = %+v, want staging/9090/warn/text", cfg)
	}
	if cfg.DatabasePath != "./.data/test.db" || cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("required values not parsed: %+v", cfg)
	}
}

func TestLoadFailsOnMissingRequired(t *testing.T) {
	unsetenv(t, "DATABASE_PATH")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_PATH") {
		t.Errorf("Load() error = %v, want DATABASE_PATH error", err)
	}
}

func TestLoadFailsOnParseError(t *testing.T) {
	setRequired(t)
	t.Setenv("HTTP_PORT", "not-a-number")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "HTTPPort") {
		t.Errorf("Load() error = %v, want HTTPPort parse error", err)
	}
}

func TestLoadFailsOnInvalidEnv(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "wat")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Errorf("Load() error = %v, want APP_ENV error", err)
	}
}

func TestLoadFailsOnPortOutOfRange(t *testing.T) {
	setRequired(t)
	t.Setenv("HTTP_PORT", "0")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "HTTP_PORT") {
		t.Errorf("Load() error = %v, want HTTP_PORT range error", err)
	}
}

func TestLoadAuthIntegersFailInOrder(t *testing.T) {
	setRequired(t)
	t.Setenv("AUTH_ACCESS_TOKEN_TTL_SECONDS", "0")
	t.Setenv("AUTH_PASSWORD_MIN_LENGTH", "0")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want the first auth integer")
	}
	const want = "AUTH_ACCESS_TOKEN_TTL_SECONDS must be a positive number, got 0"
	if err.Error() != want {
		t.Errorf("Load() error = %q, want %q", err.Error(), want)
	}
}

func TestLoadProdRequiresJSONLogs(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("LOG_FORMAT", "text")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Errorf("Load() error = %v, want LOG_FORMAT invariant error", err)
	}
}

func TestLoadPushEnabledDefaultsOffAndParsesOverride(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PushEnabled {
		t.Error("PushEnabled = true by default, want false")
	}

	t.Setenv("PUSH_ENABLED", "true")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.PushEnabled {
		t.Error("PushEnabled = false after PUSH_ENABLED=true")
	}
}
