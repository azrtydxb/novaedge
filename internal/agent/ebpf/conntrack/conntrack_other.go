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

package conntrack

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Conntrack is a stub on non-Linux platforms.
type Conntrack struct{}

// NewConntrack returns an error on non-Linux platforms.
func NewConntrack(_ *zap.Logger, _ uint32, _ time.Duration) (*Conntrack, error) {
	return nil, fmt.Errorf("eBPF conntrack is only supported on Linux")
}

// Lookup returns an error on non-Linux platforms.
func (ct *Conntrack) Lookup(_ CTKey) (*CTEntry, error) {
	return nil, fmt.Errorf("eBPF conntrack is only supported on Linux")
}

// GarbageCollect returns an error on non-Linux platforms.
func (ct *Conntrack) GarbageCollect(_ time.Duration) (int, error) {
	return 0, fmt.Errorf("eBPF conntrack is only supported on Linux")
}

// StartGC is a no-op on non-Linux platforms.
func (ct *Conntrack) StartGC() {}

// Stats returns an empty map on non-Linux platforms.
func (ct *Conntrack) Stats() map[string]uint64 {
	return map[string]uint64{}
}

// Close is a no-op on non-Linux platforms.
func (ct *Conntrack) Close() error { return nil }
