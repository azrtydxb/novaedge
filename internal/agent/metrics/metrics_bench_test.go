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

// BenchmarkMetricsRecording tests the performance of metrics recording
func BenchmarkMetricsRecording(b *testing.B) {
	cluster := "default/backend"
	endpoint := "10.0.0.1:8080"

	b.Run("HTTPRequest", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			RecordHTTPRequest("GET", "200", cluster, 0.1)
		}
	})

	b.Run("BackendRequest", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			RecordBackendRequest(cluster, endpoint, "success", 0.05)
		}
	})

	b.Run("HealthCheck", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			RecordHealthCheck(cluster, endpoint, "success", 0.01)
		}
	})
}

// BenchmarkCardinalityTracking tests endpoint cardinality tracking
func BenchmarkCardinalityTracking(b *testing.B) {
	tracker := &endpointCardinalityTracker{
		endpoints: make(map[string]map[string]bool),
	}

	cluster := "default/backend"

	b.Run("ShouldTrackEndpoint", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			endpoint := "10.0.0.1:8080"
			tracker.shouldTrackEndpoint(cluster, endpoint)
		}
	})

	b.Run("ShouldTrackMultipleEndpoints", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			endpoint := "10.0.0." + string(rune(i%255)) + ":8080"
			tracker.shouldTrackEndpoint(cluster, endpoint)
		}
	})
}

// BenchmarkSampling tests metric sampling performance
func BenchmarkSampling(b *testing.B) {
	// Configure sampling
	ConfigureMetrics(MetricsConfig{
		EnableSampling:         true,
		SampleRate:             10,
		MaxEndpointCardinality: 100,
	})

	b.Run("ShouldSample", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			shouldSample("test-key")
		}
	})

	b.Run("WithSampling", func(b *testing.B) {
		cluster := "default/backend"
		endpoint := "10.0.0.1:8080"

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			RecordBackendRequest(cluster, endpoint, "success", 0.05)
		}
	})

	b.Run("WithoutSampling", func(b *testing.B) {
		// Disable sampling for this test
		ConfigureMetrics(MetricsConfig{
			EnableSampling:         false,
			SampleRate:             100,
			MaxEndpointCardinality: 100,
		})

		cluster := "default/backend"
		endpoint := "10.0.0.1:8080"

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			RecordBackendRequest(cluster, endpoint, "success", 0.05)
		}

		// Re-enable sampling after test
		ConfigureMetrics(MetricsConfig{
			EnableSampling:         true,
			SampleRate:             10,
			MaxEndpointCardinality: 100,
		})
	})
}

// BenchmarkHighCardinality tests behavior with many unique endpoints
func BenchmarkHighCardinality(b *testing.B) {
	cluster := "default/backend"

	// Reset tracker
	endpointTracker = &endpointCardinalityTracker{
		endpoints: make(map[string]map[string]bool),
	}

	ConfigureMetrics(MetricsConfig{
		EnableSampling:         false,
		SampleRate:             100,
		MaxEndpointCardinality: 100,
	})

	b.Run("UnderCardinalityLimit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			endpoint := "10.0.0." + string(rune(i%50)) + ":8080"
			RecordBackendRequest(cluster, endpoint, "success", 0.05)
		}
	})

	b.Run("OverCardinalityLimit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			endpoint := "10.0.0." + string(rune(i%200)) + ":8080"
			RecordBackendRequest(cluster, endpoint, "success", 0.05)
		}
	})
}
