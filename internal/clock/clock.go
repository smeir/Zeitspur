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
//
// The monotonic clock reading is stripped via Round(0). Go attaches a
// monotonic reading to every time.Now() result, and time.Time.Sub prefers it
// over the wall-clock reading whenever both operands have one. On Linux, the
// monotonic clock is backed by CLOCK_MONOTONIC, which does not advance while
// the system is suspended (unlike CLOCK_BOOTTIME). Two System.Now() readings
// taken before and after a suspend/resume cycle would therefore report a
// near-zero gap instead of the real wall-clock duration, silently hiding the
// suspend from any code that measures elapsed time this way (this caused a
// real incident where a suspend went undetected and the whole idle night was
// booked as work time). Stripping the reading here forces every comparison
// to fall back to wall-clock semantics, matching the behaviour of Fixed and
// of every timestamp in this codebase, which is always persisted as an
// RFC3339Nano string anyway.
func (System) Now() time.Time { return time.Now().Round(0) }

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
