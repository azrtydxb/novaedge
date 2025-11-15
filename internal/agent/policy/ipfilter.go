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

// IPFilter implements IP address filtering
type IPFilter struct {
	allowList []*net.IPNet
	denyList  []*net.IPNet
	mode      string // "allow" or "deny"
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

// Allow checks if a request should be allowed
func (f *IPFilter) Allow(r *http.Request) bool {
	clientIP := extractClientIP(r)
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

// extractClientIP extracts the client IP from the request
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
