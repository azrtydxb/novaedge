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

package policy

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// defaultMaxXFFDepth is the default maximum number of X-Forwarded-For entries to process.
// Headers with more entries than this limit are truncated to the rightmost N entries,
// preventing header bloat attacks.
const defaultMaxXFFDepth = 10

// isBogonIP returns true if the IP is a private, loopback, or link-local address
// (RFC 1918, RFC 4193, loopback, link-local). These IPs should not appear as
// client IPs in X-Forwarded-For unless from trusted proxies.
func isBogonIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// parseSingleIPAsCIDR converts a validated net.IP to a *net.IPNet by appending
// the appropriate mask (/32 for IPv4, /128 for IPv6). It logs a warning if the
// resulting CIDR string unexpectedly fails to parse.
func parseSingleIPAsCIDR(ip net.IP, raw string) *net.IPNet {
	mask := "/128"
	if ip.To4() != nil {
		mask = "/32"
	}
	_, ipNet, err := net.ParseCIDR(raw + mask)
	if err != nil {
		zap.L().Warn("failed to parse single IP as CIDR",
			zap.String("input", raw+mask),
			zap.Error(err),
		)
	}
	return ipNet
}

// trustedProxyCIDRsPtr holds the global list of trusted proxy IP ranges.
// It uses atomic.Pointer for lock-free concurrent reads during request
// processing while allowing safe writes during config reload.
var trustedProxyCIDRsPtr atomic.Pointer[[]*net.IPNet]

// SetGlobalTrustedProxies sets the global list of trusted proxy IP ranges.
// This is used by rate limiters and other policies that need IP extraction.
// The new slice is built locally and stored atomically so that concurrent
// readers always see a consistent snapshot.
func SetGlobalTrustedProxies(cidrs []string) error {
	var newCIDRs []*net.IPNet
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return err
			}
			// Convert single IP to CIDR
			ipNet = parseSingleIPAsCIDR(ip, cidr)
		}
		newCIDRs = append(newCIDRs, ipNet)
	}
	trustedProxyCIDRsPtr.Store(&newCIDRs)
	return nil
}

// isGlobalTrustedProxy checks if an IP is in the global trusted proxy list.
// The list is loaded atomically to avoid data races with concurrent writers.
func isGlobalTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	cidrs := trustedProxyCIDRsPtr.Load()
	if cidrs == nil {
		return false
	}
	for _, ipNet := range *cidrs {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// extractClientIP is a package-level function to extract client IP with trusted proxy validation
// This can be used by rate limiters and other policies
func extractClientIP(r *http.Request) string {
	// Get the direct connection IP (RemoteAddr)
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	// If no trusted proxies configured, don't trust forwarded headers
	cidrs := trustedProxyCIDRsPtr.Load()
	if cidrs == nil || len(*cidrs) == 0 {
		return remoteIP
	}

	// Only trust X-Forwarded-For if request comes from a trusted proxy
	if !isGlobalTrustedProxy(remoteIP) {
		return remoteIP
	}

	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")

		// Warn on suspiciously large XFF headers
		if len(ips) > defaultMaxXFFDepth {
			zap.L().Warn("X-Forwarded-For header exceeds max depth, truncating",
				zap.Int("entries", len(ips)),
				zap.Int("maxDepth", defaultMaxXFFDepth),
				zap.String("remoteAddr", r.RemoteAddr),
			)
			// Only process the rightmost N entries (closest to us, most trustworthy)
			ips = ips[len(ips)-defaultMaxXFFDepth:]
		}

		// Iterate from right to left, skipping trusted proxies
		for i := len(ips) - 1; i >= 0; i-- {
			ipStr := strings.TrimSpace(ips[i])

			// Validate IP format to prevent spoofed garbage values
			parsed := net.ParseIP(ipStr)
			if parsed == nil {
				zap.L().Warn("unparseable IP in X-Forwarded-For header",
					zap.String("value", ipStr),
					zap.String("remoteAddr", r.RemoteAddr),
				)
				continue
			}

			if isGlobalTrustedProxy(ipStr) {
				continue
			}

			// Skip bogon/private IPs unless they are from trusted proxies
			if isBogonIP(parsed) {
				continue
			}

			return ipStr
		}
	}

	// Check X-Real-IP header as fallback
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		// Validate X-Real-IP format
		if parsed := net.ParseIP(xri); parsed != nil {
			return xri
		}
		zap.L().Warn("unparseable X-Real-IP header",
			zap.String("value", xri),
			zap.String("remoteAddr", r.RemoteAddr),
		)
	}

	// Fall back to RemoteAddr
	return remoteIP
}

// IPFilter implements IP address filtering
type IPFilter struct {
	allowList    []*net.IPNet
	denyList     []*net.IPNet
	mode         string       // "allow" or "deny"
	trustedProxy []*net.IPNet // Trusted proxy IP ranges for X-Forwarded-For validation
}

// NewIPAllowListFilter creates an IP allow list filter
func NewIPAllowListFilter(cidrs []string) (*IPFilter, error) {
	filter := &IPFilter{
		mode: "allow",
	}

	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, err
			}
			// Convert single IP to CIDR
			ipNet = parseSingleIPAsCIDR(ip, cidr)
		}
		filter.allowList = append(filter.allowList, ipNet)
	}

	return filter, nil
}

// NewIPDenyListFilter creates an IP deny list filter
func NewIPDenyListFilter(cidrs []string) (*IPFilter, error) {
	filter := &IPFilter{
		mode: "deny",
	}

	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, err
			}
			// Convert single IP to CIDR
			ipNet = parseSingleIPAsCIDR(ip, cidr)
		}
		filter.denyList = append(filter.denyList, ipNet)
	}

	return filter, nil
}

// SetTrustedProxies sets the list of trusted proxy IP ranges
func (f *IPFilter) SetTrustedProxies(cidrs []string) error {
	f.trustedProxy = nil
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return err
			}
			// Convert single IP to CIDR
			ipNet = parseSingleIPAsCIDR(ip, cidr)
		}
		f.trustedProxy = append(f.trustedProxy, ipNet)
	}
	return nil
}

// isTrustedProxy checks if an IP is a trusted proxy
func (f *IPFilter) isTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, ipNet := range f.trustedProxy {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// Allow checks if a request should be allowed
func (f *IPFilter) Allow(r *http.Request) bool {
	clientIP := f.extractClientIP(r)
	ip := net.ParseIP(clientIP)
	if ip == nil {
		// If we can't parse IP, deny by default
		return false
	}

	if f.mode == "allow" {
		// Allow list mode: only allow IPs in the list
		for _, ipNet := range f.allowList {
			if ipNet.Contains(ip) {
				return true
			}
		}
		return false
	}

	// Deny list mode: deny IPs in the list
	for _, ipNet := range f.denyList {
		if ipNet.Contains(ip) {
			return false
		}
	}
	return true
}

// HandleIPFilter is HTTP middleware for IP filtering
func HandleIPFilter(filter *IPFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !filter.Allow(r) {
				metrics.IPFilterDenied.WithLabelValues(filter.mode + "_list").Inc()
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Request allowed, continue
			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the client IP from the request with trusted proxy validation
func (f *IPFilter) extractClientIP(r *http.Request) string {
	// Get the direct connection IP (RemoteAddr)
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	// If no trusted proxies configured, don't trust forwarded headers
	if len(f.trustedProxy) == 0 {
		return remoteIP
	}

	// Only trust X-Forwarded-For if request comes from a trusted proxy
	if !f.isTrustedProxy(remoteIP) {
		return remoteIP
	}

	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")

		// Warn on suspiciously large XFF headers
		if len(ips) > defaultMaxXFFDepth {
			zap.L().Warn("X-Forwarded-For header exceeds max depth, truncating",
				zap.Int("entries", len(ips)),
				zap.Int("maxDepth", defaultMaxXFFDepth),
				zap.String("remoteAddr", r.RemoteAddr),
			)
			// Only process the rightmost N entries (closest to us, most trustworthy)
			ips = ips[len(ips)-defaultMaxXFFDepth:]
		}

		// Iterate from right to left, skipping trusted proxies
		for i := len(ips) - 1; i >= 0; i-- {
			ipStr := strings.TrimSpace(ips[i])

			// Validate IP format to prevent spoofed garbage values
			parsed := net.ParseIP(ipStr)
			if parsed == nil {
				zap.L().Warn("unparseable IP in X-Forwarded-For header",
					zap.String("value", ipStr),
					zap.String("remoteAddr", r.RemoteAddr),
				)
				continue
			}

			if f.isTrustedProxy(ipStr) {
				continue
			}

			// Skip bogon/private IPs unless they are from trusted proxies
			if isBogonIP(parsed) {
				continue
			}

			return ipStr
		}
	}

	// Check X-Real-IP header as fallback
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		// Validate X-Real-IP format
		if parsed := net.ParseIP(xri); parsed != nil {
			return xri
		}
		zap.L().Warn("unparseable X-Real-IP header",
			zap.String("value", xri),
			zap.String("remoteAddr", r.RemoteAddr),
		)
	}

	// Fall back to RemoteAddr
	return remoteIP
}
