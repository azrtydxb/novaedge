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
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// buildBasicAuthConfig builds a BasicAuthConfig proto from the CRD spec,
// loading the htpasswd credentials from the referenced Kubernetes Secret.
func (b *Builder) buildBasicAuthConfig(ctx context.Context, p *novaedgev1alpha1.ProxyPolicy) (*pb.BasicAuthConfig, error) {
	spec := p.Spec.BasicAuth
	if spec == nil {
		return nil, fmt.Errorf("basicAuth spec is nil")
	}

	// Load htpasswd from secret
	secret := &corev1.Secret{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: p.Namespace,
		Name:      spec.SecretRef.Name,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get htpasswd secret %s: %w", spec.SecretRef.Name, err)
	}

	htpasswd, ok := secret.Data["htpasswd"]
	if !ok {
		return nil, fmt.Errorf("secret %s missing 'htpasswd' key", spec.SecretRef.Name)
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
// loading client secret and session secret from referenced Kubernetes Secrets.
func (b *Builder) buildOIDCConfig(ctx context.Context, p *novaedgev1alpha1.ProxyPolicy) (*pb.OIDCConfig, error) {
	spec := p.Spec.OIDC
	if spec == nil {
		return nil, fmt.Errorf("oidc spec is nil")
	}

	// Load client secret
	clientSecret, err := b.loadSecretValue(ctx, p.Namespace, spec.ClientSecretRef.Name, "client-secret")
	if err != nil {
		return nil, fmt.Errorf("failed to load client secret: %w", err)
	}

	// Load session secret
	sessionSecret, err := b.loadSecretBytes(ctx, p.Namespace, spec.SessionSecretRef.Name, "session-secret")
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

// loadSecretValue loads a string value from a Kubernetes Secret.
func (b *Builder) loadSecretValue(ctx context.Context, namespace, secretName, key string) (string, error) {
	secret := &corev1.Secret{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	data, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s missing key '%s'", secretName, key)
	}

	return string(data), nil
}

// loadSecretBytes loads raw bytes from a Kubernetes Secret.
func (b *Builder) loadSecretBytes(ctx context.Context, namespace, secretName, key string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	data, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("secret %s missing key '%s'", secretName, key)
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
