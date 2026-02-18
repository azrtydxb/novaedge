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

package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// testLogger returns a no-op logger suitable for unit tests.
func testLogger() *zap.Logger {
	return zap.NewNop()
}

// endpointPool returns a selectEndpoint function that cycles through the
// provided URLs, returning a different one each call.
func endpointPool(urls ...*url.URL) func() (*url.URL, error) {
	var idx int64
	return func() (*url.URL, error) {
		i := atomic.AddInt64(&idx, 1) - 1
		if int(i) >= len(urls) {
			return nil, errors.New("no more endpoints")
		}
		return urls[i], nil
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestHedgingConfig_ApplyDefaults(t *testing.T) {
	cfg := &HedgingConfig{}
	cfg.applyDefaults()

	if cfg.InitialRequests != defaultHedgingInitialRequests {
		t.Errorf("InitialRequests = %d, want %d", cfg.InitialRequests, defaultHedgingInitialRequests)
	}
	if cfg.MaxConcurrent != defaultHedgingMaxConcurrent {
		t.Errorf("MaxConcurrent = %d, want %d", cfg.MaxConcurrent, defaultHedgingMaxConcurrent)
	}
	if cfg.PerTryTimeout != defaultHedgingPerTryTimeout {
		t.Errorf("PerTryTimeout = %v, want %v", cfg.PerTryTimeout, defaultHedgingPerTryTimeout)
	}
}

func TestHedgingConfig_CustomValues(t *testing.T) {
	cfg := &HedgingConfig{
		InitialRequests: 2,
		MaxConcurrent:   4,
		PerTryTimeout:   500 * time.Millisecond,
	}
	cfg.applyDefaults()

	if cfg.InitialRequests != 2 {
		t.Errorf("InitialRequests = %d, want 2", cfg.InitialRequests)
	}
	if cfg.MaxConcurrent != 4 {
		t.Errorf("MaxConcurrent = %d, want 4", cfg.MaxConcurrent)
	}
	if cfg.PerTryTimeout != 500*time.Millisecond {
		t.Errorf("PerTryTimeout = %v, want 500ms", cfg.PerTryTimeout)
	}
}

func TestHedging_DisabledForwardsDirect(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled: false,
	}, testLogger())

	ep := mustParseURL("http://backend1:8080")
	selectEP := func() (*url.URL, error) {
		return ep, nil
	}

	var called int64
	doReq := func(_ context.Context, endpoint *url.URL, _ *http.Request) (*http.Response, error) {
		atomic.AddInt64(&called, 1)
		if endpoint.String() != ep.String() {
			t.Errorf("unexpected endpoint %s", endpoint)
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := handler.Execute(context.Background(), req, selectEP, doReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt64(&called) != 1 {
		t.Errorf("doRequest called %d times, want 1", atomic.LoadInt64(&called))
	}
}

func TestHedging_NoHedgeWhenInitialSucceedsQuickly(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        500 * time.Millisecond,
	}, testLogger())

	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
	)

	var called int64
	doReq := func(_ context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		atomic.AddInt64(&called, 1)
		// Respond immediately -- well before the 500ms hedge timeout.
		return &http.Response{StatusCode: http.StatusOK}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	resp, err := handler.Execute(context.Background(), req, eps, doReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Only the initial request should have been sent.
	if atomic.LoadInt64(&called) != 1 {
		t.Errorf("doRequest called %d times, want 1", atomic.LoadInt64(&called))
	}
}

func TestHedging_HedgedRequestSentAfterTimeout(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        50 * time.Millisecond,
	}, testLogger())

	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
	)

	var callCount int64
	doReq := func(ctx context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		n := atomic.AddInt64(&callCount, 1)
		if n == 1 {
			// First request: slow -- exceeds the hedge timeout.
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, ctx.Err()
		}
		// Hedged request: respond immediately.
		return &http.Response{StatusCode: http.StatusOK}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	resp, err := handler.Execute(context.Background(), req, eps, doReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Both the initial and the hedged request should have been launched.
	if atomic.LoadInt64(&callCount) < 2 {
		t.Errorf("doRequest called %d times, want >= 2", atomic.LoadInt64(&callCount))
	}
}

func TestHedging_FirstResponseWinsOtherCancelled(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        10 * time.Millisecond,
	}, testLogger())

	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
	)

	cancelledCtxs := make(chan context.Context, 2)
	doReq := func(ctx context.Context, ep *url.URL, _ *http.Request) (*http.Response, error) {
		cancelledCtxs <- ctx
		if ep.Host == "backend1:8080" {
			// Original: slow
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, ctx.Err()
		}
		// Hedged: fast winner
		return &http.Response{StatusCode: http.StatusOK}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/race", nil)
	resp, err := handler.Execute(context.Background(), req, eps, doReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// The losing context should be cancelled shortly.
	select {
	case ctx := <-cancelledCtxs:
		// Give a moment for cancellation to propagate.
		select {
		case <-ctx.Done():
			// good -- cancelled
		case <-time.After(2 * time.Second):
			t.Error("expected losing request context to be cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for context from losing request")
	}
}

func TestHedging_MaxConcurrentRespected(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        10 * time.Millisecond,
	}, testLogger())

	// Provide 3 endpoints but MaxConcurrent is 2.
	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
		mustParseURL("http://backend3:8080"),
	)

	var callCount int64
	doReq := func(ctx context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		n := atomic.AddInt64(&callCount, 1)
		if n == 1 {
			// Slow initial request.
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, ctx.Err()
		}
		// Hedged responds.
		return &http.Response{StatusCode: http.StatusOK}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/maxconc", nil)
	resp, err := handler.Execute(context.Background(), req, eps, doReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// At most MaxConcurrent (2) requests should have been launched.
	total := atomic.LoadInt64(&callCount)
	if total > 2 {
		t.Errorf("doRequest called %d times, want <= 2 (MaxConcurrent)", total)
	}
}

func TestHedging_AllRequestsFail(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        10 * time.Millisecond,
	}, testLogger())

	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
	)

	errBackend := errors.New("backend unavailable")
	doReq := func(_ context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		return nil, errBackend
	}

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	resp, err := handler.Execute(context.Background(), req, eps, doReq)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err == nil {
		t.Fatal("expected error when all requests fail")
	}
	if !errors.Is(err, errBackend) {
		t.Errorf("error = %v, want %v", err, errBackend)
	}
}

func TestHedging_ContextCancellation(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:              true,
		InitialRequests:      1,
		MaxConcurrent:        2,
		HedgeOnPerTryTimeout: true,
		PerTryTimeout:        50 * time.Millisecond,
	}, testLogger())

	eps := endpointPool(
		mustParseURL("http://backend1:8080"),
		mustParseURL("http://backend2:8080"),
	)

	doReq := func(ctx context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		// Block until context is cancelled.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/cancel", nil)
	resp, err := handler.Execute(ctx, req, eps, doReq)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestHedging_SelectEndpointError(t *testing.T) {
	handler := NewHedgingHandler(HedgingConfig{
		Enabled:         true,
		InitialRequests: 1,
		MaxConcurrent:   2,
		PerTryTimeout:   50 * time.Millisecond,
	}, testLogger())

	epErr := errors.New("no healthy endpoints")
	selectEP := func() (*url.URL, error) {
		return nil, epErr
	}

	doReq := func(_ context.Context, _ *url.URL, _ *http.Request) (*http.Response, error) {
		t.Fatal("doRequest should not be called when selectEndpoint fails")
		return nil, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/noep", nil)
	resp, err := handler.Execute(context.Background(), req, selectEP, doReq)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if !errors.Is(err, epErr) {
		t.Errorf("error = %v, want %v", err, epErr)
	}
}
