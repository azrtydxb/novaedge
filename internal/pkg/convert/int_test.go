/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
