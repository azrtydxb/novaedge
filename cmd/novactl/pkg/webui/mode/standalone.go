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

package mode

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/models"
	"github.com/piwi3910/novaedge/internal/standalone"
	"gopkg.in/yaml.v3"
)

// StandaloneBackend implements Backend for standalone YAML file operations
type StandaloneBackend struct {
	configPath string
	readOnly   bool
	mu         sync.RWMutex
	config     *standalone.Config
}

// NewStandaloneBackend creates a new standalone backend
func NewStandaloneBackend(configPath string, readOnly bool) (*StandaloneBackend, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	s := &StandaloneBackend{
		configPath: configPath,
		readOnly:   readOnly,
	}

	// Load initial config
	if err := s.reload(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return s, nil
}

// reload loads/reloads the configuration from disk
func (s *StandaloneBackend) reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := standalone.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	s.config = config
	return nil
}

// save saves the configuration to disk
func (s *StandaloneBackend) save() error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	data, err := yaml.Marshal(s.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Mode returns the backend mode
func (s *StandaloneBackend) Mode() Mode {
	return ModeStandalone
}

// ReadOnly returns whether the backend is read-only
func (s *StandaloneBackend) ReadOnly() bool {
	return s.readOnly
}

// ListGateways returns all gateways (listeners in standalone mode)
func (s *StandaloneBackend) ListGateways(ctx context.Context, namespace string) ([]models.Gateway, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// In standalone mode, we create a single "default" gateway containing all listeners
	if len(s.config.Listeners) == 0 {
		return []models.Gateway{}, nil
	}

	gw := models.Gateway{
		Name:      "default",
		Namespace: "standalone",
	}

	for _, l := range s.config.Listeners {
		listener := models.Listener{
			Name:               l.Name,
			Port:               l.Port,
			Protocol:           l.Protocol,
			Hostnames:          l.Hostnames,
			MaxRequestBodySize: l.MaxRequestBodySize,
		}

		if l.TLS != nil {
			listener.TLS = &models.TLS{
				CertFile:     l.TLS.CertFile,
				KeyFile:      l.TLS.KeyFile,
				MinVersion:   l.TLS.MinVersion,
				CipherSuites: l.TLS.CipherSuites,
			}
		}

		gw.Listeners = append(gw.Listeners, listener)
	}

	// Add tracing config
	if s.config.Global.Tracing.Enabled {
		gw.Tracing = &models.Tracing{
			Enabled:         s.config.Global.Tracing.Enabled,
			SamplingRate:    s.config.Global.Tracing.SamplingRate,
			RequestIDHeader: s.config.Global.Tracing.RequestIDHeader,
		}
	}

	// Add access log config
	if s.config.Global.AccessLog.Enabled {
		gw.AccessLog = &models.AccessLog{
			Enabled: s.config.Global.AccessLog.Enabled,
			Format:  s.config.Global.AccessLog.Format,
			Path:    s.config.Global.AccessLog.Path,
		}
	}

	return []models.Gateway{gw}, nil
}

// GetGateway returns a specific gateway
func (s *StandaloneBackend) GetGateway(ctx context.Context, namespace, name string) (*models.Gateway, error) {
	gateways, err := s.ListGateways(ctx, namespace)
	if err != nil {
		return nil, err
	}

	for _, gw := range gateways {
		if gw.Name == name {
			return &gw, nil
		}
	}

	return nil, fmt.Errorf("gateway '%s' not found", name)
}

// CreateGateway creates a new gateway (adds listeners in standalone mode)
func (s *StandaloneBackend) CreateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Add listeners from the gateway
	for _, l := range gateway.Listeners {
		listener := standalone.ListenerConfig{
			Name:               l.Name,
			Port:               l.Port,
			Protocol:           l.Protocol,
			Hostnames:          l.Hostnames,
			MaxRequestBodySize: l.MaxRequestBodySize,
		}

		if l.TLS != nil {
			listener.TLS = &standalone.TLSConfig{
				CertFile:     l.TLS.CertFile,
				KeyFile:      l.TLS.KeyFile,
				MinVersion:   l.TLS.MinVersion,
				CipherSuites: l.TLS.CipherSuites,
			}
		}

		s.config.Listeners = append(s.config.Listeners, listener)
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return gateway, nil
}

// UpdateGateway updates an existing gateway
func (s *StandaloneBackend) UpdateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replace all listeners
	s.config.Listeners = make([]standalone.ListenerConfig, 0, len(gateway.Listeners))
	for _, l := range gateway.Listeners {
		listener := standalone.ListenerConfig{
			Name:               l.Name,
			Port:               l.Port,
			Protocol:           l.Protocol,
			Hostnames:          l.Hostnames,
			MaxRequestBodySize: l.MaxRequestBodySize,
		}

		if l.TLS != nil {
			listener.TLS = &standalone.TLSConfig{
				CertFile:     l.TLS.CertFile,
				KeyFile:      l.TLS.KeyFile,
				MinVersion:   l.TLS.MinVersion,
				CipherSuites: l.TLS.CipherSuites,
			}
		}

		s.config.Listeners = append(s.config.Listeners, listener)
	}

	// Update tracing
	if gateway.Tracing != nil {
		s.config.Global.Tracing = standalone.TracingConfig{
			Enabled:         gateway.Tracing.Enabled,
			SamplingRate:    gateway.Tracing.SamplingRate,
			RequestIDHeader: gateway.Tracing.RequestIDHeader,
		}
	}

	// Update access log
	if gateway.AccessLog != nil {
		s.config.Global.AccessLog = standalone.AccessLogConfig{
			Enabled: gateway.AccessLog.Enabled,
			Format:  gateway.AccessLog.Format,
			Path:    gateway.AccessLog.Path,
		}
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return gateway, nil
}

// DeleteGateway deletes a gateway (clears listeners in standalone mode)
func (s *StandaloneBackend) DeleteGateway(ctx context.Context, namespace, name string) error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if name == "default" {
		s.config.Listeners = []standalone.ListenerConfig{}
		return s.save()
	}

	return fmt.Errorf("gateway '%s' not found", name)
}

// ListRoutes returns all routes
func (s *StandaloneBackend) ListRoutes(ctx context.Context, namespace string) ([]models.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]models.Route, 0, len(s.config.Routes))
	for _, r := range s.config.Routes {
		route := models.Route{
			Name:      r.Name,
			Namespace: "standalone",
			Hostnames: r.Match.Hostnames,
			Timeout:   r.Timeout,
			Policies:  r.Policies,
		}

		// Convert match
		if r.Match.Path != nil {
			route.Matches = []models.RouteMatch{{
				Path: &models.PathMatch{
					Type:  r.Match.Path.Type,
					Value: r.Match.Path.Value,
				},
				Method: r.Match.Method,
			}}
		}

		// Convert headers
		for _, h := range r.Match.Headers {
			if len(route.Matches) == 0 {
				route.Matches = []models.RouteMatch{{}}
			}
			route.Matches[0].Headers = append(route.Matches[0].Headers, models.HeaderMatch{
				Name:  h.Name,
				Value: h.Value,
				Type:  h.Type,
			})
		}

		// Convert backend refs
		for _, b := range r.Backends {
			route.BackendRefs = append(route.BackendRefs, models.BackendRef{
				Name:   b.Name,
				Weight: b.Weight,
			})
		}

		// Convert filters
		for _, f := range r.Filters {
			filter := models.Filter{
				Type: f.Type,
			}
			if f.RewritePath != "" {
				filter.URLRewrite = &models.URLRewrite{
					Path: &models.PathModifier{
						Type:            "ReplaceFullPath",
						ReplaceFullPath: f.RewritePath,
					},
				}
			}
			route.Filters = append(route.Filters, filter)
		}

		routes = append(routes, route)
	}

	return routes, nil
}

// GetRoute returns a specific route
func (s *StandaloneBackend) GetRoute(ctx context.Context, namespace, name string) (*models.Route, error) {
	routes, err := s.ListRoutes(ctx, namespace)
	if err != nil {
		return nil, err
	}

	for _, rt := range routes {
		if rt.Name == name {
			return &rt, nil
		}
	}

	return nil, fmt.Errorf("route '%s' not found", name)
}

// CreateRoute creates a new route
func (s *StandaloneBackend) CreateRoute(ctx context.Context, route *models.Route) (*models.Route, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate
	for _, r := range s.config.Routes {
		if r.Name == route.Name {
			return nil, fmt.Errorf("route '%s' already exists", route.Name)
		}
	}

	r := s.modelRouteToStandalone(route)
	s.config.Routes = append(s.config.Routes, *r)

	if err := s.save(); err != nil {
		return nil, err
	}

	return route, nil
}

// UpdateRoute updates an existing route
func (s *StandaloneBackend) UpdateRoute(ctx context.Context, route *models.Route) (*models.Route, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for i, r := range s.config.Routes {
		if r.Name == route.Name {
			s.config.Routes[i] = *s.modelRouteToStandalone(route)
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("route '%s' not found", route.Name)
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return route, nil
}

// DeleteRoute deletes a route
func (s *StandaloneBackend) DeleteRoute(ctx context.Context, namespace, name string) error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, r := range s.config.Routes {
		if r.Name == name {
			s.config.Routes = append(s.config.Routes[:i], s.config.Routes[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("route '%s' not found", name)
}

// ListBackends returns all backends
func (s *StandaloneBackend) ListBackends(ctx context.Context, namespace string) ([]models.Backend, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backends := make([]models.Backend, 0, len(s.config.Backends))
	for _, b := range s.config.Backends {
		backend := models.Backend{
			Name:      b.Name,
			Namespace: "standalone",
			LBPolicy:  b.LBPolicy,
		}

		// Convert endpoints
		for _, e := range b.Endpoints {
			backend.Endpoints = append(backend.Endpoints, models.Endpoint{
				Address: e.Address,
				Weight:  e.Weight,
			})
		}

		// Convert health check
		if b.HealthCheck != nil {
			backend.HealthCheck = &models.HealthCheck{
				Protocol:           b.HealthCheck.Protocol,
				Path:               b.HealthCheck.Path,
				Port:               b.HealthCheck.Port,
				Interval:           b.HealthCheck.Interval,
				Timeout:            b.HealthCheck.Timeout,
				HealthyThreshold:   b.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: b.HealthCheck.UnhealthyThreshold,
			}
		}

		// Convert circuit breaker
		if b.CircuitBreaker != nil {
			backend.CircuitBreaker = &models.CircuitBreaker{
				MaxConnections:     b.CircuitBreaker.MaxConnections,
				MaxPendingRequests: b.CircuitBreaker.MaxPendingRequests,
				MaxRequests:        b.CircuitBreaker.MaxRequests,
				MaxRetries:         b.CircuitBreaker.MaxRetries,
				ConsecutiveErrors:  b.CircuitBreaker.ConsecutiveErrors,
				Interval:           b.CircuitBreaker.Interval,
				BaseEjectionTime:   b.CircuitBreaker.BaseEjectionTime,
				MaxEjectionPercent: b.CircuitBreaker.MaxEjectionPercent,
			}
		}

		// Convert connection pool
		if b.ConnectionPool != nil {
			backend.ConnectionPool = &models.ConnectionPool{
				MaxConnections:        b.ConnectionPool.MaxConnections,
				MaxIdleConnections:    b.ConnectionPool.MaxIdleConnections,
				IdleTimeout:           b.ConnectionPool.IdleTimeout,
				MaxConnectionLifetime: b.ConnectionPool.MaxConnectionLifetime,
			}
		}

		// Convert TLS
		if b.TLS != nil {
			backend.TLS = &models.BackendTLS{
				Enabled:            b.TLS.Enabled,
				InsecureSkipVerify: b.TLS.InsecureSkipVerify,
				CAFile:             b.TLS.CAFile,
				CertFile:           b.TLS.CertFile,
				KeyFile:            b.TLS.KeyFile,
				ServerName:         b.TLS.ServerName,
			}
		}

		backends = append(backends, backend)
	}

	return backends, nil
}

// GetBackend returns a specific backend
func (s *StandaloneBackend) GetBackend(ctx context.Context, namespace, name string) (*models.Backend, error) {
	backends, err := s.ListBackends(ctx, namespace)
	if err != nil {
		return nil, err
	}

	for _, be := range backends {
		if be.Name == name {
			return &be, nil
		}
	}

	return nil, fmt.Errorf("backend '%s' not found", name)
}

// CreateBackend creates a new backend
func (s *StandaloneBackend) CreateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate
	for _, b := range s.config.Backends {
		if b.Name == backend.Name {
			return nil, fmt.Errorf("backend '%s' already exists", backend.Name)
		}
	}

	b := s.modelBackendToStandalone(backend)
	s.config.Backends = append(s.config.Backends, *b)

	if err := s.save(); err != nil {
		return nil, err
	}

	return backend, nil
}

// UpdateBackend updates an existing backend
func (s *StandaloneBackend) UpdateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for i, b := range s.config.Backends {
		if b.Name == backend.Name {
			s.config.Backends[i] = *s.modelBackendToStandalone(backend)
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("backend '%s' not found", backend.Name)
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return backend, nil
}

// DeleteBackend deletes a backend
func (s *StandaloneBackend) DeleteBackend(ctx context.Context, namespace, name string) error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, b := range s.config.Backends {
		if b.Name == name {
			s.config.Backends = append(s.config.Backends[:i], s.config.Backends[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("backend '%s' not found", name)
}

// ListVIPs returns all VIPs
func (s *StandaloneBackend) ListVIPs(ctx context.Context, namespace string) ([]models.VIP, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vips := make([]models.VIP, 0, len(s.config.VIPs))
	for _, v := range s.config.VIPs {
		vip := models.VIP{
			Name:      v.Name,
			Namespace: "standalone",
			Address:   v.Address,
			Mode:      v.Mode,
			Interface: v.Interface,
		}

		if v.BGP != nil {
			vip.BGP = &models.BGPConfig{
				LocalAS:       v.BGP.LocalAS,
				RouterID:      v.BGP.RouterID,
				PeerAS:        v.BGP.PeerAS,
				PeerIP:        v.BGP.PeerIP,
				HoldTime:      v.BGP.HoldTime,
				KeepaliveTime: v.BGP.KeepaliveTime,
			}
		}

		if v.OSPF != nil {
			vip.OSPF = &models.OSPFConfig{
				RouterID:  v.OSPF.RouterID,
				Area:      v.OSPF.Area,
				Interface: v.OSPF.Interface,
			}
		}

		vips = append(vips, vip)
	}

	return vips, nil
}

// GetVIP returns a specific VIP
func (s *StandaloneBackend) GetVIP(ctx context.Context, namespace, name string) (*models.VIP, error) {
	vips, err := s.ListVIPs(ctx, namespace)
	if err != nil {
		return nil, err
	}

	for _, v := range vips {
		if v.Name == name {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("VIP '%s' not found", name)
}

// CreateVIP creates a new VIP
func (s *StandaloneBackend) CreateVIP(ctx context.Context, vip *models.VIP) (*models.VIP, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate
	for _, v := range s.config.VIPs {
		if v.Name == vip.Name {
			return nil, fmt.Errorf("VIP '%s' already exists", vip.Name)
		}
	}

	v := s.modelVIPToStandalone(vip)
	s.config.VIPs = append(s.config.VIPs, *v)

	if err := s.save(); err != nil {
		return nil, err
	}

	return vip, nil
}

// UpdateVIP updates an existing VIP
func (s *StandaloneBackend) UpdateVIP(ctx context.Context, vip *models.VIP) (*models.VIP, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for i, v := range s.config.VIPs {
		if v.Name == vip.Name {
			s.config.VIPs[i] = *s.modelVIPToStandalone(vip)
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("VIP '%s' not found", vip.Name)
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return vip, nil
}

// DeleteVIP deletes a VIP
func (s *StandaloneBackend) DeleteVIP(ctx context.Context, namespace, name string) error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, v := range s.config.VIPs {
		if v.Name == name {
			s.config.VIPs = append(s.config.VIPs[:i], s.config.VIPs[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("VIP '%s' not found", name)
}

// ListPolicies returns all policies
func (s *StandaloneBackend) ListPolicies(ctx context.Context, namespace string) ([]models.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]models.Policy, 0, len(s.config.Policies))
	for _, p := range s.config.Policies {
		policy := models.Policy{
			Name:      p.Name,
			Namespace: "standalone",
			Type:      p.Type,
		}

		if p.RateLimit != nil {
			policy.RateLimit = &models.RateLimitConfig{
				RequestsPerSecond: p.RateLimit.RequestsPerSecond,
				BurstSize:         p.RateLimit.BurstSize,
				Key:               p.RateLimit.Key,
			}
		}

		if p.CORS != nil {
			policy.CORS = &models.CORSConfig{
				AllowOrigins:     p.CORS.AllowOrigins,
				AllowMethods:     p.CORS.AllowMethods,
				AllowHeaders:     p.CORS.AllowHeaders,
				ExposeHeaders:    p.CORS.ExposeHeaders,
				MaxAge:           p.CORS.MaxAge,
				AllowCredentials: p.CORS.AllowCredentials,
			}
		}

		if p.IPFilter != nil {
			policy.IPFilter = &models.IPFilterConfig{
				AllowList: p.IPFilter.AllowList,
				DenyList:  p.IPFilter.DenyList,
			}
		}

		if p.JWT != nil {
			policy.JWT = &models.JWTConfig{
				Issuer:    p.JWT.Issuer,
				Audience:  p.JWT.Audience,
				JWKSURI:   p.JWT.JWKSURI,
				SecretKey: p.JWT.SecretKey,
			}
		}

		policies = append(policies, policy)
	}

	return policies, nil
}

// GetPolicy returns a specific policy
func (s *StandaloneBackend) GetPolicy(ctx context.Context, namespace, name string) (*models.Policy, error) {
	policies, err := s.ListPolicies(ctx, namespace)
	if err != nil {
		return nil, err
	}

	for _, p := range policies {
		if p.Name == name {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("policy '%s' not found", name)
}

// CreatePolicy creates a new policy
func (s *StandaloneBackend) CreatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate
	for _, p := range s.config.Policies {
		if p.Name == policy.Name {
			return nil, fmt.Errorf("policy '%s' already exists", policy.Name)
		}
	}

	p := s.modelPolicyToStandalone(policy)
	s.config.Policies = append(s.config.Policies, *p)

	if err := s.save(); err != nil {
		return nil, err
	}

	return policy, nil
}

// UpdatePolicy updates an existing policy
func (s *StandaloneBackend) UpdatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for i, p := range s.config.Policies {
		if p.Name == policy.Name {
			s.config.Policies[i] = *s.modelPolicyToStandalone(policy)
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("policy '%s' not found", policy.Name)
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return policy, nil
}

// DeletePolicy deletes a policy
func (s *StandaloneBackend) DeletePolicy(ctx context.Context, namespace, name string) error {
	if s.readOnly {
		return fmt.Errorf("backend is read-only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.config.Policies {
		if p.Name == name {
			s.config.Policies = append(s.config.Policies[:i], s.config.Policies[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("policy '%s' not found", name)
}

// ListNamespaces returns available namespaces (just "standalone" in standalone mode)
func (s *StandaloneBackend) ListNamespaces(ctx context.Context) ([]string, error) {
	return []string{"standalone"}, nil
}

// ValidateConfig validates the configuration
func (s *StandaloneBackend) ValidateConfig(ctx context.Context, config *models.Config) error {
	return validateConfig(config)
}

// ExportConfig exports the full configuration as YAML
func (s *StandaloneBackend) ExportConfig(ctx context.Context, namespace string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return yaml.Marshal(s.config)
}

// ImportConfig imports configuration from YAML
func (s *StandaloneBackend) ImportConfig(ctx context.Context, data []byte, dryRun bool) (*models.ImportResult, error) {
	if s.readOnly {
		return nil, fmt.Errorf("backend is read-only")
	}

	var config standalone.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	result := &models.ImportResult{DryRun: dryRun}

	// Count what would be created/updated
	for _, l := range config.Listeners {
		result.Created = append(result.Created, models.ResourceRef{
			Kind: "Listener",
			Name: l.Name,
		})
	}
	for _, r := range config.Routes {
		result.Created = append(result.Created, models.ResourceRef{
			Kind: "Route",
			Name: r.Name,
		})
	}
	for _, b := range config.Backends {
		result.Created = append(result.Created, models.ResourceRef{
			Kind: "Backend",
			Name: b.Name,
		})
	}
	for _, v := range config.VIPs {
		result.Created = append(result.Created, models.ResourceRef{
			Kind: "VIP",
			Name: v.Name,
		})
	}
	for _, p := range config.Policies {
		result.Created = append(result.Created, models.ResourceRef{
			Kind: "Policy",
			Name: p.Name,
		})
	}

	if dryRun {
		return result, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = &config

	if err := s.save(); err != nil {
		return nil, err
	}

	return result, nil
}

// Conversion helpers

func (s *StandaloneBackend) modelRouteToStandalone(route *models.Route) *standalone.RouteConfig {
	r := &standalone.RouteConfig{
		Name:     route.Name,
		Timeout:  route.Timeout,
		Policies: route.Policies,
	}

	// Convert match
	r.Match.Hostnames = route.Hostnames
	if len(route.Matches) > 0 {
		m := route.Matches[0]
		if m.Path != nil {
			r.Match.Path = &standalone.PathMatch{
				Type:  m.Path.Type,
				Value: m.Path.Value,
			}
		}
		r.Match.Method = m.Method
		for _, h := range m.Headers {
			r.Match.Headers = append(r.Match.Headers, standalone.HeaderMatch{
				Name:  h.Name,
				Value: h.Value,
				Type:  h.Type,
			})
		}
	}

	// Convert backend refs
	for _, b := range route.BackendRefs {
		r.Backends = append(r.Backends, standalone.RouteBackendRef{
			Name:   b.Name,
			Weight: b.Weight,
		})
	}

	// Convert filters
	for _, f := range route.Filters {
		filter := standalone.RouteFilter{
			Type: f.Type,
		}
		if f.URLRewrite != nil && f.URLRewrite.Path != nil {
			filter.RewritePath = f.URLRewrite.Path.ReplaceFullPath
		}
		r.Filters = append(r.Filters, filter)
	}

	return r
}

func (s *StandaloneBackend) modelBackendToStandalone(backend *models.Backend) *standalone.BackendConfig {
	b := &standalone.BackendConfig{
		Name:     backend.Name,
		LBPolicy: backend.LBPolicy,
	}

	// Convert endpoints
	for _, e := range backend.Endpoints {
		b.Endpoints = append(b.Endpoints, standalone.EndpointConfig{
			Address: e.Address,
			Weight:  e.Weight,
		})
	}

	// Convert health check
	if backend.HealthCheck != nil {
		b.HealthCheck = &standalone.HealthCheckConfig{
			Protocol:           backend.HealthCheck.Protocol,
			Path:               backend.HealthCheck.Path,
			Port:               backend.HealthCheck.Port,
			Interval:           backend.HealthCheck.Interval,
			Timeout:            backend.HealthCheck.Timeout,
			HealthyThreshold:   backend.HealthCheck.HealthyThreshold,
			UnhealthyThreshold: backend.HealthCheck.UnhealthyThreshold,
		}
	}

	// Convert circuit breaker
	if backend.CircuitBreaker != nil {
		b.CircuitBreaker = &standalone.CircuitBreakerConfig{
			MaxConnections:     backend.CircuitBreaker.MaxConnections,
			MaxPendingRequests: backend.CircuitBreaker.MaxPendingRequests,
			MaxRequests:        backend.CircuitBreaker.MaxRequests,
			MaxRetries:         backend.CircuitBreaker.MaxRetries,
			ConsecutiveErrors:  backend.CircuitBreaker.ConsecutiveErrors,
			Interval:           backend.CircuitBreaker.Interval,
			BaseEjectionTime:   backend.CircuitBreaker.BaseEjectionTime,
			MaxEjectionPercent: backend.CircuitBreaker.MaxEjectionPercent,
		}
	}

	// Convert connection pool
	if backend.ConnectionPool != nil {
		b.ConnectionPool = &standalone.ConnectionPoolConfig{
			MaxConnections:        backend.ConnectionPool.MaxConnections,
			MaxIdleConnections:    backend.ConnectionPool.MaxIdleConnections,
			IdleTimeout:           backend.ConnectionPool.IdleTimeout,
			MaxConnectionLifetime: backend.ConnectionPool.MaxConnectionLifetime,
		}
	}

	// Convert TLS
	if backend.TLS != nil {
		b.TLS = &standalone.BackendTLSConfig{
			Enabled:            backend.TLS.Enabled,
			InsecureSkipVerify: backend.TLS.InsecureSkipVerify,
			CAFile:             backend.TLS.CAFile,
			CertFile:           backend.TLS.CertFile,
			KeyFile:            backend.TLS.KeyFile,
			ServerName:         backend.TLS.ServerName,
		}
	}

	return b
}

func (s *StandaloneBackend) modelVIPToStandalone(vip *models.VIP) *standalone.VIPConfig {
	v := &standalone.VIPConfig{
		Name:      vip.Name,
		Address:   vip.Address,
		Mode:      vip.Mode,
		Interface: vip.Interface,
	}

	if vip.BGP != nil {
		v.BGP = &standalone.BGPConfig{
			LocalAS:       vip.BGP.LocalAS,
			RouterID:      vip.BGP.RouterID,
			PeerAS:        vip.BGP.PeerAS,
			PeerIP:        vip.BGP.PeerIP,
			HoldTime:      vip.BGP.HoldTime,
			KeepaliveTime: vip.BGP.KeepaliveTime,
		}
	}

	if vip.OSPF != nil {
		v.OSPF = &standalone.OSPFConfig{
			RouterID:  vip.OSPF.RouterID,
			Area:      vip.OSPF.Area,
			Interface: vip.OSPF.Interface,
		}
	}

	return v
}

func (s *StandaloneBackend) modelPolicyToStandalone(policy *models.Policy) *standalone.PolicyConfig {
	p := &standalone.PolicyConfig{
		Name: policy.Name,
		Type: policy.Type,
	}

	if policy.RateLimit != nil {
		p.RateLimit = &standalone.RateLimitPolicy{
			RequestsPerSecond: policy.RateLimit.RequestsPerSecond,
			BurstSize:         policy.RateLimit.BurstSize,
			Key:               policy.RateLimit.Key,
		}
	}

	if policy.CORS != nil {
		p.CORS = &standalone.CORSPolicy{
			AllowOrigins:     policy.CORS.AllowOrigins,
			AllowMethods:     policy.CORS.AllowMethods,
			AllowHeaders:     policy.CORS.AllowHeaders,
			ExposeHeaders:    policy.CORS.ExposeHeaders,
			MaxAge:           policy.CORS.MaxAge,
			AllowCredentials: policy.CORS.AllowCredentials,
		}
	}

	if policy.IPFilter != nil {
		p.IPFilter = &standalone.IPFilterPolicy{
			AllowList: policy.IPFilter.AllowList,
			DenyList:  policy.IPFilter.DenyList,
		}
	}

	if policy.JWT != nil {
		p.JWT = &standalone.JWTPolicy{
			Issuer:    policy.JWT.Issuer,
			Audience:  policy.JWT.Audience,
			JWKSURI:   policy.JWT.JWKSURI,
			SecretKey: policy.JWT.SecretKey,
		}
	}

	return p
}
