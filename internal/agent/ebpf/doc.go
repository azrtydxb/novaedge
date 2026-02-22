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

// Package ebpf provides shared infrastructure for eBPF/XDP-based data plane
// acceleration in NovaEdge. It includes kernel capability detection, BPF
// program loading and lifecycle management, typed map helpers, and Prometheus
// metrics. All functionality is Linux-only; non-Linux platforms receive
// compile-time stubs that return appropriate errors.
package ebpf
