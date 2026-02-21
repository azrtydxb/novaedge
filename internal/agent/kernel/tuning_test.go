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

package kernel

import (
	"runtime"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestGetRecommendedSysctls_NonEmpty(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GetRecommendedSysctls returns empty map on non-Linux platforms")
	}

	sysctls := GetRecommendedSysctls()

	if len(sysctls) == 0 {
		t.Fatal("expected non-empty sysctl map on Linux")
	}
}

func TestGetRecommendedSysctls_MinimumEntries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GetRecommendedSysctls returns empty map on non-Linux platforms")
	}

	sysctls := GetRecommendedSysctls()

	const minExpected = 15
	if len(sysctls) < minExpected {
		t.Fatalf("expected at least %d sysctl entries, got %d", minExpected, len(sysctls))
	}
}

func TestGetRecommendedSysctls_ExpectedKeys(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GetRecommendedSysctls returns empty map on non-Linux platforms")
	}

	sysctls := GetRecommendedSysctls()

	expectedKeys := []string{
		"net.core.somaxconn",
		"net.core.netdev_max_backlog",
		"net.ipv4.tcp_max_syn_backlog",
		"net.core.rmem_max",
		"net.core.wmem_max",
		"net.ipv4.tcp_rmem",
		"net.ipv4.tcp_wmem",
		"net.ipv4.tcp_fin_timeout",
		"net.ipv4.tcp_tw_reuse",
		"net.ipv4.ip_local_port_range",
		"net.ipv4.tcp_fastopen",
		"net.ipv4.tcp_slow_start_after_idle",
		"net.ipv4.tcp_keepalive_time",
		"net.ipv4.tcp_keepalive_intvl",
		"net.ipv4.tcp_keepalive_probes",
		"net.core.optmem_max",
		"net.ipv4.tcp_max_tw_buckets",
		"net.ipv4.tcp_notsent_lowat",
	}

	for _, key := range expectedKeys {
		if _, ok := sysctls[key]; !ok {
			t.Errorf("expected sysctl key %q not found in recommended map", key)
		}
	}
}

func TestGetRecommendedSysctls_ExpectedValues(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GetRecommendedSysctls returns empty map on non-Linux platforms")
	}

	sysctls := GetRecommendedSysctls()

	expectedValues := map[string]string{
		"net.core.somaxconn":       "65535",
		"net.ipv4.tcp_tw_reuse":    "1",
		"net.ipv4.tcp_fastopen":    "3",
		"net.core.rmem_max":        "16777216",
		"net.core.wmem_max":        "16777216",
		"net.ipv4.tcp_fin_timeout": "10",
	}

	for key, expected := range expectedValues {
		got, ok := sysctls[key]
		if !ok {
			t.Errorf("expected key %q not found", key)
			continue
		}
		if got != expected {
			t.Errorf("sysctl %q: expected %q, got %q", key, expected, got)
		}
	}
}

func TestCheckKernelParameters_NoPanic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// CheckKernelParameters should not panic regardless of platform.
	// On non-Linux it is a no-op; on Linux it reads /proc/sys which
	// may or may not be accessible in CI.
	CheckKernelParameters(logger)
}

func TestGetRecommendedSysctls_NonLinux_Empty(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux behavior")
	}

	sysctls := GetRecommendedSysctls()

	if len(sysctls) != 0 {
		t.Fatalf("expected empty sysctl map on non-Linux platform, got %d entries", len(sysctls))
	}
}
