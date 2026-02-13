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
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// WAFFailMode represents how the WAF behaves when processing errors occur
type WAFFailMode string

const (
	// WAFFailClosed blocks requests when WAF processing errors occur (security-first default)
	WAFFailClosed WAFFailMode = "closed"
	// WAFFailOpen allows requests through when WAF processing errors occur
	WAFFailOpen WAFFailMode = "open"
)

// WAFEngine wraps the Coraza WAF engine with NovaEdge-specific configuration
type WAFEngine struct {
	waf      coraza.WAF
	config   *pb.WAFConfig
	failMode WAFFailMode
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewWAFEngine creates a new WAF engine from protobuf configuration
func NewWAFEngine(config *pb.WAFConfig, logger *zap.Logger) (*WAFEngine, error) {
	if config == nil {
		return nil, fmt.Errorf("WAF config is nil")
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
		waf:      waf,
		config:   config,
		failMode: failMode,
		logger:   logger,
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

// ProcessRequest processes an HTTP request through the WAF engine
func (w *WAFEngine) ProcessRequest(r *http.Request) (*types.Interruption, error) {
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
		return interruption, nil
	}

	// Buffer request body before passing to WAF so downstream handlers can still read it
	var bodyBytes []byte
	if r.Body != nil {
		var readErr error
		bodyBytes, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			w.logger.Error("Error reading request body for WAF", zap.Error(readErr))
			return nil, fmt.Errorf("failed to read request body: %w", readErr)
		}
	}

	// Process request body phase using buffered body
	if len(bodyBytes) > 0 {
		bodyReader := bytes.NewReader(bodyBytes)
		if interruption, _, err := tx.ReadRequestBodyFrom(bodyReader); err != nil {
			w.logger.Error("Error reading request body for WAF", zap.Error(err))
		} else if interruption != nil {
			// Restore body before returning so downstream can still read it
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			w.logInterruption(interruption, r)
			return interruption, nil
		}
	}

	if interruption, err := tx.ProcessRequestBody(); err != nil {
		w.logger.Error("Error processing request body for WAF", zap.Error(err))
	} else if interruption != nil {
		// Restore body before returning so downstream can still read it
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		w.logInterruption(interruption, r)
		return interruption, nil
	}

	// Restore request body for downstream handlers
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return nil, nil
}

// logInterruption logs a WAF interruption event
func (w *WAFEngine) logInterruption(interruption *types.Interruption, r *http.Request) {
	metrics.WAFRequestsBlocked.Inc()
	metrics.WAFRulesMatched.Inc()
	metrics.WAFAnomalyScore.Observe(float64(interruption.Status))

	w.logger.Warn("WAF rule matched",
		zap.Int("rule_id", interruption.RuleID),
		zap.String("action", interruption.Action),
		zap.Int("status", interruption.Status),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
	)
}

// GetFailMode returns the configured fail mode for the WAF engine
func (w *WAFEngine) GetFailMode() WAFFailMode {
	return w.failMode
}

// HandleWAF is HTTP middleware for WAF protection
func HandleWAF(engine *WAFEngine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if engine == nil || !engine.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			interruption, err := engine.ProcessRequest(r)
			if err != nil {
				failMode := string(engine.failMode)
				if engine.failMode == WAFFailClosed {
					// Fail-closed: block the request on WAF processing error
					metrics.WAFProcessingErrorsTotal.WithLabelValues(failMode, "blocked").Inc()
					engine.logger.Error("WAF processing error, blocking request (fail-closed)",
						zap.Error(err),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr),
					)
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}
				// Fail-open: allow request through but log a warning
				metrics.WAFProcessingErrorsTotal.WithLabelValues(failMode, "allowed").Inc()
				engine.logger.Warn("WAF processing error, allowing request (fail-open)",
					zap.Error(err),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("remote_addr", r.RemoteAddr),
				)
				next.ServeHTTP(w, r)
				return
			}

			if interruption != nil {
				// In prevention mode, block the request
				if engine.config.Mode != "detection" {
					status := interruption.Status
					if status == 0 {
						status = http.StatusForbidden
					}
					http.Error(w, "Forbidden", status)
					return
				}
				// In detection mode, log but allow the request
				engine.logger.Info("WAF detection mode: would have blocked request",
					zap.String("path", r.URL.Path),
					zap.Int("rule_id", interruption.RuleID),
				)
			}

			next.ServeHTTP(w, r)
		})
	}
}
