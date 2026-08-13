package clock

import (
	"strings"
	"testing"
)

// TestSystemNowStripsMonotonicReading guards against a regression that let a
// system suspend/resume cycle go completely undetected in production: the
// activity engine measured gaps with now.Sub(lastActiveAt), and since
// CLOCK_MONOTONIC (Go's monotonic clock source on Linux) does not advance
// while the system is suspended, that subtraction silently used a near-zero
// monotonic delta instead of the real wall-clock gap.
//
// time.Time.String() appends a "m=" suffix whenever a monotonic reading is
// present (documented behaviour of the time package), which makes the
// reading observable without touching unexported fields.
func TestSystemNowStripsMonotonicReading(t *testing.T) {
	now := System{}.Now()
	if strings.Contains(now.String(), "m=") {
		t.Fatalf("expected System.Now() to strip the monotonic clock reading, got %v", now)
	}
}
