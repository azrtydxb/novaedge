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

// Package service provides eBPF-based service map management for L4 load balancing.
package service

import (
	"errors"

	"go.uber.org/zap"
)

var (
	errEBPFServiceMapsAreOnlySupportedOnLinux = errors.New("eBPF service maps are only supported on Linux")
)

// Map is a stub on non-Linux platforms where eBPF is not available.
type Map struct{}

// NewMap returns an error on non-Linux platforms since eBPF service
// maps require Linux kernel support.
func NewMap(_ *zap.Logger, _, _ uint32) (*Map, error) {
	return nil, errEBPFServiceMapsAreOnlySupportedOnLinux
}

// UpsertService returns an error on non-Linux platforms.
func (sm *Map) UpsertService(_ Key, _ []BackendInfo) error {
	return errEBPFServiceMapsAreOnlySupportedOnLinux
}

// DeleteService returns an error on non-Linux platforms.
func (sm *Map) DeleteService(_ Key) error {
	return errEBPFServiceMapsAreOnlySupportedOnLinux
}

// Reconcile returns an error on non-Linux platforms.
func (sm *Map) Reconcile(_ map[Key][]BackendInfo) error {
	return errEBPFServiceMapsAreOnlySupportedOnLinux
}

// Close is a no-op on non-Linux platforms.
func (sm *Map) Close() error {
	return nil
}
