// Package clock provides a testable abstraction over time.
package clock

import "time"

// Clock abstracts time.Now for testability.
type Clock interface {
	Now() time.Time
}

// System returns a Clock backed by the real system time.
type System struct{}

// Now returns the current system time.
func (System) Now() time.Time { return time.Now() }

// Fixed returns a Clock that always returns the configured time.
type Fixed struct {
	t time.Time
}

// NewFixed creates a fixed clock.
func NewFixed(t time.Time) *Fixed {
	return &Fixed{t: t}
}

// Now returns the fixed time.
func (f *Fixed) Now() time.Time { return f.t }

// Set updates the fixed time.
func (f *Fixed) Set(t time.Time) { f.t = t }

// Advance moves the fixed time forward by d.
func (f *Fixed) Advance(d time.Duration) { f.t = f.t.Add(d) }
