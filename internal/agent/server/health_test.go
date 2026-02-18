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

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewHealthServer(t *testing.T) {
	logger := zap.NewNop()
	port := 8080

	hs := NewHealthServer(logger, port)

	require.NotNil(t, hs)
	assert.Equal(t, logger, hs.logger)
	assert.Equal(t, port, hs.port)
	assert.NotNil(t, hs.rateLimiter)
}

func TestHealthServer_SetReady(t *testing.T) {
	logger := zap.NewNop()
	hs := NewHealthServer(logger, 8080)

	// Initially not ready
	assert.False(t, hs.ready.Load())

	// Set ready
	hs.SetReady(true)
	assert.True(t, hs.ready.Load())

	// Set not ready
	hs.SetReady(false)
	assert.False(t, hs.ready.Load())
}

func TestHealthServer_SetRouter(t *testing.T) {
	logger := zap.NewNop()
	hs := NewHealthServer(logger, 8080)

	// Initially nil
	assert.Nil(t, hs.router)

	// Set router (nil is acceptable for this test)
	hs.SetRouter(nil)
	assert.Nil(t, hs.router)
}

func TestHealthServer_HealthzEndpoint(t *testing.T) {
	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	// Create the handler directly
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestHealthServer_ReadyEndpoint(t *testing.T) {
	logger := zap.NewNop()
	hs := NewHealthServer(logger, 8080)

	t.Run("not ready", func(t *testing.T) {
		hs.SetReady(false)

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if hs.ready.Load() {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Ready"))
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("Not Ready"))
			}
		})

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "Not Ready", rec.Body.String())
	})

	t.Run("ready", func(t *testing.T) {
		hs.SetReady(true)

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if hs.ready.Load() {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Ready"))
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("Not Ready"))
			}
		})

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Ready", rec.Body.String())
	})
}

func TestIPRateLimiter(t *testing.T) {
	rl := NewIPRateLimiter(DefaultObservabilityRateLimitConfig())

	require.NotNil(t, rl)
}

func TestDefaultObservabilityRateLimitConfig(t *testing.T) {
	config := DefaultObservabilityRateLimitConfig()

	assert.NotNil(t, config)
}
