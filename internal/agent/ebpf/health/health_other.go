//go:build !linux

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

package health

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// HealthMonitor is a stub on non-Linux platforms.
type HealthMonitor struct{}

// NewHealthMonitor returns an error on non-Linux platforms since eBPF
// health monitoring requires Linux kernel support.
func NewHealthMonitor(_ *zap.Logger, _ uint32) (*HealthMonitor, error) {
	return nil, fmt.Errorf("eBPF health monitoring is only supported on Linux")
}

// Poll returns an error on non-Linux platforms.
func (hm *HealthMonitor) Poll() (map[BackendKey]AggregatedHealth, error) {
	return nil, fmt.Errorf("eBPF health monitoring is only supported on Linux")
}

// StartPoller is a no-op on non-Linux platforms.
func (hm *HealthMonitor) StartPoller(_ context.Context, _ time.Duration, _ func(map[BackendKey]AggregatedHealth)) {
}

// IsActive returns false on non-Linux platforms.
func (hm *HealthMonitor) IsActive() bool { return false }

// Close is a no-op on non-Linux platforms.
func (hm *HealthMonitor) Close() error { return nil }
