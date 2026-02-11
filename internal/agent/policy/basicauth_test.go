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

func TestBasicAuthValidator_ValidBcrypt(t *testing.T) {
	// Generate a bcrypt hash for testing
	hash, err := GenerateBcryptHash("secret123")
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}

	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:     "Test",
		Htpasswd:  htpasswd,
		StripAuth: true,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if !validator.Validate("admin", "secret123") {
		t.Error("expected valid credentials to pass")
	}
}

func TestBasicAuthValidator_InvalidPassword(t *testing.T) {
	hash, err := GenerateBcryptHash("secret123")
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}

	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if validator.Validate("admin", "wrongpassword") {
		t.Error("expected invalid password to fail")
	}
}

func TestBasicAuthValidator_UnknownUser(t *testing.T) {
	hash, err := GenerateBcryptHash("secret123")
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}

	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if validator.Validate("unknown", "secret123") {
		t.Error("expected unknown user to fail")
	}
}

func TestBasicAuthValidator_SHA256(t *testing.T) {
	hash := GenerateSHA256Hash("mypassword")
	htpasswd := "user1:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if !validator.Validate("user1", "mypassword") {
		t.Error("expected valid SHA256 credentials to pass")
	}

	if validator.Validate("user1", "wrongpassword") {
		t.Error("expected invalid SHA256 password to fail")
	}
}

func TestBasicAuthValidator_APR1MD5(t *testing.T) {
	hash := GenerateAPR1MD5Hash("test123")
	htpasswd := "user2:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if !validator.Validate("user2", "test123") {
		t.Error("expected valid APR1 MD5 credentials to pass")
	}

	if validator.Validate("user2", "wrong") {
		t.Error("expected invalid APR1 MD5 password to fail")
	}
}

func TestBasicAuthValidator_MultipleUsers(t *testing.T) {
	hash1, _ := GenerateBcryptHash("pass1")
	hash2 := GenerateSHA256Hash("pass2")

	htpasswd := "user1:" + hash1 + "\nuser2:" + hash2

	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	if !validator.Validate("user1", "pass1") {
		t.Error("expected user1 to pass")
	}
	if !validator.Validate("user2", "pass2") {
		t.Error("expected user2 to pass")
	}
	if validator.Validate("user1", "pass2") {
		t.Error("expected user1 with wrong password to fail")
	}
}

func TestBasicAuthValidator_EmptyHtpasswd(t *testing.T) {
	config := &pb.BasicAuthConfig{
		Realm:    "Test",
		Htpasswd: "",
	}

	_, err := NewBasicAuthValidator(config)
	if err == nil {
		t.Error("expected error for empty htpasswd")
	}
}

func TestHandleBasicAuth_ValidCredentials(t *testing.T) {
	hash, _ := GenerateBcryptHash("secret")
	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:     "TestRealm",
		Htpasswd:  htpasswd,
		StripAuth: true,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		nextCalled = true
		// Verify Authorization header was stripped
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Error("expected Authorization header to be stripped")
		}
		// Verify X-Auth-User header is set
		if user := r.Header.Get("X-Auth-User"); user != "admin" {
			t.Errorf("expected X-Auth-User=admin, got %s", user)
		}
	})

	handler := HandleBasicAuth(validator)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandleBasicAuth_InvalidCredentials(t *testing.T) {
	hash, _ := GenerateBcryptHash("secret")
	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "TestRealm",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called")
	})

	handler := HandleBasicAuth(validator)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
	if wwwAuth != `Basic realm="TestRealm"` {
		t.Errorf("expected realm TestRealm in WWW-Authenticate, got %s", wwwAuth)
	}
}

func TestHandleBasicAuth_MissingCredentials(t *testing.T) {
	hash, _ := GenerateBcryptHash("secret")
	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:    "MyApp",
		Htpasswd: htpasswd,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called")
	})

	handler := HandleBasicAuth(validator)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth != `Basic realm="MyApp"` {
		t.Errorf("expected realm MyApp, got %s", wwwAuth)
	}
}

func TestHandleBasicAuth_StripAuthDisabled(t *testing.T) {
	hash, _ := GenerateBcryptHash("secret")
	htpasswd := "admin:" + hash

	config := &pb.BasicAuthConfig{
		Realm:     "Test",
		Htpasswd:  htpasswd,
		StripAuth: false,
	}

	validator, err := NewBasicAuthValidator(config)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Authorization header should NOT be stripped
		if auth := r.Header.Get("Authorization"); auth == "" {
			t.Error("expected Authorization header to be preserved")
		}
	})

	handler := HandleBasicAuth(validator)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
}

func TestDetectHashType(t *testing.T) {
	tests := []struct {
		hash     string
		expected string
	}{
		{"$2y$10$abc123", "bcrypt"},
		{"$2a$10$abc123", "bcrypt"},
		{"$2b$10$abc123", "bcrypt"},
		{"{SHA256}base64hash", "sha256"},
		{"$apr1$salt$hash", "md5"},
		{"unknownformat", "bcrypt"}, // default
	}

	for _, tt := range tests {
		got := detectHashType(tt.hash)
		if got != tt.expected {
			t.Errorf("detectHashType(%q) = %q, want %q", tt.hash, got, tt.expected)
		}
	}
}
