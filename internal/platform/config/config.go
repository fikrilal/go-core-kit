// Package config loads and validates platform configuration from the environment.
package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Env identifies the deployment environment.
type Env string

// Deployment environments.
const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
	EnvTest    Env = "test"
)

// LogLevel controls the minimum severity emitted by the logger.
type LogLevel string

// Log levels.
const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogFormat selects the log output encoding.
type LogFormat string

// Log formats.
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Config is the validated platform configuration.
type Config struct {
	AppEnv       Env       `env:"APP_ENV" envDefault:"dev"`
	HTTPPort     int       `env:"HTTP_PORT" envDefault:"8080"`
	DatabasePath string    `env:"DATABASE_PATH,required"`
	RedisURL     string    `env:"REDIS_URL,required"`
	LogLevel     LogLevel  `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat    LogFormat `env:"LOG_FORMAT" envDefault:"json"`

	AuthAccessTokenTTLSeconds  int    `env:"AUTH_ACCESS_TOKEN_TTL_SECONDS" envDefault:"900"`
	AuthRefreshTokenTTLSeconds int    `env:"AUTH_REFRESH_TOKEN_TTL_SECONDS" envDefault:"2592000"`
	AuthPasswordMinLength      int    `env:"AUTH_PASSWORD_MIN_LENGTH" envDefault:"10"`
	AuthLoginMaxAttempts       int    `env:"AUTH_LOGIN_MAX_ATTEMPTS" envDefault:"10"`
	AuthLoginWindowSeconds     int    `env:"AUTH_LOGIN_WINDOW_SECONDS" envDefault:"60"`
	AuthLoginBlockSeconds      int    `env:"AUTH_LOGIN_BLOCK_SECONDS" envDefault:"900"`
	AuthIssuer                 string `env:"AUTH_ISSUER"`
	AuthAudience               string `env:"AUTH_AUDIENCE"`
	AuthSigningKeysJSON        string `env:"AUTH_SIGNING_KEYS_JSON"`
	AuthSigningKeysJSONBase64  string `env:"AUTH_SIGNING_KEYS_JSON_BASE64"`
	AuthOIDCGoogleClientIDs    string `env:"AUTH_OIDC_GOOGLE_CLIENT_IDS"`
	AuthOIDCGoogleJWKSURL      string `env:"AUTH_OIDC_GOOGLE_JWKS_URL"`

	// PushEnabled gates push registration until the FCM adapter lands (5.3);
	// it mirrors the reference's provider-configured check.
	PushEnabled bool `env:"PUSH_ENABLED" envDefault:"false"`
}

// ProductionLike reports whether the environment requires real secrets.
// platform/auth NewKeyRing and NewVerifier enforce them. Validate does not.
func (c Config) ProductionLike() bool {
	return c.AppEnv == EnvStaging || c.AppEnv == EnvProd
}

// Load reads the environment, applies defaults, and validates the result.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// MustLoad is Load for composition roots; it panics on invalid configuration.
func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		panic(err)
	}
	return cfg
}

// Validate checks enum values, ranges, and cross-field invariants.
func (c Config) Validate() error {
	if !c.AppEnv.valid() {
		return fmt.Errorf("APP_ENV %q is not one of: dev, staging, prod, test", c.AppEnv)
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("HTTP_PORT %d is out of range 1-65535", c.HTTPPort)
	}
	if !c.LogLevel.valid() {
		return fmt.Errorf("LOG_LEVEL %q is not one of: debug, info, warn, error", c.LogLevel)
	}
	if !c.LogFormat.valid() {
		return fmt.Errorf("LOG_FORMAT %q is not one of: json, text", c.LogFormat)
	}
	if c.AppEnv == EnvProd && c.LogFormat != LogFormatJSON {
		return fmt.Errorf("LOG_FORMAT must be %q when APP_ENV is prod", LogFormatJSON)
	}
	authIntegers := []struct {
		name  string
		value int
	}{
		{"AUTH_ACCESS_TOKEN_TTL_SECONDS", c.AuthAccessTokenTTLSeconds},
		{"AUTH_REFRESH_TOKEN_TTL_SECONDS", c.AuthRefreshTokenTTLSeconds},
		{"AUTH_PASSWORD_MIN_LENGTH", c.AuthPasswordMinLength},
		{"AUTH_LOGIN_MAX_ATTEMPTS", c.AuthLoginMaxAttempts},
		{"AUTH_LOGIN_WINDOW_SECONDS", c.AuthLoginWindowSeconds},
		{"AUTH_LOGIN_BLOCK_SECONDS", c.AuthLoginBlockSeconds},
	}
	for _, check := range authIntegers {
		if check.value < 1 {
			return fmt.Errorf("%s must be a positive number, got %d", check.name, check.value)
		}
	}
	return nil
}

func (e Env) valid() bool {
	switch e {
	case EnvDev, EnvStaging, EnvProd, EnvTest:
		return true
	}
	return false
}

func (l LogLevel) valid() bool {
	switch l {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	}
	return false
}

func (f LogFormat) valid() bool {
	switch f {
	case LogFormatJSON, LogFormatText:
		return true
	}
	return false
}
