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
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// defaultHedgingInitialRequests is the default number of initial (non-hedged) requests.
const defaultHedgingInitialRequests = 1

// defaultHedgingMaxConcurrent is the default maximum number of concurrent
// requests (initial + hedged).
const defaultHedgingMaxConcurrent = 2

// defaultHedgingPerTryTimeout is the default per-try timeout before a hedge is triggered.
const defaultHedgingPerTryTimeout = 100 * time.Millisecond

// HedgingConfig holds configuration for request hedging.
type HedgingConfig struct {
	// Enabled controls whether request hedging is active.
	Enabled bool

	// InitialRequests is the number of initial requests to send before
	// considering hedging. Defaults to 1.
	InitialRequests int

	// MaxConcurrent is the maximum number of in-flight requests allowed
	// (initial + hedged). Defaults to 2.
	MaxConcurrent int

	// HedgeOnPerTryTimeout controls whether a hedged request is sent when
	// the per-try timeout elapses. Defaults to true.
	HedgeOnPerTryTimeout bool

	// PerTryTimeout is how long to wait for the initial request before
	// sending a hedged request to a different endpoint.
	PerTryTimeout time.Duration
}

// applyDefaults fills in zero-value fields with sensible defaults.
func (c *HedgingConfig) applyDefaults() {
	if c.InitialRequests <= 0 {
		c.InitialRequests = defaultHedgingInitialRequests
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = defaultHedgingMaxConcurrent
	}
	if c.PerTryTimeout <= 0 {
		c.PerTryTimeout = defaultHedgingPerTryTimeout
	}
}

// hedgingResult holds the outcome of one leg (original or hedged) of the race.
type hedgingResult struct {
	resp    *http.Response
	err     error
	isHedge bool
}

// HedgingHandler races an original request against hedged copies that are sent
// to different endpoints when the per-try timeout elapses. The first successful
// response wins; all other in-flight requests are cancelled.
type HedgingHandler struct {
	config HedgingConfig
	logger *zap.Logger
}

// NewHedgingHandler creates a HedgingHandler with the given configuration.
// Zero-value fields in cfg are replaced with defaults.
func NewHedgingHandler(cfg HedgingConfig, logger *zap.Logger) *HedgingHandler {
	cfg.applyDefaults()
	return &HedgingHandler{
		config: cfg,
		logger: logger,
	}
}

// Execute sends the initial request and, if it does not complete within
// PerTryTimeout, sends hedged requests to different endpoints (up to
// MaxConcurrent total). The first successful response is returned; all other
// in-flight requests are cancelled.
//
// selectEndpoint returns the URL of a backend to try. It must return a
// different endpoint on each call when possible.
//
// doRequest performs the actual HTTP round-trip to the given endpoint.
func (h *HedgingHandler) Execute(
	ctx context.Context,
	req *http.Request,
	selectEndpoint func() (*url.URL, error),
	doRequest func(ctx context.Context, endpoint *url.URL, req *http.Request) (*http.Response, error),
) (*http.Response, error) {
	// When hedging is disabled, just forward directly.
	if !h.config.Enabled {
		ep, err := selectEndpoint()
		if err != nil {
			return nil, err
		}
		return doRequest(ctx, ep, req)
	}

	resultCh := make(chan hedgingResult, h.config.MaxConcurrent)

	// masterCtx governs all legs; cancelling it tears down every in-flight request.
	masterCtx, masterCancel := context.WithCancel(ctx)
	defer masterCancel()

	var wg sync.WaitGroup
	inflight := 0

	// launch starts a single request leg in its own goroutine.
	launch := func(isHedge bool) error {
		ep, err := selectEndpoint()
		if err != nil {
			return err
		}

		legCtx, legCancel := context.WithCancel(masterCtx)
		inflight++
		wg.Add(1)

		go func() {
			defer wg.Done()
			defer legCancel()

			resp, doErr := doRequest(legCtx, ep, req)
			resultCh <- hedgingResult{resp: resp, err: doErr, isHedge: isHedge}
		}()

		if isHedge {
			metrics.HedgingRequestsTotal.Inc()
			if ce := h.logger.Check(zap.DebugLevel, "Sent hedged request"); ce != nil {
				ce.Write(zap.String("endpoint", ep.String()))
			}
		}

		return nil
	}

	// Send the initial request(s).
	for i := 0; i < h.config.InitialRequests && inflight < h.config.MaxConcurrent; i++ {
		if err := launch(false); err != nil {
			// If we haven't launched anything yet, propagate the error.
			if inflight == 0 {
				return nil, err
			}
			break
		}
	}

	// hedgeTimer fires when we should send a hedged request.
	var hedgeTimer *time.Timer
	var hedgeC <-chan time.Time
	if h.config.HedgeOnPerTryTimeout && inflight < h.config.MaxConcurrent {
		hedgeTimer = time.NewTimer(h.config.PerTryTimeout)
		hedgeC = hedgeTimer.C
		defer hedgeTimer.Stop()
	}

	// Collect results until we get a success or exhaust all legs.
	var lastErr error
	received := 0

	for received < inflight {
		select {
		case <-ctx.Done():
			masterCancel()
			return nil, ctx.Err()

		case <-hedgeC:
			// Per-try timeout elapsed; send a hedged request if allowed.
			hedgeC = nil // disarm so we only hedge once
			if inflight < h.config.MaxConcurrent {
				if err := launch(true); err != nil {
					if ce := h.logger.Check(zap.DebugLevel, "Failed to launch hedged request"); ce != nil {
						ce.Write(zap.Error(err))
					}
				}
			}

		case res := <-resultCh:
			received++

			if res.err != nil {
				lastErr = res.err
				// If there are more legs still in flight, keep waiting.
				continue
			}

			// We have a winner. Cancel everything else.
			masterCancel()

			if res.isHedge {
				metrics.HedgingWins.Inc()
			} else {
				// The original won; the hedged copy (if any) will be cancelled.
				metrics.HedgingCancelled.Inc()
			}

			// Drain remaining results in the background so goroutines can exit.
			go func() {
				for range resultCh {
					// discard
				}
			}()
			go func() {
				wg.Wait()
				close(resultCh)
			}()

			return res.resp, nil
		}
	}

	// All legs failed.
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("hedging: all requests failed with no error")
}
