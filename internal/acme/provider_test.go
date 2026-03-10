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

package acme

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewHTTP01Provider(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		logger := zap.NewNop()
		provider := NewHTTP01Provider(logger)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.Equal(t, logger, provider.logger)
	})

	t.Run("with nil logger", func(t *testing.T) {
		provider := NewHTTP01Provider(nil)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.NotNil(t, provider.logger)
	})
}

func TestHTTP01Provider_Present(t *testing.T) {
	provider := NewHTTP01Provider(zap.NewNop())

	err := provider.Present("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	keyAuth, ok := provider.GetKeyAuth("test-token")
	assert.True(t, ok)
	assert.Equal(t, "test-key-auth", keyAuth)
}

func TestHTTP01Provider_CleanUp(t *testing.T) {
	provider := NewHTTP01Provider(zap.NewNop())

	// Add a challenge first
	err := provider.Present("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	// Clean up the challenge
	err = provider.CleanUp("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	// Verify it's gone
	_, ok := provider.GetKeyAuth("test-token")
	assert.False(t, ok)
}

func TestHTTP01Provider_GetKeyAuth(t *testing.T) {
	provider := NewHTTP01Provider(zap.NewNop())

	t.Run("existing token", func(t *testing.T) {
		_ = provider.Present("example.com", "token1", "keyauth1")
		keyAuth, ok := provider.GetKeyAuth("token1")
		assert.True(t, ok)
		assert.Equal(t, "keyauth1", keyAuth)
	})

	t.Run("non-existing token", func(t *testing.T) {
		keyAuth, ok := provider.GetKeyAuth("non-existing-token")
		assert.False(t, ok)
		assert.Empty(t, keyAuth)
	})
}

func TestHTTP01Provider_Handler(t *testing.T) {
	provider := NewHTTP01Provider(zap.NewNop())
	handler := provider.Handler()
	assert.NotNil(t, handler)

	t.Run("existing challenge", func(t *testing.T) {
		_ = provider.Present("example.com", "test-token", "test-key-auth-value")

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test-token", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
		assert.Equal(t, "test-key-auth-value", rec.Body.String())
	})

	t.Run("non-existing challenge", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/unknown-token", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("with leading slash", func(t *testing.T) {
		_ = provider.Present("example.com", "slash-token", "slash-key-auth")

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/slash-token", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "slash-key-auth", rec.Body.String())
	})
}

func TestNewTLSALPN01Provider(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		logger := zap.NewNop()
		provider := NewTLSALPN01Provider(logger)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.Equal(t, logger, provider.logger)
	})

	t.Run("with nil logger", func(t *testing.T) {
		provider := NewTLSALPN01Provider(nil)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.NotNil(t, provider.logger)
	})
}

func TestTLSALPN01Provider_Present(t *testing.T) {
	provider := NewTLSALPN01Provider(zap.NewNop())

	err := provider.Present("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	ch, ok := provider.GetChallenge("example.com")
	assert.True(t, ok)
	assert.Equal(t, "example.com", ch.Domain)
	assert.Equal(t, "test-key-auth", ch.KeyAuth)
}

func TestTLSALPN01Provider_CleanUp(t *testing.T) {
	provider := NewTLSALPN01Provider(zap.NewNop())

	// Add a challenge first
	err := provider.Present("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	// Clean up the challenge
	err = provider.CleanUp("example.com", "test-token", "test-key-auth")
	assert.NoError(t, err)

	// Verify it's gone
	_, ok := provider.GetChallenge("example.com")
	assert.False(t, ok)
}

func TestTLSALPN01Provider_GetChallenge(t *testing.T) {
	provider := NewTLSALPN01Provider(zap.NewNop())

	t.Run("existing domain", func(t *testing.T) {
		_ = provider.Present("example.com", "token1", "keyauth1")
		ch, ok := provider.GetChallenge("example.com")
		assert.True(t, ok)
		assert.Equal(t, "example.com", ch.Domain)
		assert.Equal(t, "keyauth1", ch.KeyAuth)
	})

	t.Run("non-existing domain", func(t *testing.T) {
		ch, ok := provider.GetChallenge("non-existing.com")
		assert.False(t, ok)
		assert.Nil(t, ch)
	})
}

func TestNewDNS01Provider(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		logger := zap.NewNop()
		provider := NewDNS01Provider(logger)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.Equal(t, logger, provider.logger)
	})

	t.Run("with nil logger", func(t *testing.T) {
		provider := NewDNS01Provider(nil)
		assert.NotNil(t, provider)
		assert.NotNil(t, provider.challenges)
		assert.NotNil(t, provider.logger)
	})
}

func TestDNS01Provider_GetInfo(t *testing.T) {
	provider := NewDNS01Provider(zap.NewNop())

	t.Run("existing domain", func(t *testing.T) {
		challenge := &DNSChallenge{
			Domain:      "example.com",
			Token:       "token1",
			KeyAuth:     "keyauth1",
			RecordName:  "_acme-challenge.example.com",
			RecordValue: "record-value",
		}
		provider.SetChallenge("example.com", challenge)

		ch, ok := provider.GetInfo("example.com")
		assert.True(t, ok)
		assert.Equal(t, "example.com", ch.Domain)
		assert.Equal(t, "token1", ch.Token)
		assert.Equal(t, "keyauth1", ch.KeyAuth)
	})

	t.Run("non-existing domain", func(t *testing.T) {
		ch, ok := provider.GetInfo("non-existing.com")
		assert.False(t, ok)
		assert.Nil(t, ch)
	})
}

func TestDNS01Provider_SetChallenge(t *testing.T) {
	provider := NewDNS01Provider(zap.NewNop())

	challenge := &DNSChallenge{
		Domain:      "example.com",
		Token:       "test-token",
		KeyAuth:     "test-keyauth",
		RecordName:  "_acme-challenge.example.com",
		RecordValue: "test-value",
	}

	provider.SetChallenge("example.com", challenge)

	ch, ok := provider.GetInfo("example.com")
	assert.True(t, ok)
	assert.Equal(t, challenge, ch)
}

func TestDNS01Provider_RemoveChallenge(t *testing.T) {
	provider := NewDNS01Provider(zap.NewNop())

	// Add a challenge first
	challenge := &DNSChallenge{
		Domain:  "example.com",
		Token:   "test-token",
		KeyAuth: "test-key-auth",
	}
	provider.SetChallenge("example.com", challenge)

	// Verify it exists
	_, ok := provider.GetInfo("example.com")
	assert.True(t, ok)

	// Remove the challenge
	provider.RemoveChallenge("example.com")

	// Verify it's gone
	_, ok = provider.GetInfo("example.com")
	assert.False(t, ok)
}

func TestDNS01Provider_ConcurrentAccess(_ *testing.T) {
	provider := NewDNS01Provider(zap.NewNop())

	// Test concurrent SetChallenge operations
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			domain := "example" + fmt.Sprintf("%d", idx) + ".com"
			challenge := &DNSChallenge{
				Domain:  domain,
				Token:   "token",
				KeyAuth: "keyauth",
			}
			provider.SetChallenge(domain, challenge)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test concurrent GetInfo operations
	for i := 0; i < 10; i++ {
		go func(idx int) {
			domain := "example" + fmt.Sprintf("%d", idx) + ".com"
			_, _ = provider.GetInfo(domain)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
