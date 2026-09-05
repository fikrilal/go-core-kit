// Package logging configures the platform structured logger.
package logging

import (
	"io"
	"log/slog"
	"strings"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
)

// Correlation field keys for structured logs.
const (
	FieldRequestID = "request_id"
	FieldTraceID   = "trace_id"
)

const redactedMarker = "[REDACTED]"

var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"authorization": {},
	"cookie":        {},
	"secret":        {},
	"api_key":       {},
	"apikey":        {},
	"private_key":   {},
}

// New builds the platform logger with the configured level, format, and redaction.
func New(level config.LogLevel, format config.LogFormat, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       slogLevel(level),
		ReplaceAttr: redact,
	}
	var handler slog.Handler
	if format == config.LogFormatJSON {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}

func slogLevel(l config.LogLevel) slog.Level {
	switch l {
	case config.LogLevelDebug:
		return slog.LevelDebug
	case config.LogLevelWarn:
		return slog.LevelWarn
	case config.LogLevelError:
		return slog.LevelError
	case config.LogLevelInfo:
		return slog.LevelInfo
	}
	return slog.LevelInfo
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if _, ok := sensitiveKeys[strings.ToLower(a.Key)]; ok {
		a.Value = slog.StringValue(redactedMarker)
	}
	return a
}
