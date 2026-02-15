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
	"net/http"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// wafResponseWriter wraps http.ResponseWriter to inspect response bodies via WAF.
// It buffers up to maxBodySize bytes for inspection, then flushes everything to
// the underlying writer. If a data leakage pattern is detected, it replaces the
// response with a 502 error.
type wafResponseWriter struct {
	http.ResponseWriter
	tx          types.Transaction
	engine      *WAFEngine
	request     *http.Request
	buf         bytes.Buffer
	maxBodySize int64
	statusCode  int
	headersSent bool
	blocked     bool
}

func (w *wafResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	// Feed response headers to WAF transaction
	for name, values := range w.Header() {
		for _, v := range values {
			w.tx.AddResponseHeader(name, v)
		}
	}
	if interruption := w.tx.ProcessResponseHeaders(code, "HTTP/1.1"); interruption != nil {
		w.blocked = true
		w.engine.logResponseInterruption(interruption, w.request)
		w.ResponseWriter.WriteHeader(http.StatusBadGateway)
		w.headersSent = true
		return
	}
	// Don't send headers yet — wait until we've inspected the body
}

func (w *wafResponseWriter) Write(b []byte) (int, error) {
	if w.blocked {
		return len(b), nil // discard
	}
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	// Buffer up to maxBodySize for inspection
	if w.buf.Len() < int(w.maxBodySize) {
		remaining := int(w.maxBodySize) - w.buf.Len()
		toWrite := b
		if len(toWrite) > remaining {
			toWrite = toWrite[:remaining]
		}
		w.buf.Write(toWrite)
	}
	return len(b), nil
}

// finish processes the buffered response body through WAF and flushes to client
func (w *wafResponseWriter) finish() {
	if w.blocked || w.headersSent {
		return
	}

	bodyBytes := w.buf.Bytes()

	// Feed buffered body to WAF
	if len(bodyBytes) > 0 {
		if interruption, _, err := w.tx.WriteResponseBody(bodyBytes); err != nil {
			w.engine.logger.Error("Error writing response body for WAF", zap.Error(err))
		} else if interruption != nil {
			w.blocked = true
			w.engine.logResponseInterruption(interruption, w.request)
			w.ResponseWriter.WriteHeader(http.StatusBadGateway)
			return
		}
	}

	if interruption, err := w.tx.ProcessResponseBody(); err != nil {
		w.engine.logger.Error("Error processing response body for WAF", zap.Error(err))
	} else if interruption != nil {
		w.blocked = true
		w.engine.logResponseInterruption(interruption, w.request)
		w.ResponseWriter.WriteHeader(http.StatusBadGateway)
		return
	}

	// No leakage detected — flush the original response
	w.ResponseWriter.WriteHeader(w.statusCode)
	w.ResponseWriter.Write(bodyBytes)
}

// logResponseInterruption logs a WAF response body interruption
func (w *WAFEngine) logResponseInterruption(interruption *types.Interruption, r *http.Request) {
	metrics.WAFResponsesBlocked.Inc()
	metrics.WAFRulesMatched.Inc()

	w.logger.Warn("WAF response body rule matched",
		zap.Int("rule_id", interruption.RuleID),
		zap.String("action", interruption.Action),
		zap.Int("status", interruption.Status),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
	)
}

// newResponseInspectionWAF creates a Coraza WAF instance configured for response body inspection
func newResponseInspectionWAF(engine *WAFEngine) (coraza.WAF, error) {
	cfg := coraza.NewWAFConfig()
	cfg = cfg.WithDirectives("SecRuleEngine On")
	cfg = cfg.WithDirectives("SecResponseBodyAccess On")
	cfg = cfg.WithDirectives(`SecResponseBodyMimeType text/plain text/html text/xml application/json application/xml`)

	// Data leakage detection rules (response body phase 4)
	for _, rule := range getResponseBodyRules() {
		cfg = cfg.WithDirectives(rule)
	}

	return coraza.NewWAF(cfg)
}

// getResponseBodyRules returns rules for detecting data leakage in response bodies
func getResponseBodyRules() []string {
	return []string{
		// Credit card numbers (Visa, MasterCard, Amex, Discover)
		`SecRule RESPONSE_BODY "@rx \b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b" "id:950001,phase:4,deny,status:502,log,msg:'Data leakage: credit card number detected',tag:'data-leakage',severity:CRITICAL"`,
		// US Social Security Numbers (SSN)
		`SecRule RESPONSE_BODY "@rx \b[0-9]{3}-[0-9]{2}-[0-9]{4}\b" "id:950002,phase:4,deny,status:502,log,msg:'Data leakage: SSN pattern detected',tag:'data-leakage',severity:CRITICAL"`,
		// SQL error messages that leak internals
		`SecRule RESPONSE_BODY "@rx (?i)(SQL syntax.*?MySQL|Warning.*?\Wmysqli?_|valid MySQL result|PostgreSQL.*?ERROR|ORA-[0-9]{5}|Microsoft OLE DB Provider|ODBC SQL Server Driver|SQLite3::|pg_query\(\)|System\.Data\.SqlClient)" "id:950003,phase:4,deny,status:502,log,msg:'Data leakage: SQL error message',tag:'data-leakage',severity:CRITICAL"`,
		// Server-side stack traces
		`SecRule RESPONSE_BODY "@rx (?i)(at [a-zA-Z_$][a-zA-Z0-9_$]*\.(java|go|py|rb|cs|php|js|ts):[0-9]+|Traceback \(most recent call last\)|goroutine [0-9]+ \[)" "id:950004,phase:4,deny,status:502,log,msg:'Data leakage: stack trace detected',tag:'data-leakage',severity:WARNING"`,
		// Directory listing patterns
		`SecRule RESPONSE_BODY "@rx (?i)(<title>Index of /|<h1>Index of /|Directory listing for)" "id:950005,phase:4,deny,status:502,log,msg:'Data leakage: directory listing',tag:'data-leakage',severity:WARNING"`,
	}
}
