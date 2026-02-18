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
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// privateNetworks contains CIDR ranges that should be blocked for outbound requests.
// These are pre-computed at package initialization time to avoid runtime parsing overhead.
var privateNetworks []*net.IPNet

// parseCIDRSafely parses a CIDR string and returns an error instead of panicking.
// This is used during package initialization to provide proper error handling.
func parseCIDRSafely(s string) (*net.IPNet, error) {
	_, block, err := net.ParseCIDR(s)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid CIDR %q: %w", s, err)
	}
	return block, nil
}

// initPrivateNetworks initializes the private network CIDR blocks.
// If initialization fails, it logs an error and leaves privateNetworks empty,
// which effectively disables SSRF protection but allows the system to continue running.
func initPrivateNetworks() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}

	privateNetworks = make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		block, err := parseCIDRSafely(cidr)
		if err != nil {
			// Log the error but don't panic - this allows the system to continue
			// The SSRF protection will be less effective but the service won't crash
			fmt.Printf("warning: failed to parse CIDR %q: %v\n", cidr, err)
			continue
		}
		privateNetworks = append(privateNetworks, block)
	}
}

func init() {
	initPrivateNetworks()
}

// isPrivateIP checks if an IP address belongs to a private network range.
func isPrivateIP(ip net.IP) bool {
	for _, block := range privateNetworks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// NewSSRFProtectedTransport returns an http.Transport that blocks connections to private IP ranges.
func NewSSRFProtectedTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %w", err)
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS resolution failed: %w", err)
			}

			for _, ipAddr := range ips {
				if isPrivateIP(ipAddr.IP) {
					return nil, fmt.Errorf("connections to private IP %s are blocked (SSRF protection)", ipAddr.IP)
				}
			}

			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

// NewSSRFProtectedClient returns an HTTP client with SSRF protection.
func NewSSRFProtectedClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: NewSSRFProtectedTransport(),
	}
}
