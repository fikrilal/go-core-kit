package utctime_test

import (
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
)

func TestScanNormalizesToUTC(t *testing.T) {
	offset := time.FixedZone("plus7", 7*60*60)
	input := time.Date(2026, 10, 10, 12, 0, 0, 0, offset)
	want := input.UTC()

	cases := map[string]any{
		"time":     input,
		"string":   input.Format(time.RFC3339Nano),
		"bytes":    []byte(input.Format(time.RFC3339Nano)),
		"database": input.Format("2006-01-02 15:04:05.999999999-07:00"),
	}
	for name, value := range cases {
		var got utctime.Time
		if err := got.Scan(value); err != nil {
			t.Errorf("%s: Scan() error = %v", name, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", name, got.Time, want)
		}
		if got.Location() != time.UTC {
			t.Errorf("%s: location = %v, want UTC", name, got.Location())
		}
	}
}

func TestScanNilIsZero(t *testing.T) {
	got := utctime.Time{Time: time.Now()}
	if err := got.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error = %v", err)
	}
	if !got.IsZero() {
		t.Errorf("got %v, want zero time", got.Time)
	}
}

func TestScanRejectsGarbage(t *testing.T) {
	var got utctime.Time
	if err := got.Scan("not a time"); err == nil {
		t.Fatal("Scan(garbage) = nil, want error")
	}
	if err := got.Scan(42); err == nil {
		t.Fatal("Scan(int) = nil, want error")
	}
}

func TestScanParsesSQLiteNativeFormats(t *testing.T) {
	cases := map[string]string{
		"2026-10-10 12:00:00":     "2026-10-10T12:00:00Z",
		"2026-10-10 12:00:00.123": "2026-10-10T12:00:00.123Z",
	}
	for raw, want := range cases {
		var got utctime.Time
		if err := got.Scan(raw); err != nil {
			t.Errorf("Scan(%q) error = %v", raw, err)
			continue
		}
		if formatted := got.UTC().Format(time.RFC3339Nano); formatted != want {
			t.Errorf("Scan(%q) = %s, want %s", raw, formatted, want)
		}
	}
}

func TestValueIsFixedWidthAndSortable(t *testing.T) {
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	previous := ""
	for _, at := range []time.Time{
		base,
		base.Add(time.Nanosecond),
		base.Add(100 * time.Millisecond),
		base.Add(150 * time.Millisecond),
	} {
		value, err := utctime.Time{Time: at}.Value()
		if err != nil {
			t.Fatalf("Value() error = %v", err)
		}
		text, ok := value.(string)
		if !ok {
			t.Fatalf("Value() type = %T, want string", value)
		}
		if len(text) != len("2026-10-10T12:00:00.000000000Z") {
			t.Errorf("Value() = %q, want fixed width", text)
		}
		if previous != "" && text <= previous {
			t.Errorf("format not sortable: %q <= %q", text, previous)
		}
		previous = text
	}
}
