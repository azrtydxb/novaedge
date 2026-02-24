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

package router

import (
	"testing"
)

func TestShouldMirror(t *testing.T) {
	tests := []struct {
		name       string
		percentage int32
		// We test the boundaries deterministically
		expected bool
	}{
		{"zero percent never mirrors", 0, false},
		{"negative percent never mirrors", -10, false},
		{"100 percent always mirrors", 100, true},
		{"over 100 percent always mirrors", 150, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldMirror(tt.percentage)
			if result != tt.expected {
				t.Errorf("shouldMirror(%d) = %v, want %v", tt.percentage, result, tt.expected)
			}
		})
	}
}

func TestShouldMirrorProbabilistic(t *testing.T) {
	// Test that 50% mirrors roughly half the time (statistical test)
	const trials = 10000
	mirrorCount := 0
	for i := 0; i < trials; i++ {
		if shouldMirror(50) {
			mirrorCount++
		}
	}

	// Allow 10% tolerance
	lower := int(float64(trials) * 0.40)
	upper := int(float64(trials) * 0.60)
	if mirrorCount < lower || mirrorCount > upper {
		t.Errorf("shouldMirror(50) mirrored %d/%d times, expected between %d and %d",
			mirrorCount, trials, lower, upper)
	}
}

func TestDiscardResponseWriter(t *testing.T) {
	w := &discardResponseWriter{}

	// Test Header returns non-nil
	h := w.Header()
	if h == nil {
		t.Error("Header() returned nil")
	}

	// Test Write discards but reports correct length
	data := []byte("hello world")
	n, err := w.Write(data)
	if err != nil {
		t.Errorf("Write() error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() returned %d, want %d", n, len(data))
	}

	// Test WriteHeader
	w.WriteHeader(200)
	if w.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", w.statusCode)
	}
}

func TestMirrorMetrics(t *testing.T) {
	// Reset metrics
	globalMirrorMetrics.requestsTotal.Store(0)
	globalMirrorMetrics.errorsTotal.Store(0)

	if MirrorRequestsTotal() != 0 {
		t.Errorf("MirrorRequestsTotal() = %d, want 0", MirrorRequestsTotal())
	}
	if MirrorErrorsTotal() != 0 {
		t.Errorf("MirrorErrorsTotal() = %d, want 0", MirrorErrorsTotal())
	}

	// Set values
	globalMirrorMetrics.requestsTotal.Store(5)
	globalMirrorMetrics.errorsTotal.Store(2)

	if MirrorRequestsTotal() != 5 {
		t.Errorf("MirrorRequestsTotal() = %d, want 5", MirrorRequestsTotal())
	}
	if MirrorErrorsTotal() != 2 {
		t.Errorf("MirrorErrorsTotal() = %d, want 2", MirrorErrorsTotal())
	}
}
