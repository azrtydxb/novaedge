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

package mesh

import (
	"errors"

	"go.uber.org/zap"
)

var (
	errTPROXYIsOnlySupportedOnLinux = errors.New("TPROXY is only supported on Linux")
)

// stubBackend is used on non-Linux platforms where TPROXY is not supported.
type stubBackend struct{}

func (s *stubBackend) Name() string { return "stub" }

func (s *stubBackend) Setup() error {
	return errTPROXYIsOnlySupportedOnLinux
}

func (s *stubBackend) ApplyRules(_ []InterceptTarget, _ int32) error {
	return errTPROXYIsOnlySupportedOnLinux
}

func (s *stubBackend) Cleanup() error { return nil }

// detectBackend returns a stub backend on non-Linux platforms.
func detectBackend(_ *zap.Logger) RuleBackend {
	return &stubBackend{}
}
