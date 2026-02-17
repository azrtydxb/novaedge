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

package tunnel

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/stun/v3"
	"go.uber.org/zap"
)

var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
}

const stunCacheTTL = 5 * time.Minute

// STUNDiscoverer discovers the public IP and port of the local host using STUN.
// It caches results for stunCacheTTL to avoid unnecessary network requests.
type STUNDiscoverer struct {
	mu         sync.RWMutex
	servers    []string
	cachedAddr *net.UDPAddr
	cachedAt   time.Time
	logger     *zap.Logger
}

// NewSTUNDiscoverer creates a new STUNDiscoverer. If servers is nil or empty,
// the default Google STUN servers are used.
func NewSTUNDiscoverer(servers []string, logger *zap.Logger) *STUNDiscoverer {
	if len(servers) == 0 {
		servers = defaultSTUNServers
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &STUNDiscoverer{
		servers: servers,
		logger:  logger,
	}
}

// Discover returns the reflexive transport address (public IP:port) by querying
// STUN servers. Results are cached for 5 minutes; subsequent calls within the
// TTL return the cached address without hitting the network.
func (d *STUNDiscoverer) Discover() (*net.UDPAddr, error) {
	d.mu.RLock()
	if d.cachedAddr != nil && time.Since(d.cachedAt) < stunCacheTTL {
		addr := *d.cachedAddr
		d.mu.RUnlock()
		return &addr, nil
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock.
	if d.cachedAddr != nil && time.Since(d.cachedAt) < stunCacheTTL {
		addr := *d.cachedAddr
		return &addr, nil
	}

	var lastErr error
	for _, server := range d.servers {
		addr, err := d.querySTUN(server)
		if err != nil {
			d.logger.Debug("STUN query failed", zap.String("server", server), zap.Error(err))
			lastErr = err
			continue
		}
		d.cachedAddr = addr
		d.cachedAt = time.Now()
		d.logger.Info("STUN discovery succeeded",
			zap.String("server", server),
			zap.String("addr", addr.String()),
		)
		result := *addr
		return &result, nil
	}

	return nil, fmt.Errorf("all STUN servers failed: %w", lastErr)
}

// querySTUN sends a STUN binding request to a single server and extracts the
// XOR-MAPPED-ADDRESS from the response.
func (d *STUNDiscoverer) querySTUN(server string) (*net.UDPAddr, error) {
	conn, err := stun.Dial("udp4", server)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", server, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			d.logger.Debug("failed to close STUN connection", zap.Error(closeErr))
		}
	}()

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	var (
		resultAddr *net.UDPAddr
		resultErr  error
	)

	if err = conn.Do(message, func(res stun.Event) {
		if res.Error != nil {
			resultErr = fmt.Errorf("STUN event error from %s: %w", server, res.Error)
			return
		}
		var xorAddr stun.XORMappedAddress
		if getErr := xorAddr.GetFrom(res.Message); getErr != nil {
			resultErr = fmt.Errorf("parse XOR-MAPPED-ADDRESS from %s: %w", server, getErr)
			return
		}
		resultAddr = &net.UDPAddr{
			IP:   xorAddr.IP,
			Port: xorAddr.Port,
		}
	}); err != nil {
		return nil, fmt.Errorf("STUN Do for %s: %w", server, err)
	}

	if resultErr != nil {
		return nil, resultErr
	}
	if resultAddr == nil {
		return nil, fmt.Errorf("no address returned from %s", server)
	}

	return resultAddr, nil
}

// ClearCache invalidates the cached STUN result, forcing the next Discover call
// to query the STUN servers again.
func (d *STUNDiscoverer) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cachedAddr = nil
	d.cachedAt = time.Time{}
}
