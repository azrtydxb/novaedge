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

package snapshot

import (
	"errors"
	"fmt"
	"time"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errBasicAuthSpecIsNil = errors.New("basicAuth spec is nil")
	errHtpasswdSecret     = errors.New("htpasswd secret")
	errSecret             = errors.New("secret")
	errOidcSpecIsNil      = errors.New("oidc spec is nil")
)

// buildBasicAuthConfig builds a BasicAuthConfig proto from the CRD spec,
// loading the htpasswd credentials from the pre-fetched Secret cache.
func (b *Builder) buildBasicAuthConfig(p *novaedgev1alpha1.ProxyPolicy, bc *buildContext) (*pb.BasicAuthConfig, error) {
	spec := p.Spec.BasicAuth
	if spec == nil {
		return nil, errBasicAuthSpecIsNil
	}

	// Load htpasswd from pre-fetched secret cache
	secret, ok := bc.getSecret(p.Namespace, spec.SecretRef.Name)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s not found in cache", errHtpasswdSecret, p.Namespace, spec.SecretRef.Name)
	}

	htpasswd, ok := secret.Data["htpasswd"]
	if !ok {
		return nil, fmt.Errorf("%w: %s missing 'htpasswd' key", errSecret, spec.SecretRef.Name)
	}

	realm := spec.Realm
	if realm == "" {
		realm = "Restricted"
	}

	return &pb.BasicAuthConfig{
		Realm:     realm,
		Htpasswd:  string(htpasswd),
		StripAuth: spec.StripAuth,
	}, nil
}

// buildForwardAuthConfig builds a ForwardAuthConfig proto from the CRD spec.
func (b *Builder) buildForwardAuthConfig(spec *novaedgev1alpha1.ForwardAuthPolicyConfig) *pb.ForwardAuthConfig {
	config := &pb.ForwardAuthConfig{
		Address:         spec.Address,
		AuthHeaders:     spec.AuthHeaders,
		ResponseHeaders: spec.ResponseHeaders,
	}

	// Parse timeout
	if spec.Timeout != "" {
		if d, err := time.ParseDuration(spec.Timeout); err == nil {
			config.TimeoutMs = d.Milliseconds()
		}
	} else {
		config.TimeoutMs = 5000 // 5 second default
	}

	// Parse cache TTL
	if spec.CacheTTL != "" {
		if d, err := time.ParseDuration(spec.CacheTTL); err == nil {
			config.CacheTtlSeconds = int64(d.Seconds())
		}
	}

	return config
}

// buildOIDCConfig builds an OIDCConfig proto from the CRD spec,
// loading client secret and session secret from the pre-fetched Secret cache.
func (b *Builder) buildOIDCConfig(p *novaedgev1alpha1.ProxyPolicy, bc *buildContext) (*pb.OIDCConfig, error) {
	spec := p.Spec.OIDC
	if spec == nil {
		return nil, errOidcSpecIsNil
	}

	// Load client secret from pre-fetched cache
	clientSecret, err := b.loadSecretValue(p.Namespace, spec.ClientSecretRef.Name, "client-secret", bc)
	if err != nil {
		return nil, fmt.Errorf("failed to load client secret: %w", err)
	}

	// Load session secret from pre-fetched cache
	sessionSecret, err := b.loadSecretBytes(p.Namespace, spec.SessionSecretRef.Name, "session-secret", bc)
	if err != nil {
		return nil, fmt.Errorf("failed to load session secret: %w", err)
	}

	// Determine issuer URL
	issuerURL := spec.IssuerURL
	if issuerURL == "" && spec.Keycloak != nil {
		issuerURL = fmt.Sprintf("%s/realms/%s",
			trimTrailingSlash(spec.Keycloak.ServerURL),
			spec.Keycloak.Realm)
	}

	config := &pb.OIDCConfig{
		Provider:       spec.Provider,
		IssuerUrl:      issuerURL,
		ClientId:       spec.ClientID,
		ClientSecret:   clientSecret,
		RedirectUrl:    spec.RedirectURL,
		Scopes:         spec.Scopes,
		SessionSecret:  sessionSecret,
		ForwardHeaders: spec.ForwardHeaders,
	}

	// Keycloak-specific config
	if spec.Keycloak != nil {
		config.Keycloak = &pb.KeycloakConfig{
			ServerUrl:  spec.Keycloak.ServerURL,
			Realm:      spec.Keycloak.Realm,
			RoleClaim:  spec.Keycloak.RoleClaim,
			GroupClaim: spec.Keycloak.GroupClaim,
		}
	}

	// Authorization config
	if spec.Authorization != nil {
		config.Authorization = &pb.AuthorizationConfig{
			RequiredRoles:  spec.Authorization.RequiredRoles,
			RequiredGroups: spec.Authorization.RequiredGroups,
			Mode:           spec.Authorization.Mode,
		}
	}

	return config, nil
}

// loadSecretValue loads a string value from the pre-fetched Secret cache.
func (b *Builder) loadSecretValue(namespace, secretName, key string, bc *buildContext) (string, error) {
	secret, ok := bc.getSecret(namespace, secretName)
	if !ok {
		return "", fmt.Errorf("%w: %s/%s not found in cache", errSecret, namespace, secretName)
	}

	data, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("%w: %s missing key '%s'", errSecret, secretName, key)
	}

	return string(data), nil
}

// loadSecretBytes loads raw bytes from the pre-fetched Secret cache.
func (b *Builder) loadSecretBytes(namespace, secretName, key string, bc *buildContext) ([]byte, error) {
	secret, ok := bc.getSecret(namespace, secretName)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s not found in cache", errSecret, namespace, secretName)
	}

	data, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s missing key '%s'", errSecret, secretName, key)
	}

	return data, nil
}

// trimTrailingSlash removes a trailing slash from a URL.
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
