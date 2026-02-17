//go:build tinygo.wasm

// Package main implements a response body rewrite WASM plugin for NovaEdge.
// Since the WASM ABI only exposes header manipulation (not body access), this
// plugin reads match/replace rules from configuration and encodes them into an
// X-Body-Rewrite-Rules response header. The host middleware interprets that
// header to perform the actual body rewriting.
//
// Configuration keys:
//   - "match_0", "match_1", ... → substring to find in the response body
//   - "replace_0", "replace_1", ... → replacement string for the corresponding match
//   - "content_types" → comma-separated content types to apply rewrites (default "text/html")
//   - "max_rules" → maximum number of rules to scan (default "16")
package main

import "unsafe"

// ---------------------------------------------------------------------------
// Host function imports (NovaEdge WASM ABI)
// ---------------------------------------------------------------------------

//go:wasmimport novaedge get_request_header
func getRequestHeader(namePtr, nameLen, valPtr, valCap uint32) uint32

//go:wasmimport novaedge set_request_header
func setRequestHeader(namePtr, nameLen, valPtr, valLen uint32)

//go:wasmimport novaedge get_response_header
func getResponseHeader(namePtr, nameLen, valPtr, valCap uint32) uint32

//go:wasmimport novaedge set_response_header
func setResponseHeader(namePtr, nameLen, valPtr, valLen uint32)

//go:wasmimport novaedge get_config_value
func getConfigValue(keyPtr, keyLen, valPtr, valCap uint32) uint32

//go:wasmimport novaedge log_message
func logMessage(level, msgPtr, msgLen uint32)

// ---------------------------------------------------------------------------
// Memory management exports
// ---------------------------------------------------------------------------

//export malloc
func wasmMalloc(size uint32) uint32 {
	buf := make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//export free
func wasmFree(_ uint32) {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ptrLen(s string) (uint32, uint32) {
	b := []byte(s)
	if len(b) == 0 {
		return 0, 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b))
}

func readRespHeader(name string) string {
	nameB := []byte(name)
	if len(nameB) == 0 {
		return ""
	}
	buf := make([]byte, 4096)
	n := getResponseHeader(
		uint32(uintptr(unsafe.Pointer(&nameB[0]))), uint32(len(nameB)),
		uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)),
	)
	if n == 0 || n > uint32(len(buf)) {
		return ""
	}
	return string(buf[:n])
}

func writeRespHeader(name, value string) {
	np, nl := ptrLen(name)
	vp, vl := ptrLen(value)
	setResponseHeader(np, nl, vp, vl)
}

func configValue(key string) string {
	keyB := []byte(key)
	if len(keyB) == 0 {
		return ""
	}
	buf := make([]byte, 4096)
	n := getConfigValue(
		uint32(uintptr(unsafe.Pointer(&keyB[0]))), uint32(len(keyB)),
		uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)),
	)
	if n == 0 || n > uint32(len(buf)) {
		return ""
	}
	return string(buf[:n])
}

func logInfo(msg string) {
	p, l := ptrLen(msg)
	logMessage(1, p, l)
}

func logDebug(msg string) {
	p, l := ptrLen(msg)
	logMessage(0, p, l)
}

func logWarn(msg string) {
	p, l := ptrLen(msg)
	logMessage(2, p, l)
}

// itoa converts a non-negative integer to its decimal string representation.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 20)
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// atoi converts a decimal string to an integer, returning the default on error.
func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	result := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return def
		}
		result = result*10 + int(c-'0')
	}
	return result
}

// escapeRuleValue escapes pipe and backslash characters so the encoded header
// value can be parsed unambiguously by the host middleware.
func escapeRuleValue(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '|':
			out = append(out, '\\', '|')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Exported plugin hooks
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	// Nothing to do during the request phase for body rewriting.
}

//export on_response_headers
func onResponseHeaders() {
	// Check content type matches configured types.
	contentTypes := configValue("content_types")
	if contentTypes == "" {
		contentTypes = "text/html"
	}

	ct := readRespHeader("Content-Type")
	if ct != "" && !containsAny(ct, contentTypes) {
		logDebug("bodyrewrite: content-type " + ct + " not in allowed list, skipping")
		return
	}

	maxRules := atoi(configValue("max_rules"), 16)

	// Build a pipe-delimited rules header: match1|replace1|match2|replace2|...
	var rules string
	ruleCount := 0
	for i := 0; i < maxRules; i++ {
		match := configValue("match_" + itoa(i))
		if match == "" {
			break
		}
		replace := configValue("replace_" + itoa(i))
		if rules != "" {
			rules += "|"
		}
		rules += escapeRuleValue(match) + "|" + escapeRuleValue(replace)
		ruleCount++
		logInfo("bodyrewrite: rule " + itoa(i) + ": " + match + " -> " + replace)
	}

	if ruleCount == 0 {
		logDebug("bodyrewrite: no rewrite rules configured")
		return
	}

	writeRespHeader("X-Body-Rewrite-Rules", rules)
	logInfo("bodyrewrite: set " + itoa(ruleCount) + " rewrite rules on response")
}

// containsAny checks whether any comma-separated token in list is a prefix of s.
func containsAny(s, list string) bool {
	start := 0
	for i := 0; i <= len(list); i++ {
		if i == len(list) || list[i] == ',' {
			token := trimSpaces(list[start:i])
			if token != "" && hasPrefix(s, token) {
				return true
			}
			start = i + 1
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func trimSpaces(s string) string {
	start := 0
	for start < len(s) && s[start] == ' ' {
		start++
	}
	end := len(s)
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

func main() {}
