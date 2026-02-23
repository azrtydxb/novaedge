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
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultDrainTimeout is the default maximum time to wait for active
	// connections to complete during a graceful drain.
	DefaultDrainTimeout = 30 * time.Second
)

// DrainManager coordinates graceful connection draining during configuration
// reloads. It tracks active connections and, when draining is initiated,
// signals existing connections to close while allowing them time to finish
// in-progress work.
type DrainManager struct {
	logger       *zap.Logger
	drainTimeout time.Duration
	draining     atomic.Bool

	mu          sync.Mutex
	activeCount int64
	zeroCond    *sync.Cond
}

// NewDrainManager creates a DrainManager with the given timeout.
// If timeout is zero, DefaultDrainTimeout is used.
func NewDrainManager(logger *zap.Logger, timeout time.Duration) *DrainManager {
	if timeout <= 0 {
		timeout = DefaultDrainTimeout
	}
	dm := &DrainManager{
		logger:       logger,
		drainTimeout: timeout,
	}
	dm.zeroCond = sync.NewCond(&dm.mu)
	return dm
}

// StartDrain initiates the drain process. It sets the draining flag, then
// blocks until all tracked connections have completed or the timeout expires.
// The provided context can be used for early cancellation.
func (dm *DrainManager) StartDrain(ctx context.Context) {
	dm.draining.Store(true)
	active := atomic.LoadInt64(&dm.activeCount)
	dm.logger.Info("Connection draining started",
		zap.Duration("timeout", dm.drainTimeout),
		zap.Int64("active_connections", active),
	)

	timeoutCtx, cancel := context.WithTimeout(ctx, dm.drainTimeout)
	defer cancel()

	// Wait for all active connections to complete or for the timeout.
	// activeCount is modified atomically (without the mutex) so we use
	// atomic.LoadInt64 inside the wait loop.
	done := make(chan struct{})
	go func() {
		dm.mu.Lock()
		for atomic.LoadInt64(&dm.activeCount) > 0 {
			dm.zeroCond.Wait()
		}
		dm.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		dm.logger.Info("All active connections drained successfully")
	case <-timeoutCtx.Done():
		remaining := atomic.LoadInt64(&dm.activeCount)
		dm.logger.Warn("Drain timeout reached, proceeding with config swap",
			zap.Int64("remaining_connections", remaining),
		)
		// Broadcast to unblock the waiter goroutine so it doesn't leak
		dm.zeroCond.Broadcast()
	}

	// Reset draining state after drain completes so the manager can be reused
	dm.draining.Store(false)
}

// IsDraining returns true if the manager is currently in drain mode.
func (dm *DrainManager) IsDraining() bool {
	return dm.draining.Load()
}

// TrackConnection increments the active connection counter. Each call must
// be paired with a corresponding ReleaseConnection call.
// Uses atomic add to avoid mutex contention in the hot request path.
func (dm *DrainManager) TrackConnection() {
	atomic.AddInt64(&dm.activeCount, 1)
}

// ReleaseConnection decrements the active connection counter.
// Uses atomic add to avoid mutex contention in the hot request path.
// When draining, broadcasts once the count reaches zero so StartDrain unblocks.
func (dm *DrainManager) ReleaseConnection() {
	if atomic.AddInt64(&dm.activeCount, -1) == 0 {
		// Broadcast doesn't require holding the lock (see sync.Cond documentation).
		// StartDrain re-reads the count atomically inside its wait loop, so a
		// spurious broadcast when not draining is harmless.
		dm.zeroCond.Broadcast()
	}
}

// ActiveConnections returns the current number of tracked active connections.
func (dm *DrainManager) ActiveConnections() int64 {
	return atomic.LoadInt64(&dm.activeCount)
}

// DrainMiddleware returns an HTTP middleware that integrates with the
// DrainManager. It tracks each request as an active connection and, when
// draining is active, sets the "Connection: close" header on HTTP/1.x
// responses to signal clients to open new connections (which will be
// served by the updated configuration).
func (dm *DrainManager) DrainMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dm.TrackConnection()
		defer dm.ReleaseConnection()

		if dm.IsDraining() {
			// For HTTP/1.x, set Connection: close to signal the client that
			// the server will close the connection after this response.
			// For HTTP/2+, Go's net/http server handles GOAWAY when
			// Shutdown is called, so no explicit header is needed.
			if !r.ProtoAtLeast(2, 0) {
				w.Header().Set("Connection", "close")
			}
		}

		next.ServeHTTP(w, r)
	})
}
