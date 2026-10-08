package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// OptionalString is a JSON field with three states: absent, explicit null, or
// a string value. A string is trimmed on decode, so a whitespace-only value
// arrives empty and fails non-empty validation. Go pointers cannot carry the
// distinction: absent and null both decode to nil.
type OptionalString struct {
	present bool
	null    bool
	value   string
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.null = true
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("optional string: %w", err)
	}
	o.value = strings.TrimSpace(decoded)
	return nil
}

// Present reports whether the field appeared in the body, including an explicit null.
func (o OptionalString) Present() bool { return o.present }

// Null reports whether the field was an explicit null.
func (o OptionalString) Null() bool { return o.present && o.null }

// Value returns the trimmed string; empty when the field is absent or null.
func (o OptionalString) Value() string { return o.value }
