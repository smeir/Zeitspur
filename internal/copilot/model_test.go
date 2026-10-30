package copilot

import (
	"math"
	"testing"
)

func TestCreditsToDollars(t *testing.T) {
	tests := []struct {
		credits float64
		want    float64
	}{
		{credits: 1, want: 0.01},
		{credits: 100, want: 1},
		{credits: 2500, want: 25},
	}

	for _, tt := range tests {
		got := CreditsToDollars(tt.credits)
		if math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("CreditsToDollars(%v) = %v, want %v", tt.credits, got, tt.want)
		}
	}
}
