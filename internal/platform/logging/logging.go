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

// redact covers attribute keys, including a key inside a group.
// slog.Any is walked only for map[string]any, map[string]string, and []any.
// Structs, http.Header, and other typed maps or slices are not scanned.
// Message text is not scanned. A camelCase key such as accessToken is left as written.
func redact(_ []string, a slog.Attr) slog.Attr {
	a.Value = redactValue(a.Key, a.Value)
	return a
}

func redactValue(key string, value slog.Value) slog.Value {
	value = value.Resolve()
	if _, ok := sensitiveKeys[strings.ToLower(key)]; ok {
		return slog.StringValue(redactedMarker)
	}
	switch value.Kind() {
	case slog.KindGroup:
		attrs := value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, attr := range attrs {
			redacted[i] = slog.Attr{Key: attr.Key, Value: redactValue(attr.Key, attr.Value)}
		}
		return slog.GroupValue(redacted...)
	case slog.KindAny:
		return slog.AnyValue(redactAny(value.Any()))
	default:
		return value
	}
}

func redactAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if _, ok := sensitiveKeys[strings.ToLower(key)]; ok {
				out[key] = redactedMarker
				continue
			}
			out[key] = redactAny(nested)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, nested := range typed {
			if _, ok := sensitiveKeys[strings.ToLower(key)]; ok {
				out[key] = redactedMarker
				continue
			}
			out[key] = nested
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = redactAny(nested)
		}
		return out
	default:
		return value
	}
}
