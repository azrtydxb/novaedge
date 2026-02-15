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
	"time"

	"github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"
)

// WAFAuditLogger emits structured audit events for SIEM integration.
// Each event contains all fields needed for security analysis: timestamp,
// client info, rule details, action taken, and request metadata.
type WAFAuditLogger struct {
	logger *zap.Logger
}

// NewWAFAuditLogger creates an audit logger using the provided zap logger.
// The caller should configure the zap logger with JSON encoding for SIEM output.
func NewWAFAuditLogger(logger *zap.Logger) *WAFAuditLogger {
	return &WAFAuditLogger{logger: logger.Named("waf.audit")}
}

// LogRequestBlocked emits an audit event when a request is blocked
func (a *WAFAuditLogger) LogRequestBlocked(r *http.Request, interruption *types.Interruption, mode string) {
	a.logger.Warn("waf_request_blocked",
		zap.String("event_type", "request_blocked"),
		zap.String("timestamp", time.Now().UTC().Format(time.RFC3339Nano)),
		zap.String("client_ip", extractClientIP(r)),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("method", r.Method),
		zap.String("host", r.Host),
		zap.String("path", r.URL.Path),
		zap.String("query", r.URL.RawQuery),
		zap.String("user_agent", r.UserAgent()),
		zap.Int("rule_id", interruption.RuleID),
		zap.String("category", ruleCategory(interruption.RuleID)),
		zap.String("action", interruption.Action),
		zap.Int("status_code", interruption.Status),
		zap.String("mode", mode),
	)
}

// LogRequestDetected emits an audit event when rules match in detection mode
func (a *WAFAuditLogger) LogRequestDetected(r *http.Request, result *WAFResult) {
	a.logger.Info("waf_request_detected",
		zap.String("event_type", "request_detected"),
		zap.String("timestamp", time.Now().UTC().Format(time.RFC3339Nano)),
		zap.String("client_ip", extractClientIP(r)),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("method", r.Method),
		zap.String("host", r.Host),
		zap.String("path", r.URL.Path),
		zap.String("query", r.URL.RawQuery),
		zap.String("user_agent", r.UserAgent()),
		zap.Int("matched_rules", result.MatchedRules),
		zap.Int("top_rule_id", result.TopRuleID),
		zap.String("top_category", result.TopCategory),
		zap.String("mode", "detection"),
	)
}

// LogResponseBlocked emits an audit event when a response is blocked for data leakage
func (a *WAFAuditLogger) LogResponseBlocked(r *http.Request, interruption *types.Interruption) {
	a.logger.Warn("waf_response_blocked",
		zap.String("event_type", "response_blocked"),
		zap.String("timestamp", time.Now().UTC().Format(time.RFC3339Nano)),
		zap.String("client_ip", extractClientIP(r)),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("method", r.Method),
		zap.String("host", r.Host),
		zap.String("path", r.URL.Path),
		zap.Int("rule_id", interruption.RuleID),
		zap.String("category", ruleCategory(interruption.RuleID)),
		zap.String("action", interruption.Action),
		zap.Int("status_code", interruption.Status),
	)
}

// LogProcessingError emits an audit event when WAF processing fails
func (a *WAFAuditLogger) LogProcessingError(r *http.Request, err error, failMode WAFFailMode, action string) {
	a.logger.Error("waf_processing_error",
		zap.String("event_type", "processing_error"),
		zap.String("timestamp", time.Now().UTC().Format(time.RFC3339Nano)),
		zap.String("client_ip", extractClientIP(r)),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.Error(err),
		zap.String("fail_mode", string(failMode)),
		zap.String("action", action),
	)
}
