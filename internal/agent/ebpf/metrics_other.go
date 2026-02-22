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

// RecordError is a no-op on non-Linux platforms.
func RecordError(_, _ string) {}

// RecordProgramLoaded is a no-op on non-Linux platforms.
func RecordProgramLoaded(_ string) {}

// RecordProgramUnloaded is a no-op on non-Linux platforms.
func RecordProgramUnloaded(_ string) {}

// RecordMapOp is a no-op on non-Linux platforms.
func RecordMapOp(_, _, _ string) {}

// ObserveAttachDuration is a no-op on non-Linux platforms.
func ObserveAttachDuration(_ string, _ float64) {}
