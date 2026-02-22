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

package ebpf

import "go.uber.org/zap"

// Capabilities describes the eBPF features available on the running kernel.
type Capabilities struct {
	HasXDP        bool
	HasSKLookup   bool
	HasAFXDP      bool
	HasBTF        bool
	HasLPMTrie    bool
	KernelVersion string
}

// Detect returns empty capabilities on non-Linux platforms. eBPF is only
// supported on Linux.
func Detect() (*Capabilities, error) {
	return &Capabilities{}, nil
}

// LogCapabilities logs that eBPF is not available on this platform.
func LogCapabilities(logger *zap.Logger, _ *Capabilities) {
	logger.Info("eBPF is not supported on this platform")
}
