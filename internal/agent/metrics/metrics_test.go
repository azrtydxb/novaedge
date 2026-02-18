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

package metrics

import (
	"testing"
)

func TestStatusClass(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{100, "1xx"},
		{101, "1xx"},
		{150, "1xx"},
		{199, "1xx"},
		{200, "2xx"},
		{201, "2xx"},
		{204, "2xx"},
		{299, "2xx"},
		{300, "3xx"},
		{301, "3xx"},
		{304, "3xx"},
		{399, "3xx"},
		{400, "4xx"},
		{401, "4xx"},
		{403, "4xx"},
		{404, "4xx"},
		{499, "4xx"},
		{500, "5xx"},
		{501, "5xx"},
		{502, "5xx"},
		{503, "5xx"},
		{599, "5xx"},
		{0, "unknown"},
		{99, "unknown"},
		{600, "unknown"},
		{1000, "unknown"},
		{-1, "unknown"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := StatusClass(tt.code)
			if result != tt.expected {
				t.Errorf("StatusClass(%d) = %q, want %q", tt.code, result, tt.expected)
			}
		})
	}
}

func TestRecordHTTPRequest(_ *testing.T) {
	// This test just verifies the function doesn't panic
	RecordHTTPRequest("GET", "2xx", "test-cluster", 0.001)
	RecordHTTPRequest("POST", "4xx", "test-cluster", 0.5)
	RecordHTTPRequest("PUT", "5xx", "test-cluster", 1.0)
}

func TestInitOTelExporter_Disabled(t *testing.T) {
	config := OTelConfig{
		Enabled: false,
	}

	exporter, err := InitOTelExporter(config)
	if err != nil {
		t.Errorf("InitOTelExporter() error = %v", err)
	}
	if exporter != nil {
		t.Error("InitOTelExporter() should return nil when disabled")
	}
}

func TestAggregationModeValues(t *testing.T) {
	// Verify AggregationMode enum values
	if AggregateByEndpoint != 0 {
		t.Errorf("AggregateByEndpoint = %d, want 0", AggregateByEndpoint)
	}
	if AggregateByCluster != 1 {
		t.Errorf("AggregateByCluster = %d, want 1", AggregateByCluster)
	}
}
