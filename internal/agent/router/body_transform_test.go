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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestBodyTransform_AddOperation(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/newField", Value: json.RawMessage(`"hello"`)},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"existing":"value"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}
	if result["newField"] != "hello" {
		t.Errorf("expected newField='hello', got %v", result["newField"])
	}
	if result["existing"] != "value" {
		t.Errorf("expected existing='value', got %v", result["existing"])
	}
}

func TestBodyTransform_RemoveOperation(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "remove", Path: "/removeMe"},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"keep":"yes","removeMe":"gone"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}
	if _, ok := result["removeMe"]; ok {
		t.Error("expected removeMe to be removed")
	}
	if result["keep"] != "yes" {
		t.Errorf("expected keep='yes', got %v", result["keep"])
	}
}

func TestBodyTransform_ReplaceOperation(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "replace", Path: "/status", Value: json.RawMessage(`"updated"`)},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"status":"original"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}
	if result["status"] != "updated" {
		t.Errorf("expected status='updated', got %v", result["status"])
	}
}

func TestBodyTransform_MoveOperation(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "move", From: "/old", Path: "/new"},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"old":"value","other":"keep"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}
	if _, ok := result["old"]; ok {
		t.Error("expected old field to be removed after move")
	}
	if result["new"] != "value" {
		t.Errorf("expected new='value', got %v", result["new"])
	}
}

func TestBodyTransform_CopyOperation(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "copy", From: "/source", Path: "/dest"},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"source":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}
	if result["source"] != "data" {
		t.Errorf("expected source='data', got %v", result["source"])
	}
	if result["dest"] != "data" {
		t.Errorf("expected dest='data', got %v", result["dest"])
	}
}

func TestBodyTransform_NonJSONPassthrough(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/field", Value: json.RawMessage(`"val"`)},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := "plain text content"
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	if string(capturedBody) != body {
		t.Errorf("expected body to pass through unchanged, got %q", string(capturedBody))
	}
}

func TestBodyTransform_MaxBodySizeEnforcement(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/field", Value: json.RawMessage(`"val"`)},
		},
		MaxBodySize: 20, // Very small limit
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	// Body larger than 20 bytes
	body := `{"field":"this is a long value that exceeds the limit"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
}

func TestBodyTransform_ResponseTransformation(t *testing.T) {
	respOps := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/injected", Value: json.RawMessage(`true`)},
			{Op: "remove", Path: "/internal"},
		},
	}
	m := NewBodyTransformMiddleware(nil, respOps, zap.NewNop())

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"public","internal":"secret"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["injected"] != true {
		t.Errorf("expected injected=true, got %v", result["injected"])
	}
	if _, ok := result["internal"]; ok {
		t.Error("expected internal field to be removed")
	}
	if result["data"] != "public" {
		t.Errorf("expected data='public', got %v", result["data"])
	}
}

func TestBodyTransform_RequestAndResponseTransformation(t *testing.T) {
	reqOps := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/requestTag", Value: json.RawMessage(`"tagged"`)},
		},
	}
	respOps := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/responseTag", Value: json.RawMessage(`"tagged"`)},
		},
	}
	m := NewBodyTransformMiddleware(reqOps, respOps, zap.NewNop())

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the request body back as response.
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))

	body := `{"original":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Request transformation should have added requestTag.
	if result["requestTag"] != "tagged" {
		t.Errorf("expected requestTag='tagged', got %v", result["requestTag"])
	}
	// Response transformation should have added responseTag.
	if result["responseTag"] != "tagged" {
		t.Errorf("expected responseTag='tagged', got %v", result["responseTag"])
	}
}

func TestBodyTransform_NilConfig(t *testing.T) {
	m := NewBodyTransformMiddleware(nil, nil, zap.NewNop())

	body := `{"data":"unchanged"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	if string(capturedBody) != body {
		t.Errorf("expected body unchanged, got %q", string(capturedBody))
	}
}

func TestBodyTransform_NestedPath(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "replace", Path: "/nested/value", Value: json.RawMessage(`42`)},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	body := `{"nested":{"value":1,"other":"keep"}}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	var result map[string]interface{}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to unmarshal transformed body: %v", err)
	}

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("expected nested to be an object")
	}
	if nested["value"] != float64(42) {
		t.Errorf("expected nested.value=42, got %v", nested["value"])
	}
	if nested["other"] != "keep" {
		t.Errorf("expected nested.other='keep', got %v", nested["other"])
	}
}

func TestBodyTransform_EmptyBody(t *testing.T) {
	ops := &BodyTransformConfig{
		Operations: []TransformOperation{
			{Op: "add", Path: "/field", Value: json.RawMessage(`"val"`)},
		},
	}
	m := NewBodyTransformMiddleware(ops, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var capturedBody []byte
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	// Empty body should pass through unchanged.
	if len(capturedBody) != 0 {
		t.Errorf("expected empty body to pass through, got %q", string(capturedBody))
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"Application/JSON", true},
		{"application/vnd.api+json", true},
		{"text/plain", false},
		{"text/html", false},
		{"", false},
		{"multipart/form-data", false},
	}

	for _, tt := range tests {
		got := isJSONContentType(tt.ct)
		if got != tt.want {
			t.Errorf("isJSONContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestParsePointer(t *testing.T) {
	tests := []struct {
		input string
		parts []string
		err   bool
	}{
		{"/foo", []string{"foo"}, false},
		{"/foo/bar", []string{"foo", "bar"}, false},
		{"/a~0b", []string{"a~b"}, false},
		{"/a~1b", []string{"a/b"}, false},
		{"", nil, false},
		{"noslash", nil, true},
	}

	for _, tt := range tests {
		parts, err := parsePointer(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parsePointer(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePointer(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if len(parts) != len(tt.parts) {
			t.Errorf("parsePointer(%q): got %v, want %v", tt.input, parts, tt.parts)
			continue
		}
		for i, p := range parts {
			if p != tt.parts[i] {
				t.Errorf("parsePointer(%q)[%d]: got %q, want %q", tt.input, i, p, tt.parts[i])
			}
		}
	}
}
