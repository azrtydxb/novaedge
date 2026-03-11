package convert

import (
	"math"
	"testing"
)

func TestSafeIntToInt32(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int32
	}{
		{"zero", 0, 0},
		{"positive", 42, 42},
		{"max int32", math.MaxInt32, math.MaxInt32},
		{"overflow high", math.MaxInt32 + 1, math.MaxInt32},
		{"overflow low", math.MinInt32 - 1, math.MinInt32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeIntToInt32(tt.in); got != tt.want {
				t.Errorf("SafeIntToInt32(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
