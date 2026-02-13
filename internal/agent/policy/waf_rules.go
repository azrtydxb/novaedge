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
	lines := strings.Split(content, "\n")
	rules := make([]string, 0, len(lines))

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

// GetCRSRules returns OWASP Core Rule Set-compatible rules for the given paranoia level.
// Rules are cumulative: higher levels include all rules from lower levels.
// Level 1: Standard attack detection (SQLi, XSS, LFI, RCE, protocol violations)
// Level 2: Enhanced detection with stricter patterns
// Level 3: Strict detection including time-based and injection variants
// Level 4: Maximum paranoia with broad character restrictions
func GetCRSRules(level int32) []string {
	if level < 1 {
		level = 1
	}
	if level > 4 {
		level = 4
	}

	var rules []string

	// ── Paranoia Level 1: Standard Protection ──────────────────────────

	// 920: Protocol Enforcement
	rules = append(rules,
		// Reject requests with both Content-Length and Transfer-Encoding (request smuggling)
		`SecRule REQUEST_HEADERS:Content-Length "." "id:920100,phase:1,chain,deny,status:400,log,msg:'Request smuggling: dual content-length/transfer-encoding',tag:'attack-protocol',severity:CRITICAL"`,
		`SecRule REQUEST_HEADERS:Transfer-Encoding "." ""`,
		// Disallow unusual HTTP methods in phase 1
		`SecRule REQUEST_METHOD "!@rx ^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS|CONNECT|TRACE)$" "id:920101,phase:1,deny,status:405,log,msg:'Invalid HTTP method',tag:'attack-protocol',severity:WARNING"`,
	)

	// 921: HTTP Response Splitting / Request Smuggling
	rules = append(rules,
		// CR/LF in request headers (header injection)
		`SecRule REQUEST_HEADERS|REQUEST_URI|ARGS "@rx [\r\n]" "id:921100,phase:1,deny,status:400,log,msg:'HTTP header injection via CR/LF',tag:'attack-protocol',severity:CRITICAL"`,
	)

	// 930: Local File Inclusion (expanded from base rule 1003)
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)(\.\./|\.\.\\\\|%2e%2e%2f|%2e%2e/|\.%2e/|%2e\./|%252e%252e%252f|\.\.%00|\.\.%0d)" "id:930100,phase:2,deny,status:403,log,msg:'Path traversal detected',tag:'attack-lfi',severity:CRITICAL"`,
		`SecRule REQUEST_URI|ARGS "@rx (?i)(/etc/(passwd|shadow|hosts|issue|motd)|/proc/self/(environ|cmdline|fd)|/var/log/)" "id:930110,phase:2,deny,status:403,log,msg:'System file access attempt',tag:'attack-lfi',severity:CRITICAL"`,
		`SecRule REQUEST_URI|ARGS "@rx (?i)(/(boot|Windows|win)\.ini|/\.env|/\.git/|/\.svn/|/\.htaccess|/\.htpasswd|web\.config)" "id:930120,phase:2,deny,status:403,log,msg:'Sensitive file access attempt',tag:'attack-lfi',severity:CRITICAL"`,
	)

	// 932: Remote Code Execution (expanded from base rule 1004)
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)(;|\||\x60|&&|\$\(|\$\{)(\s*)(\b(cat|ls|id|whoami|pwd|uname|wget|curl|nc|netcat|bash|sh|cmd|powershell|python|ruby|perl|php|node|java|gcc|nmap|dig|nslookup|tftp|ftp|scp|ssh|telnet)\b)" "id:932100,phase:2,deny,status:403,log,msg:'OS command injection',tag:'attack-rce',severity:CRITICAL"`,
		// Direct shell invocation patterns
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(/bin/(ba)?sh|/usr/(local/)?bin/(python|ruby|perl|php|node)|cmd\.exe|powershell\.exe)" "id:932110,phase:2,deny,status:403,log,msg:'Shell invocation attempt',tag:'attack-rce',severity:CRITICAL"`,
	)

	// 933: PHP / Server-Side Injection
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(php://(input|filter|data|expect)|data:text/html|<\?php|\beval\s*\(|\bsystem\s*\(|\bexec\s*\(|\bpassthru\s*\(|\bshell_exec\s*\(|\bpopen\s*\(|\bproc_open\s*\()" "id:933100,phase:2,deny,status:403,log,msg:'PHP/server-side injection',tag:'attack-injection-php',severity:CRITICAL"`,
	)

	// 941: XSS (expanded from base rule 1002)
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)(<script[^>]*>|</script>|javascript\s*:|vbscript\s*:)" "id:941100,phase:2,deny,status:403,log,msg:'XSS: script injection',tag:'attack-xss',severity:CRITICAL"`,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)(on(load|error|click|mouseover|submit|focus|blur|change|input|keyup|keydown|mouseout|mouseenter|dblclick|contextmenu|abort|beforeunload|hashchange|message|offline|online|pagehide|pageshow|popstate|resize|storage|unload)\s*=)" "id:941110,phase:2,deny,status:403,log,msg:'XSS: event handler injection',tag:'attack-xss',severity:CRITICAL"`,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(<iframe|<object|<embed|<applet|<form|<input|<button|<textarea|<select|<svg[/\s>]|<math[/\s>]|<video|<audio|<source|<img[^>]+onerror)" "id:941120,phase:2,deny,status:403,log,msg:'XSS: HTML tag injection',tag:'attack-xss',severity:CRITICAL"`,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(document\.(cookie|domain|write|location|URL)|window\.(location|open|name)|\.innerHTML\s*=|\.outerHTML\s*=|\.insertAdjacentHTML|\.write\s*\(|eval\s*\(|Function\s*\(|setTimeout\s*\(|setInterval\s*\(|setImmediate\s*\()" "id:941130,phase:2,deny,status:403,log,msg:'XSS: DOM manipulation',tag:'attack-xss',severity:CRITICAL"`,
	)

	// 942: SQL Injection (expanded from base rule 1001)
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)(\b(select|insert|update|delete|drop|union|alter|create|exec|execute|truncate|grant|revoke)\b\s+.{0,40}\b(from|into|table|database|where|set|values|index|view|procedure|function|trigger)\b)" "id:942100,phase:2,deny,status:403,log,msg:'SQL injection: keyword combination',tag:'attack-sqli',severity:CRITICAL"`,
		// Tautology / always-true conditions
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)((\b(or|and)\b\s+[\d\w'\"]+\s*[=<>!]+\s*[\d\w'\"]+)|(--\s*$)|(/\*[\s\S]*?\*/))" "id:942110,phase:2,deny,status:403,log,msg:'SQL injection: tautology/comment',tag:'attack-sqli',severity:CRITICAL"`,
		// UNION-based injection
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(\bunion\b\s+(all\s+)?select\b)" "id:942120,phase:2,deny,status:403,log,msg:'SQL injection: UNION SELECT',tag:'attack-sqli',severity:CRITICAL"`,
		// Stacked queries
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(;\s*(select|insert|update|delete|drop|alter|create|exec|declare|set)\b)" "id:942130,phase:2,deny,status:403,log,msg:'SQL injection: stacked query',tag:'attack-sqli',severity:CRITICAL"`,
	)

	// 943: Session Fixation
	rules = append(rules,
		`SecRule ARGS|REQUEST_BODY "@rx (?i)((\bset-cookie\b\s*:)|(\bsessionid\b|\bphpsessid\b|\bjsessionid\b|\baspsessionid\b|\bcfid\b|\bcftoken\b)\s*=)" "id:943100,phase:2,deny,status:403,log,msg:'Session fixation attempt',tag:'attack-fixation',severity:CRITICAL"`,
	)

	if level < 2 {
		return rules
	}

	// ── Paranoia Level 2: Enhanced Detection ───────────────────────────

	// Stricter SQL function detection
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|ARGS_NAMES|REQUEST_BODY "@rx (?i)\b(concat|char|ascii|ord|substr|substring|mid|left|right|reverse|hex|unhex|conv|cast|convert|coalesce|nullif|ifnull|iif)\s*\(" "id:942200,phase:2,deny,status:403,log,msg:'PL2: SQL function detected',tag:'attack-sqli',severity:WARNING"`,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)\b(information_schema|mysql\.(user|db)|pg_catalog|sys\.(tables|columns)|sqlite_master)\b" "id:942210,phase:2,deny,status:403,log,msg:'PL2: Database metadata access',tag:'attack-sqli',severity:WARNING"`,
	)

	// Additional XSS vectors
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(<[^>]*\bstyle\s*=\s*[^>]*\b(expression|behavior|moz-binding|url)\s*\()" "id:941200,phase:2,deny,status:403,log,msg:'PL2: XSS via CSS expression',tag:'attack-xss',severity:WARNING"`,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(data:\s*(text/html|application/x?html|image/svg)[;,])" "id:941210,phase:2,deny,status:403,log,msg:'PL2: XSS via data URI',tag:'attack-xss',severity:WARNING"`,
	)

	// HTTP response splitting
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS "@rx (%0[ad]|%0[AD]|\r|\n)(Content-Type|Set-Cookie|Location)\s*:" "id:921200,phase:2,deny,status:403,log,msg:'PL2: HTTP response splitting',tag:'attack-protocol',severity:WARNING"`,
	)

	if level < 3 {
		return rules
	}

	// ── Paranoia Level 3: Strict Detection ─────────────────────────────

	// Time-based SQL injection
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)\b(sleep|benchmark|pg_sleep|waitfor\s+delay|dbms_pipe\.receive_message)\s*\(" "id:942300,phase:2,deny,status:403,log,msg:'PL3: Time-based SQL injection',tag:'attack-sqli',severity:WARNING"`,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)\b(load_file|into\s+(out|dump)file|into\s+dumpfile)\b" "id:942310,phase:2,deny,status:403,log,msg:'PL3: SQL file operation',tag:'attack-sqli',severity:WARNING"`,
	)

	// LDAP injection
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (?i)(\)|\(|\||&|\*)\s*(\([\w]+=)" "id:933300,phase:2,deny,status:403,log,msg:'PL3: LDAP injection',tag:'attack-ldap',severity:WARNING"`,
	)

	// Server-Side Template Injection (SSTI)
	rules = append(rules,
		`SecRule REQUEST_URI|ARGS|REQUEST_BODY "@rx (\{\{.*\}\}|\{%.*%\}|\$\{.*\}|#\{.*\}|<%.*%>)" "id:934300,phase:2,deny,status:403,log,msg:'PL3: Server-side template injection',tag:'attack-ssti',severity:WARNING"`,
	)

	if level < 4 {
		return rules
	}

	// ── Paranoia Level 4: Maximum Paranoia ─────────────────────────────

	// Broad special character detection in query args
	rules = append(rules,
		`SecRule ARGS "@rx ['\";\x60\\\\]" "id:942400,phase:2,deny,status:403,log,msg:'PL4: Special characters in parameters',tag:'paranoia-level/4',severity:NOTICE"`,
	)

	// Extended encoding detection
	rules = append(rules,
		`SecRule REQUEST_URI "@rx (%u[0-9a-fA-F]{4}|\\\\u[0-9a-fA-F]{4})" "id:920400,phase:1,deny,status:400,log,msg:'PL4: Unicode encoding evasion',tag:'paranoia-level/4',severity:NOTICE"`,
	)

	return rules
}
