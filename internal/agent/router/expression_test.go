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

package router

import (
	"net/http"
	"net/url"
	"testing"
)

func makeRequest(method, path string, headers map[string]string) *http.Request {
	u, _ := url.Parse("http://example.com" + path)
	r := &http.Request{
		Method:     method,
		URL:        u,
		Header:     http.Header{},
		RemoteAddr: "10.0.0.1:12345",
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestCompileExpression_Empty(t *testing.T) {
	node, err := CompileExpression("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := makeRequest("GET", "/", nil)
	if !node.Evaluate(r) {
		t.Error("empty expression should evaluate to true")
	}
}

func TestCompileExpression_HeaderMatch(t *testing.T) {
	node, err := CompileExpression(`header:X-Env == "staging"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/", map[string]string{"X-Env": "staging"})
	if !node.Evaluate(r) {
		t.Error("should match when header matches")
	}

	r = makeRequest("GET", "/", map[string]string{"X-Env": "production"})
	if node.Evaluate(r) {
		t.Error("should not match when header doesn't match")
	}
}

func TestCompileExpression_HeaderNotEqual(t *testing.T) {
	node, err := CompileExpression(`header:X-Env != "production"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/", map[string]string{"X-Env": "staging"})
	if !node.Evaluate(r) {
		t.Error("should match when header is not production")
	}
}

func TestCompileExpression_PathPrefix(t *testing.T) {
	node, err := CompileExpression(`path prefix "/api"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/api/v1/users", nil)
	if !node.Evaluate(r) {
		t.Error("should match /api prefix")
	}

	r = makeRequest("GET", "/health", nil)
	if node.Evaluate(r) {
		t.Error("should not match /health")
	}
}

func TestCompileExpression_PathExact(t *testing.T) {
	node, err := CompileExpression(`path exact "/health"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/health", nil)
	if !node.Evaluate(r) {
		t.Error("should match exact /health")
	}

	r = makeRequest("GET", "/health/check", nil)
	if node.Evaluate(r) {
		t.Error("should not match /health/check")
	}
}

func TestCompileExpression_MethodMatch(t *testing.T) {
	node, err := CompileExpression(`method == "POST"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("POST", "/", nil)
	if !node.Evaluate(r) {
		t.Error("should match POST")
	}

	r = makeRequest("GET", "/", nil)
	if node.Evaluate(r) {
		t.Error("should not match GET")
	}
}

func TestCompileExpression_AND(t *testing.T) {
	node, err := CompileExpression(`header:X-Env == "staging" AND path prefix "/api"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both match
	r := makeRequest("GET", "/api/v1", map[string]string{"X-Env": "staging"})
	if !node.Evaluate(r) {
		t.Error("should match when both conditions met")
	}

	// Only header matches
	r = makeRequest("GET", "/health", map[string]string{"X-Env": "staging"})
	if node.Evaluate(r) {
		t.Error("should not match when only header matches")
	}

	// Only path matches
	r = makeRequest("GET", "/api/v1", map[string]string{"X-Env": "production"})
	if node.Evaluate(r) {
		t.Error("should not match when only path matches")
	}
}

func TestCompileExpression_OR(t *testing.T) {
	node, err := CompileExpression(`path prefix "/api" OR path prefix "/v2"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/api/v1", nil)
	if !node.Evaluate(r) {
		t.Error("should match /api")
	}

	r = makeRequest("GET", "/v2/users", nil)
	if !node.Evaluate(r) {
		t.Error("should match /v2")
	}

	r = makeRequest("GET", "/health", nil)
	if node.Evaluate(r) {
		t.Error("should not match /health")
	}
}

func TestCompileExpression_NOT(t *testing.T) {
	node, err := CompileExpression(`NOT path prefix "/admin"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/api", nil)
	if !node.Evaluate(r) {
		t.Error("should match non-admin path")
	}

	r = makeRequest("GET", "/admin/settings", nil)
	if node.Evaluate(r) {
		t.Error("should not match admin path")
	}
}

func TestCompileExpression_Complex(t *testing.T) {
	expr := `(header:X-Env == "staging") AND (path prefix "/api" OR path prefix "/v2")`
	node, err := CompileExpression(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/api/v1", map[string]string{"X-Env": "staging"})
	if !node.Evaluate(r) {
		t.Error("should match staging + /api")
	}

	r = makeRequest("GET", "/v2/users", map[string]string{"X-Env": "staging"})
	if !node.Evaluate(r) {
		t.Error("should match staging + /v2")
	}

	r = makeRequest("GET", "/api/v1", map[string]string{"X-Env": "production"})
	if node.Evaluate(r) {
		t.Error("should not match production + /api")
	}

	r = makeRequest("GET", "/health", map[string]string{"X-Env": "staging"})
	if node.Evaluate(r) {
		t.Error("should not match staging + /health")
	}
}

func TestCompileExpression_SourceIP(t *testing.T) {
	node, err := CompileExpression(`source_ip in "10.0.0.0/8"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:45678"
	if !node.Evaluate(r) {
		t.Error("should match 10.x.x.x")
	}

	r.RemoteAddr = "192.168.1.1:45678"
	if node.Evaluate(r) {
		t.Error("should not match 192.168.x.x")
	}
}

func TestCompileExpression_QueryParam(t *testing.T) {
	node, err := CompileExpression(`query:env == "test"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := makeRequest("GET", "/api?env=test", nil)
	if !node.Evaluate(r) {
		t.Error("should match query param env=test")
	}

	r = makeRequest("GET", "/api?env=prod", nil)
	if node.Evaluate(r) {
		t.Error("should not match query param env=prod")
	}
}

func TestCompileExpression_Invalid(t *testing.T) {
	tests := []string{
		`header:X-Env`,
		`unknown_keyword "foo"`,
		`source_ip in "invalid-cidr"`,
		`(header:X-Env == "test"`,
	}

	for _, expr := range tests {
		_, err := CompileExpression(expr)
		if err == nil {
			t.Errorf("expected error for expression %q", expr)
		}
	}
}

func TestCompileExpression_NestedParens(t *testing.T) {
	expr := `(path prefix "/api" AND method == "GET") OR (path prefix "/admin" AND header:X-Admin == "true")`
	node, err := CompileExpression(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// GET /api -> true
	r := makeRequest("GET", "/api/v1", nil)
	if !node.Evaluate(r) {
		t.Error("should match GET /api")
	}

	// POST /api -> false
	r = makeRequest("POST", "/api/v1", nil)
	if node.Evaluate(r) {
		t.Error("should not match POST /api")
	}

	// GET /admin with X-Admin: true -> true
	r = makeRequest("GET", "/admin", map[string]string{"X-Admin": "true"})
	if !node.Evaluate(r) {
		t.Error("should match admin with header")
	}

	// GET /admin without header -> false
	r = makeRequest("GET", "/admin", nil)
	if node.Evaluate(r) {
		t.Error("should not match admin without header")
	}
}
