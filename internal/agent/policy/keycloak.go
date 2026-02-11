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
	"fmt"
	"net/http"
	"strings"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// extractKeycloakRoles extracts roles from Keycloak-specific JWT claims.
// Keycloak stores realm roles in realm_access.roles and client roles
// in resource_access.<clientID>.roles.
func extractKeycloakRoles(config *pb.OIDCConfig, claims map[string]interface{}) []string {
	var roles []string

	// Extract realm roles from realm_access.roles
	roleClaim := "realm_access.roles"
	if config.Keycloak != nil && config.Keycloak.RoleClaim != "" {
		roleClaim = config.Keycloak.RoleClaim
	}
	realmRoles := extractStringSlice(claims, roleClaim)
	roles = append(roles, realmRoles...)

	// Extract client roles from resource_access.<clientID>.roles
	if config.ClientId != "" {
		clientRolePath := fmt.Sprintf("resource_access.%s.roles", config.ClientId)
		clientRoles := extractStringSlice(claims, clientRolePath)
		roles = append(roles, clientRoles...)
	}

	return roles
}

// extractKeycloakGroups extracts groups from the Keycloak groups claim.
func extractKeycloakGroups(config *pb.OIDCConfig, claims map[string]interface{}) []string {
	groupClaim := "groups"
	if config.Keycloak != nil && config.Keycloak.GroupClaim != "" {
		groupClaim = config.Keycloak.GroupClaim
	}
	return extractStringSlice(claims, groupClaim)
}

// forwardKeycloakInfo sets Keycloak-specific headers on the upstream request.
func forwardKeycloakInfo(r *http.Request, config *pb.OIDCConfig, claims map[string]interface{}) {
	// Forward realm roles
	roles := extractKeycloakRoles(config, claims)
	if len(roles) > 0 {
		r.Header.Set("X-Auth-Roles", strings.Join(roles, ","))
	}

	// Forward groups
	groups := extractKeycloakGroups(config, claims)
	if len(groups) > 0 {
		r.Header.Set("X-Auth-Groups", strings.Join(groups, ","))
	}

	// Forward preferred_username if available
	if username, ok := claims["preferred_username"].(string); ok {
		r.Header.Set("X-Auth-Username", username)
	}

	// Forward realm name
	if config.Keycloak != nil && config.Keycloak.Realm != "" {
		r.Header.Set("X-Auth-Realm", config.Keycloak.Realm)
	}
}

// BuildKeycloakIssuerURL constructs the OIDC issuer URL from Keycloak server URL and realm.
func BuildKeycloakIssuerURL(serverURL, realm string) string {
	return fmt.Sprintf("%s/realms/%s", strings.TrimRight(serverURL, "/"), realm)
}

// BuildKeycloakLogoutURL constructs the Keycloak logout URL.
func BuildKeycloakLogoutURL(serverURL, realm, clientID, postLogoutRedirectURI string) string {
	base := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/logout",
		strings.TrimRight(serverURL, "/"), realm)
	if postLogoutRedirectURI != "" {
		return fmt.Sprintf("%s?post_logout_redirect_uri=%s&client_id=%s", base, postLogoutRedirectURI, clientID)
	}
	return base
}
