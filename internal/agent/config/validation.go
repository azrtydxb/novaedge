package config

import (
	"fmt"
	"net"
	"strings"

	pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Validator provides configuration validation
type Validator struct{}

// NewValidator creates a new configuration validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateSnapshot validates a complete configuration snapshot
func (v *Validator) ValidateSnapshot(snapshot *Snapshot) error {
	if snapshot == nil || snapshot.ConfigSnapshot == nil {
		return pkgerrors.NewValidationError("snapshot cannot be nil")
	}

	if snapshot.Version == "" {
		return pkgerrors.NewValidationError("version is required").WithField("field", "version")
	}

	parentErr := pkgerrors.NewValidationError("snapshot validation failed")
	hasErrors := false

	// Validate gateways
	for i, gw := range snapshot.Gateways {
		if err := v.ValidateGateway(gw, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	// Validate routes
	for i, route := range snapshot.Routes {
		if err := v.ValidateRoute(route, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	// Validate clusters
	for i, cluster := range snapshot.Clusters {
		if err := v.ValidateCluster(cluster, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	// Validate endpoints
	for clusterName, endpointList := range snapshot.Endpoints {
		if err := v.ValidateEndpointList(clusterName, endpointList); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	// Validate VIP assignments
	for i, vip := range snapshot.VipAssignments {
		if err := v.ValidateVIPAssignment(vip, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	if hasErrors {
		return parentErr
	}

	return nil
}

// ValidateGateway validates a gateway configuration
func (v *Validator) ValidateGateway(gw *pb.Gateway, index int) error {
	if gw == nil {
		return pkgerrors.NewValidationError("gateway cannot be nil").
			WithField("field", fmt.Sprintf("gateways[%d]", index))
	}

	prefix := fmt.Sprintf("gateways[%d]", index)

	if gw.Name == "" {
		return pkgerrors.NewValidationError("gateway name is required").
			WithField("field", prefix+".name")
	}

	if gw.Namespace == "" {
		return pkgerrors.NewValidationError("gateway namespace is required").
			WithField("field", prefix+".namespace")
	}

	if len(gw.Listeners) == 0 {
		return pkgerrors.NewValidationError("gateway must have at least one listener").
			WithField("field", prefix+".listeners")
	}

	parentErr := pkgerrors.NewValidationError(fmt.Sprintf("gateway '%s/%s' validation failed", gw.Namespace, gw.Name))
	hasErrors := false

	for i, listener := range gw.Listeners {
		if err := v.ValidateListener(listener, prefix, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	if hasErrors {
		return parentErr
	}

	return nil
}

// ValidateListener validates a listener configuration
func (v *Validator) ValidateListener(listener *pb.Listener, parentPrefix string, index int) error {
	if listener == nil {
		return pkgerrors.NewValidationError("listener cannot be nil").
			WithField("field", fmt.Sprintf("%s.listeners[%d]", parentPrefix, index))
	}

	prefix := fmt.Sprintf("%s.listeners[%d]", parentPrefix, index)

	if listener.Name == "" {
		return pkgerrors.NewValidationError("listener name is required").
			WithField("field", prefix+".name")
	}

	if listener.Port < 1 || listener.Port > 65535 {
		return pkgerrors.NewValidationError("listener port must be between 1 and 65535").
			WithField("field", prefix+".port").
			WithField("value", listener.Port)
	}

	if listener.Protocol == pb.Protocol_PROTOCOL_UNSPECIFIED {
		return pkgerrors.NewValidationError("listener protocol must be specified").
			WithField("field", prefix+".protocol")
	}

	// Validate TLS config if present
	if listener.Tls != nil {
		if err := v.ValidateTLSConfig(listener.Tls, prefix+".tls"); err != nil {
			return err
		}
	}

	// Validate SNI TLS certificates if present
	for hostname, tlsCfg := range listener.TlsCertificates {
		if tlsCfg != nil {
			if err := v.ValidateTLSConfig(tlsCfg, fmt.Sprintf("%s.tls_certificates[%s]", prefix, hostname)); err != nil {
				return err
			}
		}
	}

	// HTTPS and TLS protocols should have TLS config
	if listener.Protocol == pb.Protocol_HTTPS || listener.Protocol == pb.Protocol_TLS {
		if listener.Tls == nil && len(listener.TlsCertificates) == 0 {
			return pkgerrors.NewValidationError("HTTPS/TLS listener requires TLS configuration").
				WithField("field", prefix+".tls").
				WithField("protocol", listener.Protocol.String())
		}
	}

	return nil
}

// ValidateTLSConfig validates a TLS configuration
func (v *Validator) ValidateTLSConfig(tlsCfg *pb.TLSConfig, fieldPrefix string) error {
	if tlsCfg == nil {
		return nil
	}

	hasCert := len(tlsCfg.Cert) > 0
	hasKey := len(tlsCfg.Key) > 0

	// Cert and key must both be present or both be absent
	if hasCert != hasKey {
		if hasCert {
			return pkgerrors.NewValidationError("TLS key is required when cert is provided").
				WithField("field", fieldPrefix+".key")
		}
		return pkgerrors.NewValidationError("TLS cert is required when key is provided").
			WithField("field", fieldPrefix+".cert")
	}

	return nil
}

// ValidateRoute validates a route configuration
func (v *Validator) ValidateRoute(route *pb.Route, index int) error {
	if route == nil {
		return pkgerrors.NewValidationError("route cannot be nil").
			WithField("field", fmt.Sprintf("routes[%d]", index))
	}

	prefix := fmt.Sprintf("routes[%d]", index)

	if route.Name == "" {
		return pkgerrors.NewValidationError("route name is required").
			WithField("field", prefix+".name")
	}

	if route.Namespace == "" {
		return pkgerrors.NewValidationError("route namespace is required").
			WithField("field", prefix+".namespace")
	}

	if len(route.Rules) == 0 {
		return pkgerrors.NewValidationError("route must have at least one rule").
			WithField("field", prefix+".rules")
	}

	parentErr := pkgerrors.NewValidationError(fmt.Sprintf("route '%s/%s' validation failed", route.Namespace, route.Name))
	hasErrors := false

	for i, rule := range route.Rules {
		if err := v.ValidateRouteRule(rule, prefix, i); err != nil {
			var validationErr *pkgerrors.ValidationError
			if ok := isValidationError(err, &validationErr); ok {
				_ = parentErr.AddChild(validationErr)
			} else {
				_ = parentErr.AddChild(pkgerrors.NewValidationError(err.Error()))
			}
			hasErrors = true
		}
	}

	if hasErrors {
		return parentErr
	}

	return nil
}

// ValidateRouteRule validates a route rule
func (v *Validator) ValidateRouteRule(rule *pb.RouteRule, parentPrefix string, index int) error {
	if rule == nil {
		return pkgerrors.NewValidationError("route rule cannot be nil").
			WithField("field", fmt.Sprintf("%s.rules[%d]", parentPrefix, index))
	}

	prefix := fmt.Sprintf("%s.rules[%d]", parentPrefix, index)

	// A rule must have at least one match or be a catch-all (empty matches means match all)
	// But if matches are present, validate each one
	for i, match := range rule.Matches {
		if err := v.ValidateRouteMatch(match, prefix, i); err != nil {
			return err
		}
	}

	// A rule must have at least one backend ref
	if len(rule.BackendRefs) == 0 {
		return pkgerrors.NewValidationError("route rule must have at least one backend reference").
			WithField("field", prefix+".backend_refs")
	}

	for i, backendRef := range rule.BackendRefs {
		if err := v.ValidateBackendRef(backendRef, prefix, i); err != nil {
			return err
		}
	}

	return nil
}

// ValidateRouteMatch validates a route match
func (v *Validator) ValidateRouteMatch(match *pb.RouteMatch, parentPrefix string, index int) error {
	if match == nil {
		return pkgerrors.NewValidationError("route match cannot be nil").
			WithField("field", fmt.Sprintf("%s.matches[%d]", parentPrefix, index))
	}

	prefix := fmt.Sprintf("%s.matches[%d]", parentPrefix, index)

	// Validate path match if present
	if match.Path != nil {
		if match.Path.Value == "" {
			return pkgerrors.NewValidationError("path match value cannot be empty").
				WithField("field", prefix+".path.value")
		}

		if match.Path.Type == pb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED {
			return pkgerrors.NewValidationError("path match type must be specified").
				WithField("field", prefix+".path.type")
		}

		// Path prefix and exact must start with /
		if match.Path.Type == pb.PathMatchType_EXACT || match.Path.Type == pb.PathMatchType_PATH_PREFIX {
			if !strings.HasPrefix(match.Path.Value, "/") {
				return pkgerrors.NewValidationError("path match value must start with '/'").
					WithField("field", prefix+".path.value").
					WithField("value", match.Path.Value)
			}
		}
	}

	// Validate method if specified
	if match.Method != "" {
		validMethods := map[string]bool{
			"GET": true, "HEAD": true, "POST": true, "PUT": true,
			"DELETE": true, "CONNECT": true, "OPTIONS": true,
			"TRACE": true, "PATCH": true,
		}
		if !validMethods[match.Method] {
			return pkgerrors.NewValidationError("invalid HTTP method").
				WithField("field", prefix+".method").
				WithField("value", match.Method)
		}
	}

	return nil
}

// ValidateBackendRef validates a backend reference
func (v *Validator) ValidateBackendRef(ref *pb.BackendRef, parentPrefix string, index int) error {
	if ref == nil {
		return pkgerrors.NewValidationError("backend reference cannot be nil").
			WithField("field", fmt.Sprintf("%s.backend_refs[%d]", parentPrefix, index))
	}

	prefix := fmt.Sprintf("%s.backend_refs[%d]", parentPrefix, index)

	if ref.Name == "" {
		return pkgerrors.NewValidationError("backend reference name is required").
			WithField("field", prefix+".name")
	}

	if ref.Weight < 0 {
		return pkgerrors.NewValidationError("backend reference weight cannot be negative").
			WithField("field", prefix+".weight").
			WithField("value", ref.Weight)
	}

	return nil
}

// ValidateCluster validates a cluster (backend) configuration
func (v *Validator) ValidateCluster(cluster *pb.Cluster, index int) error {
	if cluster == nil {
		return pkgerrors.NewValidationError("cluster cannot be nil").
			WithField("field", fmt.Sprintf("clusters[%d]", index))
	}

	prefix := fmt.Sprintf("clusters[%d]", index)

	if cluster.Name == "" {
		return pkgerrors.NewValidationError("cluster name is required").
			WithField("field", prefix+".name")
	}

	if cluster.Namespace == "" {
		return pkgerrors.NewValidationError("cluster namespace is required").
			WithField("field", prefix+".namespace")
	}

	// Validate timeouts are non-negative
	if cluster.ConnectTimeoutMs < 0 {
		return pkgerrors.NewValidationError("cluster connect timeout cannot be negative").
			WithField("field", prefix+".connect_timeout_ms").
			WithField("value", cluster.ConnectTimeoutMs)
	}

	if cluster.IdleTimeoutMs < 0 {
		return pkgerrors.NewValidationError("cluster idle timeout cannot be negative").
			WithField("field", prefix+".idle_timeout_ms").
			WithField("value", cluster.IdleTimeoutMs)
	}

	return nil
}

// ValidateEndpointList validates an endpoint list for a cluster
func (v *Validator) ValidateEndpointList(clusterName string, endpointList *pb.EndpointList) error {
	if endpointList == nil {
		return pkgerrors.NewValidationError("endpoint list cannot be nil").
			WithField("field", fmt.Sprintf("endpoints[%s]", clusterName))
	}

	if len(endpointList.Endpoints) == 0 {
		// Empty endpoint list is valid (cluster may have no ready pods)
		return nil
	}

	for i, ep := range endpointList.Endpoints {
		if err := v.ValidateEndpoint(ep, clusterName, i); err != nil {
			return err
		}
	}

	return nil
}

// ValidateEndpoint validates a single endpoint
func (v *Validator) ValidateEndpoint(ep *pb.Endpoint, clusterName string, index int) error {
	if ep == nil {
		return pkgerrors.NewValidationError("endpoint cannot be nil").
			WithField("field", fmt.Sprintf("endpoints[%s][%d]", clusterName, index))
	}

	prefix := fmt.Sprintf("endpoints[%s][%d]", clusterName, index)

	if ep.Address == "" {
		return pkgerrors.NewValidationError("endpoint address is required").
			WithField("field", prefix+".address")
	}

	// Validate address is a valid IP or hostname
	if net.ParseIP(ep.Address) == nil {
		// Not a valid IP, check if it could be a valid hostname
		if !isValidHostname(ep.Address) {
			return pkgerrors.NewValidationError("endpoint address must be a valid IP or hostname").
				WithField("field", prefix+".address").
				WithField("value", ep.Address)
		}
	}

	if ep.Port < 1 || ep.Port > 65535 {
		return pkgerrors.NewValidationError("endpoint port must be between 1 and 65535").
			WithField("field", prefix+".port").
			WithField("value", ep.Port)
	}

	return nil
}

// ValidateVIPAssignment validates a VIP assignment
func (v *Validator) ValidateVIPAssignment(vip *pb.VIPAssignment, index int) error {
	if vip == nil {
		return pkgerrors.NewValidationError("VIP assignment cannot be nil").
			WithField("field", fmt.Sprintf("vip_assignments[%d]", index))
	}

	prefix := fmt.Sprintf("vip_assignments[%d]", index)

	if vip.VipName == "" {
		return pkgerrors.NewValidationError("VIP name is required").
			WithField("field", prefix+".vip_name")
	}

	if vip.Address == "" {
		return pkgerrors.NewValidationError("VIP address is required").
			WithField("field", prefix+".address")
	}

	// VIP address should be a valid IP (with optional CIDR notation)
	addr := vip.Address
	if strings.Contains(addr, "/") {
		_, _, err := net.ParseCIDR(addr)
		if err != nil {
			return pkgerrors.NewValidationError("VIP address must be a valid CIDR").
				WithField("field", prefix+".address").
				WithField("value", addr)
		}
	} else {
		if net.ParseIP(addr) == nil {
			return pkgerrors.NewValidationError("VIP address must be a valid IP").
				WithField("field", prefix+".address").
				WithField("value", addr)
		}
	}

	if vip.Mode == pb.VIPMode_VIP_MODE_UNSPECIFIED {
		return pkgerrors.NewValidationError("VIP mode must be specified").
			WithField("field", prefix+".mode")
	}

	// Validate ports if present
	for i, port := range vip.Ports {
		if port < 1 || port > 65535 {
			return pkgerrors.NewValidationError("VIP port must be between 1 and 65535").
				WithField("field", fmt.Sprintf("%s.ports[%d]", prefix, i)).
				WithField("value", port)
		}
	}

	return nil
}

// isValidHostname performs basic hostname validation
func isValidHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}

	parts := strings.Split(hostname, ".")
	for _, part := range parts {
		if len(part) == 0 || len(part) > 63 {
			return false
		}
		for _, c := range part {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
		// Labels cannot start or end with a hyphen
		if part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
	}

	return true
}

// isValidationError checks if an error is a ValidationError and extracts it
func isValidationError(err error, target **pkgerrors.ValidationError) bool {
	if ve, ok := err.(*pkgerrors.ValidationError); ok {
		*target = ve
		return true
	}
	return false
}
