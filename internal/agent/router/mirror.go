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
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/upstream"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// MirrorConfig holds configuration for traffic mirroring on a route rule.
type MirrorConfig struct {
	// BackendRef references the mirror backend (namespace/name).
	BackendRef *pb.BackendRef
	// Percentage is the percentage of requests to mirror (0-100).
	Percentage int32
	// ClusterKey is the pre-computed "namespace/name" key for the mirror
	// backend. Computed once at config time to avoid per-request string
	// formatting in the mirror hot path.
	ClusterKey string
}

// mirrorMetrics holds Prometheus metrics for traffic mirroring.
type mirrorMetrics struct {
	mu            sync.Mutex
	requestsTotal int64
	errorsTotal   int64
}

var globalMirrorMetrics = &mirrorMetrics{}

// MirrorRequestsTotal returns the total mirrored requests count.
func MirrorRequestsTotal() int64 {
	globalMirrorMetrics.mu.Lock()
	defer globalMirrorMetrics.mu.Unlock()
	return globalMirrorMetrics.requestsTotal
}

// MirrorErrorsTotal returns the total mirror error count.
func MirrorErrorsTotal() int64 {
	globalMirrorMetrics.mu.Lock()
	defer globalMirrorMetrics.mu.Unlock()
	return globalMirrorMetrics.errorsTotal
}

// shouldMirror determines whether to mirror this request based on configured percentage.
func shouldMirror(percentage int32) bool {
	if percentage <= 0 {
		return false
	}
	if percentage >= 100 {
		return true
	}
	bigRand, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return false
	}
	return bigRand.Int64() < int64(percentage)
}

// mirrorRequest clones the incoming request and sends it to the mirror backend
// asynchronously. The mirror response is discarded (fire-and-forget).
// This function does NOT block the original request flow.
func (r *Router) mirrorRequest(
	ctx context.Context,
	req *http.Request,
	mirrorCfg *MirrorConfig,
	pools map[string]*upstream.Pool,
	loadBalancers map[string]interface{},
	standardLBs map[string]interface{},
) {
	if mirrorCfg == nil || mirrorCfg.BackendRef == nil {
		return
	}

	if !shouldMirror(mirrorCfg.Percentage) {
		return
	}

	// Buffer the request body so it can be read by both original and mirror
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			r.logger.Debug("Failed to read request body for mirroring", zap.Error(err))
			return
		}
		// Restore original body for the primary request
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Clone request for mirror
	mirrorReq := req.Clone(ctx)
	if bodyBytes != nil {
		mirrorReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	mirrorReq.Header.Set("X-Mirror", "true")

	clusterKey := mirrorCfg.ClusterKey

	// Look up the pool
	pool, ok := pools[clusterKey]
	if !ok {
		r.logger.Debug("No pool for mirror cluster", zap.String("cluster", clusterKey))
		return
	}

	// Select an endpoint from the pool
	endpoint := r.selectMirrorEndpoint(clusterKey, loadBalancers, standardLBs)
	if endpoint == nil {
		r.logger.Debug("No healthy endpoint for mirror", zap.String("cluster", clusterKey))
		return
	}

	// Fire-and-forget: launch mirror in a goroutine
	go func() {
		// Use a detached context with a timeout so we don't leak goroutines,
		// but still respect the original request's cancellation.
		mirrorCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Check if the original context was already cancelled before starting
		select {
		case <-ctx.Done():
			return
		default:
		}

		mirrorReq = mirrorReq.WithContext(mirrorCtx)

		globalMirrorMetrics.mu.Lock()
		globalMirrorMetrics.requestsTotal++
		globalMirrorMetrics.mu.Unlock()

		mirrorStart := time.Now()

		// Create a discard response writer for the mirror
		discardWriter := &discardResponseWriter{}

		if err := pool.Forward(endpoint, mirrorReq, discardWriter); err != nil {
			globalMirrorMetrics.mu.Lock()
			globalMirrorMetrics.errorsTotal++
			globalMirrorMetrics.mu.Unlock()

			r.logger.Debug("Mirror request failed",
				zap.String("cluster", clusterKey),
				zap.Error(err),
				zap.Duration("duration", time.Since(mirrorStart)),
			)
		} else {
			r.logger.Debug("Mirror request succeeded",
				zap.String("cluster", clusterKey),
				zap.Duration("duration", time.Since(mirrorStart)),
			)
		}
	}()
}

// selectMirrorEndpoint selects an endpoint for the mirror backend.
func (r *Router) selectMirrorEndpoint(
	clusterKey string,
	loadBalancers map[string]interface{},
	standardLBs map[string]interface{},
) *pb.Endpoint {
	// Try standard load balancer first
	if lbVal, ok := standardLBs[clusterKey]; ok {
		type standardLB interface {
			Select() *pb.Endpoint
		}
		if lb, ok := lbVal.(standardLB); ok {
			return lb.Select()
		}
	}

	// Try hash-based load balancer (just select without key)
	if lbVal, ok := loadBalancers[clusterKey]; ok {
		type standardLB interface {
			Select() *pb.Endpoint
		}
		if lb, ok := lbVal.(standardLB); ok {
			return lb.Select()
		}
	}

	return nil
}

// discardResponseWriter is an http.ResponseWriter that discards all output.
// Used for mirror requests where we don't care about the response.
type discardResponseWriter struct {
	statusCode int
}

func (w *discardResponseWriter) Header() http.Header {
	return http.Header{}
}

func (w *discardResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *discardResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}
