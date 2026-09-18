// Package clock provides the injectable time source for service and domain code.
package clock

import "time"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// System is the production clock; it always reports UTC.
type System struct{}

// Now returns the current UTC time.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a test clock frozen at a moment.
type Fixed struct {
	Moment time.Time
}

// Now returns the frozen time.
func (f Fixed) Now() time.Time { return f.Moment }
