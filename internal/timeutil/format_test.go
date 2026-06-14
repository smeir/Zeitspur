package timeutil

import "testing"

func TestFormatMinutes(t *testing.T) {
	tests := []struct {
		minutes int
		want    string
	}{
		{0, "0m"},
		{42, "42m"},
		{60, "1h 0m"},
		{90, "1h 30m"},
		{342, "5h 42m"},
		{632, "10h 32m"},
		{-15, "15m"},
	}

	for _, tt := range tests {
		if got := FormatMinutes(tt.minutes); got != tt.want {
			t.Errorf("FormatMinutes(%d) = %q, want %q", tt.minutes, got, tt.want)
		}
	}
}
