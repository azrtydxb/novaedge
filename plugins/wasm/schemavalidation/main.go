//go:build tinygo.wasm

// Package main implements a request schema validation WASM plugin for NovaEdge.
// It validates incoming requests against configured rules for required headers,
// header value patterns, and content type enforcement.
//
// Configuration keys:
//   - "required_headers"  → comma-separated list of headers that must be present
//   - "header_pattern_N"  → "header_name:pattern" (N=0,1,2,...) simple wildcard matching
//   - "content_type"      → required Content-Type value (prefix match)
//   - "max_content_length"→ maximum Content-Length in bytes (0 = no limit)
//   - "allowed_methods"   → comma-separated list of allowed HTTP methods
//   - "reject_status"     → HTTP status for rejected requests (default "400")
//   - "reject_body"       → response body for rejected requests
//   - "max_patterns"      → max number of header_pattern entries to scan (default "32")
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

//go:wasmimport novaedge get_method
func getMethod(bufPtr, bufCap uint32) uint32

//go:wasmimport novaedge get_path
func getPath(bufPtr, bufCap uint32) uint32

//go:wasmimport novaedge get_config_value
func getConfigValue(keyPtr, keyLen, valPtr, valCap uint32) uint32

//go:wasmimport novaedge log_message
func logMessage(level, msgPtr, msgLen uint32)

//go:wasmimport novaedge send_response
func sendResponse(statusCode uint32, bodyPtr, bodyLen uint32)

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

func readHeader(name string) string {
	nameB := []byte(name)
	if len(nameB) == 0 {
		return ""
	}
	buf := make([]byte, 8192)
	n := getRequestHeader(
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

func readMethod() string {
	buf := make([]byte, 16)
	n := getMethod(
		uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)),
	)
	if n == 0 || n > uint32(len(buf)) {
		return ""
	}
	return string(buf[:n])
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

func logWarn(msg string) {
	p, l := ptrLen(msg)
	logMessage(2, p, l)
}

func logError(msg string) {
	p, l := ptrLen(msg)
	logMessage(3, p, l)
}

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

func rejectRequest(status int, body string) {
	writeRespHeader("Content-Type", "application/json")
	bp, bl := ptrLen(body)
	sendResponse(uint32(status), bp, bl)
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

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func toLower(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

// splitCSV splits a comma-separated string into trimmed tokens.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			token := trimSpaces(s[start:i])
			if token != "" {
				result = append(result, token)
			}
			start = i + 1
		}
	}
	return result
}

// wildcardMatch performs simple wildcard pattern matching supporting '*' (match
// any sequence) and '?' (match single character).
func wildcardMatch(s, pattern string) bool {
	si := 0
	pi := 0
	starSI := -1
	starPI := -1

	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			si++
			pi++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			starPI = pi
			starSI = si
			pi++
		} else if starPI >= 0 {
			pi = starPI + 1
			starSI++
			si = starSI
		} else {
			return false
		}
	}

	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}

	return pi == len(pattern)
}

// ---------------------------------------------------------------------------
// Validation logic
// ---------------------------------------------------------------------------

// validationError collects validation failure messages.
type validationError struct {
	errors []string
}

func (ve *validationError) add(msg string) {
	ve.errors = append(ve.errors, msg)
}

func (ve *validationError) hasErrors() bool {
	return len(ve.errors) > 0
}

func (ve *validationError) toJSON() string {
	if len(ve.errors) == 0 {
		return `{"errors":[]}`
	}
	result := `{"errors":[`
	for i, e := range ve.errors {
		if i > 0 {
			result += ","
		}
		result += `"` + escapeJSON(e) + `"`
	}
	result += "]}"
	return result
}

func escapeJSON(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

func validateRequiredHeaders(ve *validationError) {
	required := configValue("required_headers")
	if required == "" {
		return
	}
	headers := splitCSV(required)
	for _, h := range headers {
		val := readHeader(h)
		if val == "" {
			ve.add("missing required header: " + h)
		}
	}
}

func validateHeaderPatterns(ve *validationError) {
	maxPatterns := atoi(configValue("max_patterns"), 32)

	for i := 0; i < maxPatterns; i++ {
		rule := configValue("header_pattern_" + itoa(i))
		if rule == "" {
			break
		}
		// Parse "header_name:pattern".
		colonIdx := -1
		for j := 0; j < len(rule); j++ {
			if rule[j] == ':' {
				colonIdx = j
				break
			}
		}
		if colonIdx < 0 {
			logWarn("schemavalidation: invalid header_pattern_" + itoa(i) + ": missing colon separator")
			continue
		}
		headerName := trimSpaces(rule[:colonIdx])
		pattern := trimSpaces(rule[colonIdx+1:])

		val := readHeader(headerName)
		if val == "" {
			ve.add("header " + headerName + " is empty but must match pattern: " + pattern)
			continue
		}
		if !wildcardMatch(val, pattern) {
			ve.add("header " + headerName + " value does not match pattern: " + pattern)
		}
	}
}

func validateContentType(ve *validationError) {
	required := configValue("content_type")
	if required == "" {
		return
	}
	ct := readHeader("Content-Type")
	if ct == "" {
		ve.add("missing required Content-Type header, expected: " + required)
		return
	}
	if !hasPrefix(toLower(ct), toLower(required)) {
		ve.add("Content-Type mismatch: got " + ct + ", expected: " + required)
	}
}

func validateContentLength(ve *validationError) {
	maxLen := configValue("max_content_length")
	if maxLen == "" {
		return
	}
	maxBytes := atoi(maxLen, 0)
	if maxBytes <= 0 {
		return
	}
	cl := readHeader("Content-Length")
	if cl == "" {
		return
	}
	actualLen := atoi(cl, 0)
	if actualLen > maxBytes {
		ve.add("Content-Length " + cl + " exceeds maximum " + maxLen)
	}
}

func validateAllowedMethods(ve *validationError) {
	allowed := configValue("allowed_methods")
	if allowed == "" {
		return
	}
	method := readMethod()
	if method == "" {
		return
	}
	methods := splitCSV(allowed)
	for _, m := range methods {
		if toLower(m) == toLower(method) {
			return
		}
	}
	ve.add("method " + method + " is not allowed")
}

// ---------------------------------------------------------------------------
// Exported plugin hooks
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	ve := &validationError{}

	validateAllowedMethods(ve)
	validateRequiredHeaders(ve)
	validateHeaderPatterns(ve)
	validateContentType(ve)
	validateContentLength(ve)

	if !ve.hasErrors() {
		logInfo("schemavalidation: request passed all validation rules")
		return
	}

	rejectStatus := atoi(configValue("reject_status"), 400)
	rejectBody := configValue("reject_body")
	if rejectBody == "" {
		rejectBody = ve.toJSON()
	}

	for _, e := range ve.errors {
		logWarn("schemavalidation: " + e)
	}
	logInfo("schemavalidation: rejecting request with " + itoa(len(ve.errors)) + " validation errors")
	rejectRequest(rejectStatus, rejectBody)
}

//export on_response_headers
func onResponseHeaders() {
	// No response-phase processing needed for request validation.
}

func main() {}
