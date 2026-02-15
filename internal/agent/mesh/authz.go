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

package mesh

import (
	"net/url"
	"path"
	"strings"
	"sync"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// SourceIdentity represents a parsed SPIFFE identity.
type SourceIdentity struct {
	SpiffeID       string
	TrustDomain    string
	Namespace      string
	ServiceAccount string
}

// ParseSPIFFEID parses a SPIFFE ID into its components.
// Format: spiffe://trust-domain/ns/NAMESPACE/sa/SERVICE_ACCOUNT
// or: spiffe://trust-domain/agent/NODE_NAME
// Returns empty SourceIdentity if parsing fails.
func ParseSPIFFEID(spiffeID string) SourceIdentity {
	u, err := url.Parse(spiffeID)
	if err != nil || u.Scheme != "spiffe" || u.Host == "" {
		return SourceIdentity{}
	}

	identity := SourceIdentity{
		SpiffeID:    spiffeID,
		TrustDomain: u.Host,
	}

	// Split path segments, ignoring leading slash
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	// Parse workload identity: /ns/NAMESPACE/sa/SERVICE_ACCOUNT
	if len(segments) == 4 && segments[0] == "ns" && segments[2] == "sa" {
		identity.Namespace = segments[1]
		identity.ServiceAccount = segments[3]
		return identity
	}

	// Parse agent identity: /agent/NODE_NAME (namespace and SA remain empty)
	if len(segments) >= 2 && segments[0] == "agent" {
		return identity
	}

	// Unrecognized format — return with trust domain only
	return identity
}

// Authorizer evaluates mesh authorization policies.
type Authorizer struct {
	mu       sync.RWMutex
	logger   *zap.Logger
	policies map[string][]*pb.MeshAuthorizationPolicy // key: "service.namespace"
}

// NewAuthorizer creates a new Authorizer.
func NewAuthorizer(logger *zap.Logger) *Authorizer {
	return &Authorizer{
		logger:   logger,
		policies: make(map[string][]*pb.MeshAuthorizationPolicy),
	}
}

// UpdatePolicies replaces the authorization policies.
func (a *Authorizer) UpdatePolicies(policies []*pb.MeshAuthorizationPolicy) {
	a.mu.Lock()
	defer a.mu.Unlock()

	newPolicies := make(map[string][]*pb.MeshAuthorizationPolicy)
	for _, p := range policies {
		key := p.GetTargetService() + "." + p.GetTargetNamespace()
		newPolicies[key] = append(newPolicies[key], p)
	}

	a.policies = newPolicies
	a.logger.Info("updated mesh authorization policies",
		zap.Int("policy_count", len(policies)),
		zap.Int("service_count", len(newPolicies)),
	)
}

// Authorize checks if a source identity is allowed to access a destination.
// destService is "name.namespace".
// method and requestPath are for L7 matching (empty for opaque TCP).
// Returns true if access is allowed.
func (a *Authorizer) Authorize(source SourceIdentity, destService, method, requestPath string) bool {
	a.mu.RLock()
	servicePolicies, exists := a.policies[destService]
	a.mu.RUnlock()

	// No policies for this service — default allow
	if !exists || len(servicePolicies) == 0 {
		return true
	}

	// Evaluate DENY policies first — if any DENY rule matches, deny immediately
	for _, p := range servicePolicies {
		if strings.EqualFold(p.GetAction(), "DENY") {
			for _, rule := range p.GetRules() {
				if matchRule(rule, source, method, requestPath) {
					a.logger.Debug("mesh authorization denied by DENY policy",
						zap.String("policy", p.GetName()),
						zap.String("source", source.SpiffeID),
						zap.String("dest", destService),
					)
					return false
				}
			}
		}
	}

	// Collect ALLOW policies
	hasAllowPolicies := false
	for _, p := range servicePolicies {
		if strings.EqualFold(p.GetAction(), "ALLOW") {
			hasAllowPolicies = true
			for _, rule := range p.GetRules() {
				if matchRule(rule, source, method, requestPath) {
					a.logger.Debug("mesh authorization allowed by ALLOW policy",
						zap.String("policy", p.GetName()),
						zap.String("source", source.SpiffeID),
						zap.String("dest", destService),
					)
					return true
				}
			}
		}
	}

	// If ALLOW policies exist but none matched, deny
	if hasAllowPolicies {
		a.logger.Debug("mesh authorization denied: no ALLOW policy matched",
			zap.String("source", source.SpiffeID),
			zap.String("dest", destService),
		)
		return false
	}

	// Only DENY policies existed and none matched — allow
	return true
}

// PolicyCount returns the number of loaded policies.
func (a *Authorizer) PolicyCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	count := 0
	for _, policies := range a.policies {
		count += len(policies)
	}
	return count
}

// matchRule checks if a single authorization rule matches the given source and destination.
func matchRule(rule *pb.MeshAuthorizationRule, source SourceIdentity, method, requestPath string) bool {
	return matchFrom(rule.GetFrom(), source) && matchTo(rule.GetTo(), method, requestPath)
}

// matchFrom checks if the source identity matches any of the from sources.
// Empty from list matches all sources.
func matchFrom(fromSources []*pb.MeshSource, source SourceIdentity) bool {
	if len(fromSources) == 0 {
		return true
	}

	for _, from := range fromSources {
		if matchSource(from, source) {
			return true
		}
	}
	return false
}

// matchSource checks if a source identity matches a single MeshSource specification.
func matchSource(from *pb.MeshSource, source SourceIdentity) bool {
	// Check namespace constraint
	if len(from.GetNamespaces()) > 0 && !containsString(from.GetNamespaces(), source.Namespace) {
		return false
	}

	// Check service account constraint
	if len(from.GetServiceAccounts()) > 0 && !containsString(from.GetServiceAccounts(), source.ServiceAccount) {
		return false
	}

	// Check SPIFFE ID constraint (glob matching)
	if len(from.GetSpiffeIds()) > 0 && !matchAnyPattern(from.GetSpiffeIds(), source.SpiffeID) {
		return false
	}

	return true
}

// matchTo checks if the method and path match any of the to destinations.
// Empty to list matches all destinations.
func matchTo(toDests []*pb.MeshDestination, method, requestPath string) bool {
	if len(toDests) == 0 {
		return true
	}

	for _, to := range toDests {
		if matchDestination(to, method, requestPath) {
			return true
		}
	}
	return false
}

// matchDestination checks if a method and path match a single MeshDestination specification.
func matchDestination(to *pb.MeshDestination, method, requestPath string) bool {
	// For opaque TCP (empty method and path), destinations with methods or paths set do not match
	isOpaqueTCP := method == "" && requestPath == ""
	hasMethods := len(to.GetMethods()) > 0
	hasPaths := len(to.GetPaths()) > 0

	if isOpaqueTCP && (hasMethods || hasPaths) {
		return false
	}

	// Check method constraint (case-insensitive)
	if hasMethods && !containsStringFold(to.GetMethods(), method) {
		return false
	}

	// Check path constraint (glob matching)
	if hasPaths && !matchAnyPattern(to.GetPaths(), requestPath) {
		return false
	}

	return true
}

// containsString checks if a slice contains a specific string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// containsStringFold checks if a slice contains a specific string (case-insensitive).
func containsStringFold(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// matchAnyPattern checks if a string matches any of the given glob patterns.
func matchAnyPattern(patterns []string, s string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, s)
		if err == nil && matched {
			return true
		}
	}
	return false
}
