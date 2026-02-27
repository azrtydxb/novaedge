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

package vault

import (
	"errors"
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"
)
var (
	errVaultIsSealed = errors.New("vault is sealed")
	errVaultIsNotInitialized = errors.New("vault is not initialized")
	errVaultIsInStandbyMode = errors.New("vault is in standby mode")
	errVaultIsUnhealthyStatusCode = errors.New("vault is unhealthy (status code")
)


// HealthChecker provides health check endpoints for Vault connectivity.
type HealthChecker struct {
	client *Client
	logger *zap.Logger
}

// NewHealthChecker creates a new Vault health checker.
func NewHealthChecker(client *Client, logger *zap.Logger) *HealthChecker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &HealthChecker{
		client: client,
		logger: logger,
	}
}

// Check performs a health check against Vault.
// Returns nil if Vault is healthy.
func (h *HealthChecker) Check(ctx context.Context) error {
	status, err := h.client.httpClient.HealthCheck(ctx)
	if err != nil {
		return fmt.Errorf("vault health check failed: %w", err)
	}

	if !status.IsHealthy() {
		if status.Sealed {
			return errVaultIsSealed
		}
		if !status.Initialized {
			return errVaultIsNotInitialized
		}
		if status.Standby {
			return errVaultIsInStandbyMode
		}
		return fmt.Errorf("%w: %d)", errVaultIsUnhealthyStatusCode, status.StatusCode)
	}

	return nil
}

// Handler returns an HTTP handler that reports Vault health.
func (h *HealthChecker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.Check(r.Context()); err != nil {
			h.logger.Warn("Vault health check failed", zap.Error(err))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, "vault unhealthy: %s", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "vault healthy")
	})
}

// CheckerFunc returns a function compatible with controller-runtime's healthz.Checker.
func (h *HealthChecker) CheckerFunc() func(*http.Request) error {
	return func(r *http.Request) error {
		return h.Check(r.Context())
	}
}
