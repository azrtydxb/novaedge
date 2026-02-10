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
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestOIDCSessionEncryptDecrypt(t *testing.T) {
	sessionKey := make([]byte, 32)
	for i := range sessionKey {
		sessionKey[i] = byte(i)
	}

	handler := &OIDCHandler{
		sessionKey: sessionKey,
	}

	original := &OIDCSession{
		IDToken:     "id-token-value",
		AccessToken: "access-token-value",
		Claims: map[string]interface{}{
			"sub":   "user123",
			"email": "user@example.com",
		},
	}

	// Encrypt
	encrypted, err := handler.encryptSession(original)
	if err != nil {
		t.Fatalf("failed to encrypt session: %v", err)
	}

	if encrypted == "" {
		t.Fatal("encrypted session should not be empty")
	}

	// Decrypt
	decrypted, err := handler.decryptSession(encrypted)
	if err != nil {
		t.Fatalf("failed to decrypt session: %v", err)
	}

	if decrypted.IDToken != original.IDToken {
		t.Errorf("IDToken mismatch: got %s, want %s", decrypted.IDToken, original.IDToken)
	}
	if decrypted.AccessToken != original.AccessToken {
		t.Errorf("AccessToken mismatch: got %s, want %s", decrypted.AccessToken, original.AccessToken)
	}

	sub, ok := decrypted.Claims["sub"].(string)
	if !ok || sub != "user123" {
		t.Errorf("Claims.sub mismatch: got %v", decrypted.Claims["sub"])
	}
}

func TestOIDCSessionDecrypt_InvalidData(t *testing.T) {
	sessionKey := make([]byte, 32)

	handler := &OIDCHandler{
		sessionKey: sessionKey,
	}

	_, err := handler.decryptSession("not-valid-base64!@#$")
	if err == nil {
		t.Error("expected error for invalid encrypted data")
	}

	_, err = handler.decryptSession("aW52YWxpZA==") // "invalid" in base64
	if err == nil {
		t.Error("expected error for non-encrypted data")
	}
}

func TestCheckAuthorization_AnyMode_Roles(t *testing.T) {
	config := &pb.OIDCConfig{
		Authorization: &pb.AuthorizationConfig{
			RequiredRoles: []string{"admin", "editor"},
			Mode:          "any",
		},
	}

	// User has admin role
	claims := map[string]interface{}{
		"roles": []interface{}{"admin", "viewer"},
	}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with admin role in any mode")
	}

	// User has neither required role
	claims = map[string]interface{}{
		"roles": []interface{}{"viewer"},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail without required roles")
	}
}

func TestCheckAuthorization_AllMode_Roles(t *testing.T) {
	config := &pb.OIDCConfig{
		Authorization: &pb.AuthorizationConfig{
			RequiredRoles: []string{"admin", "editor"},
			Mode:          "all",
		},
	}

	// User has both roles
	claims := map[string]interface{}{
		"roles": []interface{}{"admin", "editor", "viewer"},
	}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with all required roles")
	}

	// User has only one role
	claims = map[string]interface{}{
		"roles": []interface{}{"admin"},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail without all required roles")
	}
}

func TestCheckAuthorization_Groups(t *testing.T) {
	config := &pb.OIDCConfig{
		Authorization: &pb.AuthorizationConfig{
			RequiredGroups: []string{"engineering"},
			Mode:           "any",
		},
	}

	claims := map[string]interface{}{
		"groups": []interface{}{"engineering", "platform"},
	}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with engineering group")
	}

	claims = map[string]interface{}{
		"groups": []interface{}{"marketing"},
	}

	if checkAuthorization(config, claims) {
		t.Error("expected authorization to fail without required group")
	}
}

func TestCheckAuthorization_NoRequirements(t *testing.T) {
	config := &pb.OIDCConfig{
		Authorization: &pb.AuthorizationConfig{
			Mode: "any",
		},
	}

	claims := map[string]interface{}{}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with no requirements")
	}
}

func TestCheckAuthorization_NilAuthz(t *testing.T) {
	config := &pb.OIDCConfig{}

	claims := map[string]interface{}{}

	if !checkAuthorization(config, claims) {
		t.Error("expected authorization to pass with nil authorization config")
	}
}

func TestExtractStringSlice_Simple(t *testing.T) {
	claims := map[string]interface{}{
		"roles": []interface{}{"admin", "user"},
	}

	roles := extractStringSlice(claims, "roles")
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles))
	}
	if roles[0] != "admin" || roles[1] != "user" {
		t.Errorf("unexpected roles: %v", roles)
	}
}

func TestExtractStringSlice_Nested(t *testing.T) {
	claims := map[string]interface{}{
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"admin", "offline_access"},
		},
	}

	roles := extractStringSlice(claims, "realm_access.roles")
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles))
	}
	if roles[0] != "admin" {
		t.Errorf("expected first role admin, got %s", roles[0])
	}
}

func TestExtractStringSlice_MissingPath(t *testing.T) {
	claims := map[string]interface{}{
		"name": "test",
	}

	roles := extractStringSlice(claims, "realm_access.roles")
	if roles != nil {
		t.Errorf("expected nil for missing path, got %v", roles)
	}
}

func TestContainsAll(t *testing.T) {
	if !containsAll([]string{"a", "b", "c"}, []string{"a", "b"}) {
		t.Error("expected containsAll to return true")
	}
	if containsAll([]string{"a"}, []string{"a", "b"}) {
		t.Error("expected containsAll to return false")
	}
	if !containsAll([]string{"a", "b"}, []string{}) {
		t.Error("expected containsAll to return true for empty required")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny([]string{"a", "b", "c"}, []string{"b", "d"}) {
		t.Error("expected containsAny to return true")
	}
	if containsAny([]string{"a"}, []string{"b", "c"}) {
		t.Error("expected containsAny to return false")
	}
	if containsAny([]string{}, []string{"a"}) {
		t.Error("expected containsAny to return false for empty user items")
	}
}

func TestGenerateState(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("failed to generate state: %v", err)
	}
	if state == "" {
		t.Error("state should not be empty")
	}

	// Generate another and verify they're different
	state2, err := generateState()
	if err != nil {
		t.Fatalf("failed to generate second state: %v", err)
	}
	if state == state2 {
		t.Error("two states should be different")
	}
}

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("failed to generate code verifier: %v", err)
	}
	if verifier == "" {
		t.Error("code verifier should not be empty")
	}
	if len(verifier) < 43 { // PKCE spec requires 43-128 characters
		t.Errorf("code verifier too short: %d chars", len(verifier))
	}
}
