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

package snapshot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestConvertCircuitBreaker(t *testing.T) {
	tests := []struct {
		name     string
		input    *novaedgev1alpha1.CircuitBreaker
		expected *pb.CircuitBreaker
	}{
		{
			name: "all fields set",
			input: &novaedgev1alpha1.CircuitBreaker{
				MaxConnections:     ptrInt32(100),
				MaxPendingRequests: ptrInt32(50),
				MaxRequests:        ptrInt32(200),
				MaxRetries:         ptrInt32(3),
			},
			expected: &pb.CircuitBreaker{
				MaxConnections:     100,
				MaxPendingRequests: 50,
				MaxRequests:        200,
				MaxRetries:         3,
			},
		},
		{
			name: "partial fields set",
			input: &novaedgev1alpha1.CircuitBreaker{
				MaxConnections: ptrInt32(100),
			},
			expected: &pb.CircuitBreaker{
				MaxConnections:     100,
				MaxPendingRequests: 0,
				MaxRequests:        0,
				MaxRetries:         0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertCircuitBreaker(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.MaxConnections, result.MaxConnections)
				assert.Equal(t, tt.expected.MaxPendingRequests, result.MaxPendingRequests)
				assert.Equal(t, tt.expected.MaxRequests, result.MaxRequests)
				assert.Equal(t, tt.expected.MaxRetries, result.MaxRetries)
			}
		})
	}
}

func TestConvertHealthCheck(t *testing.T) {
	tests := []struct {
		name     string
		input    *novaedgev1alpha1.HealthCheck
		expected *pb.HealthCheck
	}{
		{
			name: "all fields set",
			input: &novaedgev1alpha1.HealthCheck{
				Interval:           metav1.Duration{Duration: 10 * time.Second},
				Timeout:            metav1.Duration{Duration: 5 * time.Second},
				HealthyThreshold:   ptrInt32(3),
				UnhealthyThreshold: ptrInt32(2),
				HTTPPath:           ptrString("/health"),
			},
			expected: &pb.HealthCheck{
				IntervalMs:         10000,
				TimeoutMs:          5000,
				HealthyThreshold:   3,
				UnhealthyThreshold: 2,
				HttpPath:           "/health",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHealthCheck(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.IntervalMs, result.IntervalMs)
				assert.Equal(t, tt.expected.TimeoutMs, result.TimeoutMs)
				assert.Equal(t, tt.expected.HealthyThreshold, result.HealthyThreshold)
				assert.Equal(t, tt.expected.UnhealthyThreshold, result.UnhealthyThreshold)
				assert.Equal(t, tt.expected.HttpPath, result.HttpPath)
			}
		})
	}
}

func TestDurationToMillis(t *testing.T) {
	tests := []struct {
		name     string
		input    metav1.Duration
		expected int64
	}{
		{
			name:     "zero duration",
			input:    metav1.Duration{Duration: 0},
			expected: 0,
		},
		{
			name:     "one second",
			input:    metav1.Duration{Duration: time.Second},
			expected: 1000,
		},
		{
			name:     "500 milliseconds",
			input:    metav1.Duration{Duration: 500 * time.Millisecond},
			expected: 500,
		},
		{
			name:     "1.5 seconds",
			input:    metav1.Duration{Duration: 1500 * time.Millisecond},
			expected: 1500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := durationToMillis(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDurationToSeconds(t *testing.T) {
	tests := []struct {
		name     string
		input    *metav1.Duration
		expected int64
	}{
		{
			name:     "nil duration",
			input:    nil,
			expected: 0,
		},
		{
			name:     "one minute",
			input:    &metav1.Duration{Duration: time.Minute},
			expected: 60,
		},
		{
			name:     "30 seconds",
			input:    &metav1.Duration{Duration: 30 * time.Second},
			expected: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := durationToSeconds(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetWeight(t *testing.T) {
	tests := []struct {
		name     string
		input    *int32
		expected int32
	}{
		{
			name:     "nil weight returns default 1",
			input:    nil,
			expected: 1,
		},
		{
			name:     "weight 100",
			input:    ptrInt32(100),
			expected: 100,
		},
		{
			name:     "weight 0",
			input:    ptrInt32(0),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWeight(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetInt32(t *testing.T) {
	tests := []struct {
		name     string
		input    *int32
		expected int32
	}{
		{
			name:     "nil returns 0",
			input:    nil,
			expected: 0,
		},
		{
			name:     "value 42",
			input:    ptrInt32(42),
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getInt32(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected string
	}{
		{
			name:     "nil returns empty",
			input:    nil,
			expected: "",
		},
		{
			name:     "value test",
			input:    ptrString("test"),
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertSessionAffinity(t *testing.T) {
	tests := []struct {
		name         string
		input        *novaedgev1alpha1.SessionAffinityConfig
		expectedType string
	}{
		{
			name:  "nil input",
			input: nil,
		},
		{
			name: "cookie type",
			input: &novaedgev1alpha1.SessionAffinityConfig{
				Type:       "Cookie",
				CookieName: "SESSION",
				CookiePath: "/app",
			},
			expectedType: "cookie",
		},
		{
			name: "header type",
			input: &novaedgev1alpha1.SessionAffinityConfig{
				Type: "Header",
			},
			expectedType: "header",
		},
		{
			name: "source ip type",
			input: &novaedgev1alpha1.SessionAffinityConfig{
				Type: "SourceIP",
			},
			expectedType: "source_ip",
		},
		{
			name: "unknown type defaults to cookie",
			input: &novaedgev1alpha1.SessionAffinityConfig{
				Type: "Unknown",
			},
			expectedType: "cookie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSessionAffinity(tt.input)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expectedType, result.Type)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "bytes only",
			input:    "1024",
			expected: 1024,
		},
		{
			name:        "invalid format",
			input:       "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseByteSize(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseDurationMs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "milliseconds",
			input:    "500ms",
			expected: 500,
		},
		{
			name:     "seconds",
			input:    "30s",
			expected: 30000,
		},
		{
			name:     "minutes",
			input:    "2m",
			expected: 120000,
		},
		{
			name:        "invalid format",
			input:       "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDurationMs(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "valid number",
			input:    "42",
			expected: 42,
		},
		{
			name:     "negative number",
			input:    "-10",
			expected: -10,
		},
		{
			name:        "invalid number",
			input:       "not-a-number",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInt64(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// Helper functions - using the same pattern as existing test files
func ptrInt32(v int32) *int32 {
	return &v
}

func ptrString(v string) *string {
	return &v
}
