// Package convert provides shared numeric conversion helpers.
package convert

import "math"

// SafeIntToInt32 safely converts an int to int32, clamping to the int32 range.
func SafeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) //nolint:gosec // bounds checked above
}
