// Package config loads and validates platform configuration from the environment.
package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Env identifies the deployment environment.
type Env string

// Supported deployment environments.
const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
	EnvTest    Env = "test"
)

// LogLevel controls the minimum severity emitted by the logger.
type LogLevel string

// Supported log levels.
const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogFormat selects the log output encoding.
type LogFormat string

// Supported log formats.
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Config is the validated platform configuration.
type Config struct {
	// AppEnv is the deployment environment.
	AppEnv Env `env:"APP_ENV" envDefault:"dev"`
	// HTTPPort is the API process listen port.
	HTTPPort int `env:"HTTP_PORT" envDefault:"8080"`
	// DatabasePath is the SQLite database file path.
	DatabasePath string `env:"DATABASE_PATH,required"`
	// RedisURL is the Redis connection URL.
	RedisURL string `env:"REDIS_URL,required"`
	// LogLevel is the minimum log severity.
	LogLevel LogLevel `env:"LOG_LEVEL" envDefault:"info"`
	// LogFormat selects structured JSON or human-readable text output.
	LogFormat LogFormat `env:"LOG_FORMAT" envDefault:"json"`
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
