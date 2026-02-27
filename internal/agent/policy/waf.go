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
	"errors"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)
var (
	errWAFConfigIsNil = errors.New("WAF config is nil")
)


// WAFFailMode represents how the WAF behaves when processing errors occur
type WAFFailMode string

const (
	// WAFFailClosed blocks requests when WAF processing errors occur (security-first default)
	WAFFailClosed WAFFailMode = "closed"
	// WAFFailOpen allows requests through when WAF processing errors occur
	WAFFailOpen WAFFailMode = "open"

	// maxWAFBodyHardCap is the absolute maximum body size (100 MB) that the
	// WAF will read, even when body inspection is disabled (MaxBodySize < 0).
	// This prevents unbounded memory consumption from very large payloads.
	maxWAFBodyHardCap int64 = 100 << 20
)

// WAFEngine wraps the Coraza WAF engine with NovaEdge-specific configuration
type WAFEngine struct {
	waf          coraza.WAF
	config       *pb.WAFConfig
	failMode     WAFFailMode
	logger       *zap.Logger
	audit        *WAFAuditLogger
	matchCounter *WAFMatchCounter
	mu           sync.RWMutex
}

// NewWAFEngine creates a new WAF engine from protobuf configuration
func NewWAFEngine(config *pb.WAFConfig, logger *zap.Logger) (*WAFEngine, error) {
	if config == nil {
		return nil, errWAFConfigIsNil
	}

	wafConfig := coraza.NewWAFConfig()

	// Build directives based on configuration
	directives := buildWAFDirectives(config)

	for _, directive := range directives {
		wafConfig = wafConfig.WithDirectives(directive)
	}

	// Apply custom rules if provided
	for _, rule := range config.CustomRules {
		wafConfig = wafConfig.WithDirectives(rule)
	}

	waf, err := coraza.NewWAF(wafConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create WAF engine: %w", err)
	}

	// Determine fail mode: default to fail-closed (security-first)
	failMode := WAFFailClosed
	if strings.EqualFold(config.GetFailMode(), "open") {
		failMode = WAFFailOpen
	}

	return &WAFEngine{
		waf:          waf,
		config:       config,
		failMode:     failMode,
		logger:       logger,
		audit:        NewWAFAuditLogger(logger),
		matchCounter: NewWAFMatchCounter(10 * time.Minute),
	}, nil
}

// buildWAFDirectives constructs Coraza directives from WAF configuration
func buildWAFDirectives(config *pb.WAFConfig) []string {
	directives := make([]string, 0, 8+len(config.RuleExclusions))

	// Enable SecRule Engine based on mode
	switch config.Mode {
	case "detection":
		directives = append(directives, "SecRuleEngine DetectionOnly")
	case "prevention":
		directives = append(directives, "SecRuleEngine On")
	default:
		directives = append(directives, "SecRuleEngine On")
	}

	// Enable request body inspection (required for body-based rules)
	directives = append(directives, "SecRequestBodyAccess On")

	// Set audit logging
	directives = append(directives, "SecAuditEngine On")
	directives = append(directives, `SecAuditLogParts ABCFHZ`)

	// Set anomaly scoring thresholds
	if config.AnomalyThreshold > 0 {
		directives = append(directives,
			fmt.Sprintf(`SecAction "id:900110,phase:1,nolog,pass,t:none,setvar:tx.inbound_anomaly_score_threshold=%d"`, config.AnomalyThreshold),
		)
	}

	// Set paranoia level
	paranoia := config.ParanoiaLevel
	if paranoia < 1 {
		paranoia = 1
	}
	if paranoia > 4 {
		paranoia = 4
	}
	directives = append(directives,
		fmt.Sprintf(`SecAction "id:900000,phase:1,nolog,pass,t:none,setvar:tx.paranoia_level=%d"`, paranoia),
	)

	// OWASP CRS-compatible rules at the configured paranoia level
	directives = append(directives, GetCRSRules(paranoia)...)

	// Add rule exclusions (must come after rules are defined)
	for _, exclusion := range config.RuleExclusions {
		directives = append(directives,
			fmt.Sprintf(`SecRuleRemoveById %s`, exclusion),
		)
	}

	return directives
}

// WAFResult contains the result of WAF processing
type WAFResult struct {
	Interruption *types.Interruption
	MatchedRules int
	TopRuleID    int
	TopCategory  string
}

// ProcessRequest processes an HTTP request through the WAF engine
func (w *WAFEngine) ProcessRequest(r *http.Request) (*types.Interruption, error) {
	result, err := w.ProcessRequestDetailed(r)
	if err != nil {
		return nil, err
	}
	return result.Interruption, nil
}

// ProcessRequestDetailed processes an HTTP request and returns detailed match information
func (w *WAFEngine) ProcessRequestDetailed(r *http.Request) (*WAFResult, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	tx := w.waf.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		if closeErr := tx.Close(); closeErr != nil {
			w.logger.Debug("Error closing WAF transaction", zap.Error(closeErr))
		}
	}()

	// Process connection
	clientIP := extractClientIP(r)
	var clientPort int
	if parts := strings.Split(r.RemoteAddr, ":"); len(parts) > 1 {
		if p, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			clientPort = p
		}
	}
	tx.ProcessConnection(clientIP, clientPort, "", 0)

	// Process URI
	tx.ProcessURI(r.URL.String(), r.Method, r.Proto)

	// Process headers
	for name, values := range r.Header {
		for _, value := range values {
			tx.AddRequestHeader(name, value)
		}
	}

	// Process request headers phase
	if interruption := tx.ProcessRequestHeaders(); interruption != nil {
		w.logInterruption(interruption, r)
		return &WAFResult{Interruption: interruption, MatchedRules: 1, TopRuleID: interruption.RuleID, TopCategory: ruleCategory(interruption.RuleID)}, nil
	}

	// Warn on requests with both Content-Length and Transfer-Encoding headers.
	// Per RFC 7230 section 3.3.3, a sender MUST NOT send both; the presence
	// of both may indicate a request smuggling attempt.
	if r.Header.Get("Content-Length") != "" && r.Header.Get("Transfer-Encoding") != "" {
		w.logger.Warn("Ambiguous request encoding: both Content-Length and Transfer-Encoding headers present (possible smuggling attempt)",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("content_length", r.Header.Get("Content-Length")),
			zap.String("transfer_encoding", r.Header.Get("Transfer-Encoding")),
		)
	}

	// Buffer request body with size limit for WAF inspection.
	// MaxBodySize > 0: inspect up to that many bytes
	// MaxBodySize == 0: use default 128KB (backward compatible)
	// MaxBodySize < 0: skip body inspection entirely
	var bodyBytes []byte
	var fullBody []byte
	if r.Body != nil && w.config.MaxBodySize < 0 {
		// Negative: skip body inspection, read body for downstream with hard cap
		var readErr error
		fullBody, readErr = io.ReadAll(io.LimitReader(r.Body, maxWAFBodyHardCap))
		if readErr != nil {
			w.logger.Error("Error reading request body", zap.Error(readErr))
			return nil, fmt.Errorf("failed to read request body: %w", readErr)
		}
		r.Body = io.NopCloser(bytes.NewReader(fullBody))
		return &WAFResult{}, nil
	} else if r.Body != nil {
		maxSize := w.config.MaxBodySize
		if maxSize <= 0 {
			maxSize = 131072 // Default 128KB
		}
		// Read up to maxSize bytes for WAF inspection
		limitedReader := io.LimitReader(r.Body, maxSize)
		var readErr error
		bodyBytes, readErr = io.ReadAll(limitedReader)
		if readErr != nil {
			w.logger.Error("Error reading request body for WAF", zap.Error(readErr))
			return nil, fmt.Errorf("failed to read request body: %w", readErr)
		}
		// Read remaining body (not inspected) for downstream with hard cap
		remainCap := maxWAFBodyHardCap - int64(len(bodyBytes))
		if remainCap < 0 {
			remainCap = 0
		}
		remaining, _ := io.ReadAll(io.LimitReader(r.Body, remainCap))
		fullBody = bodyBytes
		fullBody = append(fullBody, remaining...)
	}

	// Process request body phase using buffered (size-limited) body
	if len(bodyBytes) > 0 {
		bodyReader := bytes.NewReader(bodyBytes)
		if interruption, _, err := tx.ReadRequestBodyFrom(bodyReader); err != nil {
			w.logger.Error("Error reading request body for WAF", zap.Error(err))
		} else if interruption != nil {
			r.Body = io.NopCloser(bytes.NewReader(fullBody))
			w.logInterruption(interruption, r)
			return &WAFResult{Interruption: interruption, MatchedRules: 1, TopRuleID: interruption.RuleID, TopCategory: ruleCategory(interruption.RuleID)}, nil
		}
	}

	if interruption, err := tx.ProcessRequestBody(); err != nil {
		w.logger.Error("Error processing request body for WAF", zap.Error(err))
	} else if interruption != nil {
		r.Body = io.NopCloser(bytes.NewReader(fullBody))
		w.logInterruption(interruption, r)
		return &WAFResult{Interruption: interruption, MatchedRules: 1, TopRuleID: interruption.RuleID, TopCategory: ruleCategory(interruption.RuleID)}, nil
	}

	// Restore full request body for downstream handlers
	r.Body = io.NopCloser(bytes.NewReader(fullBody))

	// Collect matched rules info (for detection mode where there's no interruption)
	result := &WAFResult{}
	matched := tx.MatchedRules()
	result.MatchedRules = len(matched)
	if len(matched) > 0 {
		// Use the last matched rule as the "top" rule (highest phase/priority)
		lastRule := matched[len(matched)-1]
		result.TopRuleID = lastRule.Rule().ID()
		result.TopCategory = ruleCategory(lastRule.Rule().ID())
	}
	return result, nil
}

// logInterruption logs a WAF interruption event and updates per-IP match counter
func (w *WAFEngine) logInterruption(interruption *types.Interruption, r *http.Request) {
	ruleID := strconv.Itoa(interruption.RuleID)
	category := ruleCategory(interruption.RuleID)
	clientIP := extractClientIP(r)

	metrics.WAFRequestsBlocked.Inc()
	metrics.WAFRulesMatched.WithLabelValues(ruleID, category).Inc()
	metrics.WAFAnomalyScore.Observe(float64(interruption.Status))
	w.matchCounter.Increment(clientIP)

	w.logger.Warn("WAF rule matched",
		zap.Int("rule_id", interruption.RuleID),
		zap.String("category", category),
		zap.String("action", interruption.Action),
		zap.Int("status", interruption.Status),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
	)
}

// GetMatchCount returns the WAF match count for the given IP address.
// This can be used by the rate limiter to dynamically throttle repeat offenders.
func (w *WAFEngine) GetMatchCount(ip string) int {
	return w.matchCounter.Get(ip)
}

// ruleCategory maps a CRS rule ID to its attack category
func ruleCategory(ruleID int) string {
	prefix := ruleID / 1000
	switch prefix {
	case 920:
		return "protocol"
	case 921:
		return "protocol"
	case 930:
		return "lfi"
	case 932:
		return "rce"
	case 933:
		return "injection-php"
	case 934:
		return "ssti"
	case 941:
		return "xss"
	case 942:
		return "sqli"
	case 943:
		return "fixation"
	case 950:
		return "data-leakage"
	default:
		return "other"
	}
}

// GetFailMode returns the configured fail mode for the WAF engine
func (w *WAFEngine) GetFailMode() WAFFailMode {
	return w.failMode
}

// HandleWAF is HTTP middleware for WAF protection
func HandleWAF(engine *WAFEngine) func(http.Handler) http.Handler {
	// Pre-create response inspection WAF if enabled
	var respWAF coraza.WAF
	if engine != nil && engine.config.ResponseBodyInspection {
		var err error
		respWAF, err = newResponseInspectionWAF(engine)
		if err != nil {
			engine.logger.Error("Failed to create response inspection WAF, disabling response inspection", zap.Error(err))
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if engine == nil || !engine.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			result, err := engine.ProcessRequestDetailed(r)
			if err != nil {
				failMode := string(engine.failMode)
				if engine.failMode == WAFFailClosed {
					metrics.WAFProcessingErrorsTotal.WithLabelValues(failMode, "blocked").Inc()
					engine.audit.LogProcessingError(r, err, engine.failMode, "blocked")
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}
				metrics.WAFProcessingErrorsTotal.WithLabelValues(failMode, "allowed").Inc()
				engine.audit.LogProcessingError(r, err, engine.failMode, "allowed")
				next.ServeHTTP(w, r)
				return
			}

			if result.Interruption != nil {
				if engine.config.Mode != "detection" {
					engine.audit.LogRequestBlocked(r, result.Interruption, "prevention")
					status := result.Interruption.Status
					if status == 0 {
						status = http.StatusForbidden
					}
					http.Error(w, "Forbidden", status)
					return
				}
				// Detection mode: log but allow; add score header
				engine.audit.LogRequestBlocked(r, result.Interruption, "detection")
				w.Header().Set("X-WAF-Score", strconv.Itoa(result.Interruption.Status))
				w.Header().Set("X-WAF-Rule", strconv.Itoa(result.Interruption.RuleID))
			} else if result.MatchedRules > 0 {
				engine.audit.LogRequestDetected(r, result)
				w.Header().Set("X-WAF-Matches", strconv.Itoa(result.MatchedRules))
				w.Header().Set("X-WAF-Rule", strconv.Itoa(result.TopRuleID))
			}

			// If response body inspection is enabled, wrap the writer
			if respWAF != nil {
				tx := respWAF.NewTransaction()
				maxRespSize := engine.config.MaxResponseBodySize
				if maxRespSize <= 0 {
					maxRespSize = 131072 // Default 128KB
				}
				rw := &wafResponseWriter{
					ResponseWriter: w,
					tx:             tx,
					engine:         engine,
					request:        r,
					maxBodySize:    maxRespSize,
				}
				next.ServeHTTP(rw, r)
				rw.finish()
				tx.ProcessLogging()
				if closeErr := tx.Close(); closeErr != nil {
					engine.logger.Debug("Error closing WAF response transaction", zap.Error(closeErr))
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
