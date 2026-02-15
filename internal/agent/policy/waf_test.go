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
	"bytes"
	"fmt"
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
	req.RemoteAddr = testRemoteAddr

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
	req.RemoteAddr = testRemoteAddr

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
	req.RemoteAddr = testRemoteAddr

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
	req.RemoteAddr = testRemoteAddr
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
	req.RemoteAddr = testRemoteAddr
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
	req.RemoteAddr = testRemoteAddr
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
	req.RemoteAddr = testRemoteAddr
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
	req.RemoteAddr = testRemoteAddr

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

func TestWAFEngine_BlockPathTraversal(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?file=../../../../etc/passwd", nil)
	req.RemoteAddr = testRemoteAddr

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}
	if interruption == nil {
		t.Error("expected WAF to detect path traversal")
	}
}

func TestWAFEngine_BlockCommandInjection(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?cmd=;cat+/etc/passwd", nil)
	req.RemoteAddr = testRemoteAddr

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}
	if interruption == nil {
		t.Error("expected WAF to detect command injection")
	}
}

func TestWAFEngine_ParanoiaLevel2_SQLFunctions(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    2,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=concat(username,password)", nil)
	req.RemoteAddr = testRemoteAddr

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}
	if interruption == nil {
		t.Error("expected WAF PL2 to detect SQL function usage")
	}
}

func TestWAFEngine_ParanoiaLevel3_TimeBased(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    3,
		AnomalyThreshold: 5,
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?id=1+AND+sleep(5)", nil)
	req.RemoteAddr = testRemoteAddr

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}
	if interruption == nil {
		t.Error("expected WAF PL3 to detect time-based SQL injection")
	}
}

func TestWAFEngine_RuleExclusion(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		RuleExclusions:   []string{"942100", "942110", "942120", "942130"},
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	// SQL injection should pass since SQLi rules are excluded
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api?q=SELECT+username+FROM+users", nil)
	req.RemoteAddr = testRemoteAddr

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("WAF processing error: %v", err)
	}
	if interruption != nil {
		t.Error("expected SQL injection to pass with excluded rules")
	}
}

func TestWAFEngine_CleanRequestAllParanoiaLevels(t *testing.T) {
	for _, pl := range []int32{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("PL%d", pl), func(t *testing.T) {
			logger := zap.NewNop()
			config := &pb.WAFConfig{
				Enabled:          true,
				Mode:             "prevention",
				ParanoiaLevel:    pl,
				AnomalyThreshold: 5,
			}

			engine, err := NewWAFEngine(config, logger)
			if err != nil {
				t.Fatalf("failed to create WAF engine: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/api/users?page=1&limit=10", nil)
			req.RemoteAddr = testRemoteAddr

			interruption, err := engine.ProcessRequest(req)
			if err != nil {
				t.Fatalf("WAF processing error: %v", err)
			}
			if interruption != nil {
				t.Errorf("expected clean request to pass at PL%d, rule=%d", pl, interruption.RuleID)
			}
		})
	}
}

func TestGetCRSRules_LevelCoverage(t *testing.T) {
	pl1 := GetCRSRules(1)
	pl2 := GetCRSRules(2)
	pl3 := GetCRSRules(3)
	pl4 := GetCRSRules(4)

	if len(pl1) == 0 {
		t.Error("PL1 should have rules")
	}
	if len(pl2) <= len(pl1) {
		t.Error("PL2 should have more rules than PL1")
	}
	if len(pl3) <= len(pl2) {
		t.Error("PL3 should have more rules than PL2")
	}
	if len(pl4) <= len(pl3) {
		t.Error("PL4 should have more rules than PL3")
	}
}

func TestNewWAFEngine_DefaultFailMode(t *testing.T) {
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

	if engine.GetFailMode() != WAFFailClosed {
		t.Errorf("expected default fail mode to be closed, got %s", engine.GetFailMode())
	}
}

func TestNewWAFEngine_FailClosedExplicit(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		FailMode:         "closed",
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	if engine.GetFailMode() != WAFFailClosed {
		t.Errorf("expected fail mode closed, got %s", engine.GetFailMode())
	}
}

func TestNewWAFEngine_FailOpen(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		FailMode:         "open",
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	if engine.GetFailMode() != WAFFailOpen {
		t.Errorf("expected fail mode open, got %s", engine.GetFailMode())
	}
}

func TestNewWAFEngine_FailOpenCaseInsensitive(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		FailMode:         "Open",
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	if engine.GetFailMode() != WAFFailOpen {
		t.Errorf("expected fail mode open (case insensitive), got %s", engine.GetFailMode())
	}
}

func TestNewWAFEngine_UnknownFailModeDefaultsClosed(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		FailMode:         "unknown",
	}

	engine, err := NewWAFEngine(config, logger)
	if err != nil {
		t.Fatalf("failed to create WAF engine: %v", err)
	}

	if engine.GetFailMode() != WAFFailClosed {
		t.Errorf("expected unknown fail mode to default to closed, got %s", engine.GetFailMode())
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

func TestWAFEngine_MaxBodySize_Negative_SkipsBody(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:       true,
		Mode:          "prevention",
		ParanoiaLevel: 1,
		MaxBodySize:   -1, // Skip body inspection entirely
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	// Body contains SQLi but MaxBodySize=0 means no body inspection
	body := "username=admin' OR 1=1--"
	req := httptest.NewRequest("POST", "/login", stringReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if interruption != nil {
		t.Error("Body should not be inspected when MaxBodySize=0")
	}
}

func TestWAFEngine_MaxBodySize_LimitsInspection(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:       true,
		Mode:          "prevention",
		ParanoiaLevel: 1,
		MaxBodySize:   50, // Only inspect first 50 bytes
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	// Put attack payload after the 50-byte limit
	padding := make([]byte, 100)
	for i := range padding {
		padding[i] = 'A'
	}
	body := string(padding) + "'; DROP TABLE users; --"
	req := httptest.NewRequest("POST", "/api/data", stringReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Attack is past the limit — should not be caught
	if interruption != nil {
		t.Error("Attack past MaxBodySize should not be detected")
	}
}

func TestWAFEngine_MaxBodySize_Default_InspectsBody(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
		// MaxBodySize 0 = use default 128KB
		MaxBodySize: 0,
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	// SQLi in body should be detected with default body limit
	body := "id=1' OR '1'='1"
	req := httptest.NewRequest("POST", "/login", stringReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	interruption, err := engine.ProcessRequest(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if interruption == nil {
		t.Error("SQLi in body should be detected with default body size limit")
	}
}

func stringReader(s string) *stringReaderCloser {
	return &stringReaderCloser{reader: bytes.NewReader([]byte(s))}
}

type stringReaderCloser struct {
	reader *bytes.Reader
}

func (r *stringReaderCloser) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

func (r *stringReaderCloser) Close() error {
	return nil
}

func TestWAFEngineCache_HitAndMiss(t *testing.T) {
	cache := NewWAFEngineCache(zap.NewNop())

	config1 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	// First call: cache miss
	engine1, err := cache.GetOrCreate(config1)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}

	// Second call with same config: cache hit
	engine2, err := cache.GetOrCreate(config1)
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}
	if engine1 != engine2 {
		t.Error("Expected same engine instance from cache")
	}
	if cache.Size() != 1 {
		t.Errorf("Expected cache size still 1, got %d", cache.Size())
	}

	// Different config: cache miss
	config2 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    2,
		AnomalyThreshold: 10,
	}
	engine3, err := cache.GetOrCreate(config2)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if engine3 == engine1 {
		t.Error("Expected different engine for different config")
	}
	if cache.Size() != 2 {
		t.Errorf("Expected cache size 2, got %d", cache.Size())
	}

	// Purge
	cache.Purge()
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after purge, got %d", cache.Size())
	}
}

func TestHandleWAF_ResponseBodyInspection_BlocksCreditCard(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:                true,
		Mode:                   "prevention",
		ParanoiaLevel:          1,
		AnomalyThreshold:       5,
		ResponseBodyInspection: true,
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	// Backend handler that returns a credit card number in response
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"card": "4111111111111111"}`))
	})

	handler := HandleWAF(engine)(backend)
	req := httptest.NewRequest("GET", "/api/user", nil)
	req.RemoteAddr = testRemoteAddr
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("Expected 502 Bad Gateway for credit card leakage, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWAF_ResponseBodyInspection_AllowsCleanResponse(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:                true,
		Mode:                   "prevention",
		ParanoiaLevel:          1,
		AnomalyThreshold:       5,
		ResponseBodyInspection: true,
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok", "user": "john"}`))
	})

	handler := HandleWAF(engine)(backend)
	req := httptest.NewRequest("GET", "/api/user", nil)
	req.RemoteAddr = testRemoteAddr
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for clean response, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status": "ok", "user": "john"}` {
		t.Errorf("Response body mismatch: %s", rec.Body.String())
	}
}

func TestHandleWAF_DetectionMode_AddsHeaders(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := HandleWAF(engine)(backend)
	// Use a SQLi payload that triggers rules
	req := httptest.NewRequest("GET", "http://example.com/api?q=SELECT+*+FROM+users+WHERE+1%3D1", nil)
	req.RemoteAddr = testRemoteAddr
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 in detection mode, got %d", rec.Code)
	}
	// In detection mode, matched rules info should be in headers
	if matches := rec.Header().Get("X-WAF-Matches"); matches == "" {
		t.Error("Expected X-WAF-Matches header in detection mode")
	}
	if ruleID := rec.Header().Get("X-WAF-Rule"); ruleID == "" {
		t.Error("Expected X-WAF-Rule header in detection mode")
	}
}

func TestHandleWAF_ResponseBodyInspection_Disabled(t *testing.T) {
	config := &pb.WAFConfig{
		Enabled:                true,
		Mode:                   "prevention",
		ParanoiaLevel:          1,
		AnomalyThreshold:       5,
		ResponseBodyInspection: false,
	}
	engine, err := NewWAFEngine(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create WAF engine: %v", err)
	}

	// Backend returns a credit card number — should pass through since inspection is disabled
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"card": "4111111111111111"}`))
	})

	handler := HandleWAF(engine)(backend)
	req := httptest.NewRequest("GET", "/api/user", nil)
	req.RemoteAddr = testRemoteAddr
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK when response inspection is disabled, got %d", rec.Code)
	}
}
