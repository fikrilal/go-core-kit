// Package utctime provides the UTC-normalized time type used for database columns.
package utctime

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Time is a timestamp stored and loaded as UTC.
// Scan of NULL is the zero time, so IsZero means a NULL read.
// Value of the zero time still writes year 1, not SQL NULL.
type Time struct {
	time.Time
}

// Scan implements sql.Scanner.
func (t *Time) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*t = Time{}
		return nil
	case time.Time:
		*t = Time{v.UTC()}
		return nil
	case string:
		return t.parse(v)
	case []byte:
		return t.parse(string(v))
	default:
		return fmt.Errorf("utctime: cannot scan %T", value)
	}
}

// valueLayout is fixed-width so TEXT comparisons in SQLite stay chronological.
const valueLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Value implements driver.Valuer.
func (t Time) Value() (driver.Value, error) {
	return t.UTC().Format(valueLayout), nil
}

func (t *Time) parse(raw string) error {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			*t = Time{parsed.UTC()}
			return nil
		}
	}
	return fmt.Errorf("utctime: cannot parse %q", raw)
}
