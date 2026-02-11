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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// WAFRuleSource defines where WAF rules are loaded from
type WAFRuleSource int

const (
	// WAFRuleSourceInline loads rules from inline configuration
	WAFRuleSourceInline WAFRuleSource = iota
	// WAFRuleSourceFile loads rules from a file
	WAFRuleSourceFile
	// WAFRuleSourceConfigMap loads rules from a Kubernetes ConfigMap (via snapshot)
	WAFRuleSourceConfigMap
)

// WAFRuleLoader manages loading WAF rules from various sources
type WAFRuleLoader struct {
	logger *zap.Logger
}

// NewWAFRuleLoader creates a new WAF rule loader
func NewWAFRuleLoader(logger *zap.Logger) *WAFRuleLoader {
	return &WAFRuleLoader{logger: logger}
}

// LoadFromFile loads WAF rules from a file on disk
func (l *WAFRuleLoader) LoadFromFile(path string) ([]string, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAF rules file %s: %w", cleanPath, err)
	}

	rules := parseRules(string(data))
	l.logger.Info("Loaded WAF rules from file",
		zap.String("path", cleanPath),
		zap.Int("rule_count", len(rules)),
	)
	return rules, nil
}

// LoadFromConfigMapData loads WAF rules from ConfigMap data
func (l *WAFRuleLoader) LoadFromConfigMapData(data map[string]string) []string {
	var allRules []string

	for key, content := range data {
		rules := parseRules(content)
		l.logger.Info("Loaded WAF rules from ConfigMap key",
			zap.String("key", key),
			zap.Int("rule_count", len(rules)),
		)
		allRules = append(allRules, rules...)
	}

	return allRules
}

// LoadInline processes inline rule strings
func (l *WAFRuleLoader) LoadInline(rules []string) []string {
	var validRules []string
	for _, rule := range rules {
		trimmed := strings.TrimSpace(rule)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			validRules = append(validRules, trimmed)
		}
	}
	return validRules
}

// parseRules parses a multi-line rules string into individual rule directives
func parseRules(content string) []string {
	var rules []string
	lines := strings.Split(content, "\n")

	var currentRule strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if currentRule.Len() > 0 {
				rules = append(rules, currentRule.String())
				currentRule.Reset()
			}
			continue
		}

		// Check for line continuation (backslash at end)
		if strings.HasSuffix(trimmed, "\\") {
			currentRule.WriteString(strings.TrimSuffix(trimmed, "\\"))
			currentRule.WriteString(" ")
			continue
		}

		currentRule.WriteString(trimmed)
		rules = append(rules, currentRule.String())
		currentRule.Reset()
	}

	// Don't forget remaining accumulated rule
	if currentRule.Len() > 0 {
		rules = append(rules, currentRule.String())
	}

	return rules
}

// GetDefaultParanoiaRules returns additional rules for higher paranoia levels
func GetDefaultParanoiaRules(level int32) []string {
	var rules []string

	if level >= 2 {
		// Paranoia level 2: stricter SQL injection and XSS rules
		rules = append(rules,
			`SecRule REQUEST_URI|ARGS|ARGS_NAMES "@rx (?i)(concat|char|ascii|substr|substring|mid|left|right|reverse)" "id:2001,phase:2,deny,status:403,log,msg:'PL2 SQL function detected',tag:'attack-sqli',severity:WARNING"`,
			`SecRule REQUEST_URI|ARGS "@rx (?i)(<[^>]*style[^>]*>|style\s*=)" "id:2002,phase:2,deny,status:403,log,msg:'PL2 XSS via style attribute',tag:'attack-xss',severity:WARNING"`,
		)
	}

	if level >= 3 {
		// Paranoia level 3: even stricter rules
		rules = append(rules,
			`SecRule REQUEST_URI|ARGS "@rx (?i)(sleep|benchmark|load_file|outfile|dumpfile)" "id:3001,phase:2,deny,status:403,log,msg:'PL3 SQL time-based injection',tag:'attack-sqli',severity:WARNING"`,
		)
	}

	if level >= 4 {
		// Paranoia level 4: maximum paranoia
		rules = append(rules,
			`SecRule REQUEST_URI|ARGS "@rx (?i)['"\;]" "id:4001,phase:2,deny,status:403,log,msg:'PL4 Special characters detected',tag:'paranoia-level/4',severity:NOTICE"`,
		)
	}

	return rules
}
