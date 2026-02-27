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

// Package testutil provides shared test utilities for eBPF packages.
package testutil

import (
	"strings"
	"testing"
)

// SkipIfBPFUnavailable skips the test if the error indicates that BPF
// operations are unavailable (insufficient privileges or MEMLOCK limits).
func SkipIfBPFUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "MEMLOCK") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "EPERM") {
		t.Skipf("Skipping: eBPF unavailable (insufficient privileges): %v", err)
	}
}
