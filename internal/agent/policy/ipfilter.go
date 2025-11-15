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

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// Global list of trusted proxy IP ranges (can be configured at package level)
var trustedProxyCIDRs []*net.IPNet

// SetGlobalTrustedProxies sets the global list of trusted proxy IP ranges
// This is used by rate limiters and other policies that need IP extraction
func SetGlobalTrustedProxies(cidrs []string) error {
	trustedProxyCIDRs = nil
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return err
			}
			// Convert single IP to CIDR
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(cidr + "/128")
			}
		}
		trustedProxyCIDRs = append(trustedProxyCIDRs, ipNet)
	}
	return nil
}

// isGlobalTrustedProxy checks if an IP is in the global trusted proxy list
func isGlobalTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, ipNet := range trustedProxyCIDRs {
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
	if len(trustedProxyCIDRs) == 0 {
		return remoteIP
	}

	// Only trust X-Forwarded-For if request comes from a trusted proxy
	if !isGlobalTrustedProxy(remoteIP) {
		return remoteIP
	}

	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For format: client, proxy1, proxy2, ...
		// We need to find the rightmost IP that is NOT a trusted proxy
		// This is the real client IP
		ips := strings.Split(xff, ",")

		// Iterate from right to left, skipping trusted proxies
		for i := len(ips) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(ips[i])
			if !isGlobalTrustedProxy(ip) {
				// Found the rightmost non-proxy IP
				return ip
			}
		}
	}

	// Check X-Real-IP header as fallback
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
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
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(cidr + "/128")
			}
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
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(cidr + "/128")
			}
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
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(cidr + "/128")
			}
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
	} else {
		// Deny list mode: deny IPs in the list
		for _, ipNet := range f.denyList {
			if ipNet.Contains(ip) {
				return false
			}
		}
		return true
	}
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
		// X-Forwarded-For format: client, proxy1, proxy2, ...
		// We need to find the rightmost IP that is NOT a trusted proxy
		// This is the real client IP
		ips := strings.Split(xff, ",")

		// Iterate from right to left, skipping trusted proxies
		for i := len(ips) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(ips[i])
			if !f.isTrustedProxy(ip) {
				// Found the rightmost non-proxy IP
				return ip
			}
		}
	}

	// Check X-Real-IP header as fallback
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return remoteIP
}
