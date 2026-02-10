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

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewWAFEngine_Prevention(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil WAF engine")
	}
}

func TestNewWAFEngine_Detection(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    2,
		AnomalyThreshold: 10,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil WAF engine")
	}
}

func TestWAFEngine_BlockSQLInjection(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	// Create SQL injection request
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?id=1'+OR+'1'='1", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}

	if interruption == nil {
		t.Error("expected WAF to detect SQL injection")
	}
}

func TestWAFEngine_BlockXSS(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	// Create XSS request
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?name=<script>alert('xss')</script>", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}

	if interruption == nil {
		t.Error("expected WAF to detect XSS attack")
	}
}

func TestWAFEngine_AllowCleanRequest(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	// Create clean request
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/users?page=1&limit=10", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}

	if interruption != nil {
		t.Errorf("expected clean request to pass WAF, but got interruption: ruleID=%d", interruption.RuleID)
	}
}

func TestHandleWAF_PreventionMode(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	handler := HandleWAF(engine)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := handler(next)

	// SQL injection should be blocked
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=SELECT+*+FROM+users+WHERE+1=1", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for SQL injection in prevention mode, got %d", w.Code)
	}
}

func TestHandleWAF_DetectionMode(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	handler := HandleWAF(engine)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := handler(next)

	// SQL injection should be logged but allowed in detection mode
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=SELECT+*+FROM+users+WHERE+1=1", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for SQL injection in detection mode, got %d", w.Code)
	}
}

func TestHandleWAF_Disabled(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled: false,
		Mode:    "prevention",
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	handler := HandleWAF(engine)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := handler(next)

	// Even SQL injection should pass when WAF is disabled
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=SELECT+*+FROM+users", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when WAF is disabled, got %d", w.Code)
	}
}

func TestHandleWAF_NilEngine(t *testing.T) {
	handler := HandleWAF(nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := handler(next)

	// Nil engine should pass all requests
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=SELECT+*+FROM+users", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when WAF engine is nil, got %d", w.Code)
	}
}

func TestWAFEngine_CustomRules(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		CustomRules: []string{
			`SecRule REQUEST_HEADERS:X-Bad-Header "@rx evil" "id:9001,phase:1,deny,status:403,msg:'Custom rule triggered'"`,
		},
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine with custom rules: %v", err)
	}

	// Request with the custom bad header should be blocked
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Header.Set("X-Bad-Header", "evil-value")
	req.RemoteAddr = "192.168.1.1:12345"

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}

	if interruption == nil {
		t.Error("expected custom rule to trigger on bad header")
	}
}

func TestBuildWAFDirectives(t *testing.T) {
	config := &pb.WAFConfig{
		Mode:             "prevention",
		ParanoiaLevel:    2,
		AnomalyThreshold: 10,
		RuleExclusions:   []string{"1001"},
	}

	directives := buildWAFDirectives(config)

	if len(directives) == 0 {
		t.Fatal("expected non-empty directives")
	}

	// Check for required directives
	hasEngineOn := false
	hasParanoia := false
	hasExclusion := false

	for _, d := range directives {
		if d == "SecRuleEngine On" {
			hasEngineOn = true
		}
		if contains(d, "tx.paranoia_level=2") {
			hasParanoia = true
		}
		if contains(d, "SecRuleRemoveById 1001") {
			hasExclusion = true
		}
	}

	if !hasEngineOn {
		t.Error("expected SecRuleEngine On directive")
	}
	if !hasParanoia {
		t.Error("expected paranoia level 2 directive")
	}
	if !hasExclusion {
		t.Error("expected rule exclusion directive for 1001")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
