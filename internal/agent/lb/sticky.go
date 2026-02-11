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

package lb

import (
	"net/http"
	"sync"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// StickyConfig holds cookie-based session affinity configuration.
type StickyConfig struct {
	// CookieName is the name of the affinity cookie
	CookieName string

	// TTL is the cookie time-to-live. Zero means session cookie.
	TTL time.Duration

	// Path is the cookie Path attribute (default "/")
	Path string

	// Secure sets the Secure flag on the cookie
	Secure bool

	// SameSite is the SameSite attribute: "Strict", "Lax", or "None"
	SameSite http.SameSite
}

// DefaultStickyConfig returns a StickyConfig with sensible defaults.
func DefaultStickyConfig() StickyConfig {
	return StickyConfig{
		CookieName: "NOVAEDGE_AFFINITY",
		TTL:        0, // session cookie
		Path:       "/",
		Secure:     false,
		SameSite:   http.SameSiteLaxMode,
	}
}

// ParseSameSite converts a string to http.SameSite.
func ParseSameSite(s string) http.SameSite {
	switch s {
	case "Strict":
		return http.SameSiteStrictMode
	case "Lax":
		return http.SameSiteLaxMode
	case "None":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// StickyWrapper wraps any LoadBalancer with cookie-based session affinity.
// On the first request (no affinity cookie), it delegates to the underlying LB,
// then sets a cookie mapping the client to the chosen endpoint.
// On subsequent requests, it reads the cookie and routes to the same endpoint,
// falling back to the underlying LB if the endpoint is no longer available.
type StickyWrapper struct {
	inner     LoadBalancer
	config    StickyConfig
	mu        sync.RWMutex
	endpoints []*pb.Endpoint
}

// NewStickyWrapper creates a new sticky session wrapper around the given LB.
func NewStickyWrapper(inner LoadBalancer, config StickyConfig, endpoints []*pb.Endpoint) *StickyWrapper {
	return &StickyWrapper{
		inner:     inner,
		config:    config,
		endpoints: endpoints,
	}
}

// Select delegates to the underlying load balancer.
// For sticky sessions, use SelectWithAffinity instead.
func (sw *StickyWrapper) Select() *pb.Endpoint {
	return sw.inner.Select()
}

// UpdateEndpoints updates endpoints on both the wrapper and the inner LB.
func (sw *StickyWrapper) UpdateEndpoints(endpoints []*pb.Endpoint) {
	sw.mu.Lock()
	sw.endpoints = endpoints
	sw.mu.Unlock()
	sw.inner.UpdateEndpoints(endpoints)
}

// SelectWithAffinity selects an endpoint considering cookie affinity.
// It reads the affinity cookie from the request. If a valid cookie exists and
// the endpoint is still healthy, that endpoint is returned.
// Otherwise the inner LB is used and a new cookie is set on the response.
func (sw *StickyWrapper) SelectWithAffinity(r *http.Request, w http.ResponseWriter) *pb.Endpoint {
	// Try to read existing affinity cookie
	cookie, err := r.Cookie(sw.config.CookieName)
	if err == nil && cookie.Value != "" {
		// Look up the endpoint from the cookie value (format: "address:port")
		if ep := sw.findEndpoint(cookie.Value); ep != nil {
			return ep
		}
		// Endpoint not found or not healthy; fall through to LB
	}

	// No valid affinity; use underlying LB
	ep := sw.inner.Select()
	if ep == nil {
		return nil
	}

	// Set affinity cookie on response
	sw.setAffinityCookie(w, ep)
	return ep
}

// findEndpoint looks up a healthy endpoint by its key ("address:port").
func (sw *StickyWrapper) findEndpoint(key string) *pb.Endpoint {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	for _, ep := range sw.endpoints {
		if ep.Ready && endpointKey(ep) == key {
			return ep
		}
	}
	return nil
}

// setAffinityCookie writes the affinity cookie to the HTTP response.
func (sw *StickyWrapper) setAffinityCookie(w http.ResponseWriter, ep *pb.Endpoint) {
	c := &http.Cookie{
		Name:     sw.config.CookieName,
		Value:    endpointKey(ep),
		Path:     sw.config.Path,
		Secure:   sw.config.Secure,
		HttpOnly: true,
		SameSite: sw.config.SameSite,
	}

	if sw.config.TTL > 0 {
		c.MaxAge = int(sw.config.TTL.Seconds())
	}

	http.SetCookie(w, c)
}

// GetInner returns the underlying load balancer.
// This is useful for calling algorithm-specific methods (e.g., IncrementActive).
func (sw *StickyWrapper) GetInner() LoadBalancer {
	return sw.inner
}
