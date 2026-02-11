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
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestExtractKeycloakRoles_RealmRoles(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			RoleClaim: "realm_access.roles",
		},
	}

	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin", "user", "offline_access"},
		},
	}

	roles := extractKeycloakRoles(config, claims)

	if len(roles) < 3 {
		t.Fatalf("expected at least 3 realm roles, got %d: %v", len(roles), roles)
	}

	found := false
	for _, r := range roles {
		if r == "admin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected admin role, roles: %v", roles)
	}
}

func TestExtractKeycloakRoles_ClientRoles(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			RoleClaim: "realm_access.roles",
		},
	}

	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"user"},
		},
		"resource_access": map[string]interface{}{
			"my-app": map[string]interface{}{
				"roles": []interface{}{"app-admin", "app-editor"},
			},
		},
	}

	roles := extractKeycloakRoles(config, claims)

	// Should include both realm and client roles
	expectedRoles := map[string]bool{"user": true, "app-admin": true, "app-editor": true}
	for _, role := range roles {
		delete(expectedRoles, role)
	}
	if len(expectedRoles) > 0 {
		t.Errorf("missing roles: %v, got roles: %v", expectedRoles, roles)
	}
}

func TestExtractKeycloakGroups(t *testing.T) {
	config := &pb.OIDCConfig{
		Keycloak: &pb.KeycloakConfig{
			GroupClaim: "groups",
		},
	}

	claims := map[string]interface{}{
		"groups": []interface{}{"/engineering", "/platform-team"},
	}

	groups := extractKeycloakGroups(config, claims)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0] != "/engineering" {
		t.Errorf("expected /engineering, got %s", groups[0])
	}
}

func TestExtractKeycloakGroups_CustomClaim(t *testing.T) {
	config := &pb.OIDCConfig{
		Keycloak: &pb.KeycloakConfig{
			GroupClaim: "custom_groups",
		},
	}

	claims := map[string]interface{}{
		"custom_groups": []interface{}{"team-a", "team-b"},
	}

	groups := extractKeycloakGroups(config, claims)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestForwardKeycloakInfo(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			Realm:      "my-realm",
			RoleClaim:  "realm_access.roles",
			GroupClaim: "groups",
		},
	}

	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin", "user"},
		},
		"groups":             []interface{}{"/engineering"},
		"preferred_username": "jdoe",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	forwardKeycloakInfo(req, config, claims)

	roles := req.Header.Get("X-Auth-Roles")
	if roles == "" {
		t.Error("expected X-Auth-Roles header")
	}

	groups := req.Header.Get("X-Auth-Groups")
	if groups != "/engineering" {
		t.Errorf("expected X-Auth-Groups=/engineering, got %s", groups)
	}

	username := req.Header.Get("X-Auth-Username")
	if username != "jdoe" {
		t.Errorf("expected X-Auth-Username=jdoe, got %s", username)
	}

	realm := req.Header.Get("X-Auth-Realm")
	if realm != "my-realm" {
		t.Errorf("expected X-Auth-Realm=my-realm, got %s", realm)
	}
}

func TestCheckAuthorization_KeycloakRealmRoles(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			RoleClaim: "realm_access.roles",
		},
		Authorization: &pb.AuthorizationConfig{
			RequiredRoles: []string{"admin"},
			Mode:          "any",
		},
	}

	// User with admin role
	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin", "user"},
		},
	}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with admin role")
	}

	// User without admin role
	claims = map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"user"},
		},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail without admin role")
	}
}

func TestCheckAuthorization_KeycloakAllMode(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			RoleClaim:  "realm_access.roles",
			GroupClaim: "groups",
		},
		Authorization: &pb.AuthorizationConfig{
			RequiredRoles:  []string{"admin"},
			RequiredGroups: []string{"/engineering"},
			Mode:           "all",
		},
	}

	// User with both role and group
	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin"},
		},
		"groups": []interface{}{"/engineering"},
	}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with both role and group")
	}

	// User with only role
	claims = map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin"},
		},
		"groups": []interface{}{"/marketing"},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail without required group in all mode")
	}
}

func TestCheckAuthorization_Keycloak403OnMissingRole(t *testing.T) {
	config := &pb.OIDCConfig{
		ClientId: "my-app",
		Keycloak: &pb.KeycloakConfig{
			RoleClaim: "realm_access.roles",
		},
		Authorization: &pb.AuthorizationConfig{
			RequiredRoles: []string{"super-admin"},
			Mode:          "any",
		},
	}

	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"user", "viewer"},
		},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail: user lacks super-admin role (should result in 403)")
	}
}

func TestBuildKeycloakIssuerURL(t *testing.T) {
	url := BuildKeycloakIssuerURL("https://keycloak.example.com", "my-realm")
	expected := "https://keycloak.example.com/realms/my-realm"
	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}

	// With trailing slash
	url = BuildKeycloakIssuerURL("https://keycloak.example.com/", "my-realm")
	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}

func TestBuildKeycloakLogoutURL(t *testing.T) {
	url := BuildKeycloakLogoutURL("https://keycloak.example.com", "my-realm", "my-client", "https://app.example.com")
	expected := "https://keycloak.example.com/realms/my-realm/protocol/openid-connect/logout?post_logout_redirect_uri=https://app.example.com&client_id=my-client"
	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}
