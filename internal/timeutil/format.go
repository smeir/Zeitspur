// Package timeutil provides small formatting helpers for durations.
package timeutil

import "fmt"

// FormatMinutes formats a duration given in minutes as "Xh Ym".
// Values below one hour omit the hour part; zero minutes prints "0m".
func FormatMinutes(minutes int) string {
	if minutes < 0 {
		minutes = -minutes
	}
	hours := minutes / 60
	mins := minutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}
