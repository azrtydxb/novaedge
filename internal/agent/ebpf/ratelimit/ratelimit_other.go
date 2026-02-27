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

package ratelimit

import (
	"errors"
	"net"

	"go.uber.org/zap"
)

var (
	errEBPFRateLimitingIsOnlySupportedOnLinux = errors.New("eBPF rate limiting is only supported on Linux")
)

// RateLimiter is a stub on non-Linux platforms.
type RateLimiter struct{}

// NewRateLimiter returns an error on non-Linux platforms since eBPF
// rate limiting requires Linux kernel support.
func NewRateLimiter(_ *zap.Logger, _ uint32) (*RateLimiter, error) {
	return nil, errEBPFRateLimitingIsOnlySupportedOnLinux
}

// Configure returns an error on non-Linux platforms.
func (rl *RateLimiter) Configure(_, _ uint64) error {
	return errEBPFRateLimitingIsOnlySupportedOnLinux
}

// CheckAllowed always returns true on non-Linux platforms.
func (rl *RateLimiter) CheckAllowed(_ net.IP) (bool, error) {
	return true, errEBPFRateLimitingIsOnlySupportedOnLinux
}

// GetStats returns empty stats on non-Linux platforms.
func (rl *RateLimiter) GetStats() (RateLimitStats, error) {
	return RateLimitStats{}, errEBPFRateLimitingIsOnlySupportedOnLinux
}

// IsActive returns false on non-Linux platforms.
func (rl *RateLimiter) IsActive() bool { return false }

// Close is a no-op on non-Linux platforms.
func (rl *RateLimiter) Close() error { return nil }
