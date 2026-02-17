//go:build tinygo.wasm

package main

import "unsafe"

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
func sendResponse(statusCode, bodyPtr, bodyLen uint32)

//export malloc
func wasmMalloc(size uint32) uint32 {
	buf := make([]byte, size)
	if len(buf) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//export free
func wasmFree(_ uint32) {}

func ptrLen(s string) (uint32, uint32) {
	if len(s) == 0 {
		return 0, 0
	}
	b := []byte(s)
	return uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b))
}

func readHostString(getter func(uint32, uint32, uint32, uint32) uint32, key string) string {
	keyPtr, keyLen := ptrLen(key)
	buf := make([]byte, 4096)
	bufPtr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	n := getter(keyPtr, keyLen, bufPtr, uint32(len(buf)))
	if n == 0 {
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

func getConfig(key string) string {
	return readHostString(getConfigValue, key)
}

func getReqHeader(name string) string {
	return readHostString(getRequestHeader, name)
}

func setReqHeader(name, value string) {
	np, nl := ptrLen(name)
	vp, vl := ptrLen(value)
	setRequestHeader(np, nl, vp, vl)
}

func setRespHeader(name, value string) {
	np, nl := ptrLen(name)
	vp, vl := ptrLen(value)
	setResponseHeader(np, nl, vp, vl)
}

func rejectRequest(status uint32, body string) {
	bp, bl := ptrLen(body)
	sendResponse(status, bp, bl)
}

// getHostPath reads the request path from the host.
func getHostPath() string {
	buf := make([]byte, 4096)
	bufPtr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	n := getPath(bufPtr, uint32(len(buf)))
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

// atoi parses a decimal string into an integer; returns defaultVal on failure.
func atoi(s string, defaultVal int) int {
	if len(s) == 0 {
		return defaultVal
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return defaultVal
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// itoa converts a non-negative integer to a string.
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

// splitComma splits a string by commas, trimming spaces from each element.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := trimSpace(s[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	return parts
}

// trimSpace trims leading and trailing whitespace from a string.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// contains checks if haystack contains needle (case-sensitive).
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// containsLower checks if haystack contains needle case-insensitively.
func containsLower(haystack, needle string) bool {
	return contains(toLower(haystack), toLower(needle))
}

// toLower converts ASCII uppercase to lowercase.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b[i] = s[i] + 32
		} else {
			b[i] = s[i]
		}
	}
	return string(b)
}

// hasPrefix checks if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// countNestingDepth estimates query nesting depth by counting opening braces.
func countNestingDepth(query string) int {
	maxDepth := 0
	depth := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '{' {
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		} else if query[i] == '}' {
			depth--
		}
	}
	return maxDepth
}

// countAliases estimates the number of aliases in a GraphQL query by counting
// occurrences of the pattern "identifier:" followed by a field name.
func countAliases(query string) int {
	count := 0
	i := 0
	for i < len(query) {
		// Skip whitespace
		for i < len(query) && (query[i] == ' ' || query[i] == '\t' || query[i] == '\n' || query[i] == '\r') {
			i++
		}
		if i >= len(query) {
			break
		}
		// Check for identifier followed by colon (alias pattern)
		if isAlphaOrUnderscore(query[i]) {
			start := i
			for i < len(query) && isAlphaNumOrUnderscore(query[i]) {
				i++
			}
			// Skip whitespace between identifier and colon
			j := i
			for j < len(query) && (query[j] == ' ' || query[j] == '\t') {
				j++
			}
			if j < len(query) && query[j] == ':' {
				// Check it is not a variable definition (argument pattern)
				// Aliases have format: aliasName: fieldName
				k := j + 1
				for k < len(query) && (query[k] == ' ' || query[k] == '\t') {
					k++
				}
				if k < len(query) && isAlphaOrUnderscore(query[k]) {
					count++
				}
			}
			_ = start
		} else {
			i++
		}
	}
	return count
}

func isAlphaOrUnderscore(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isAlphaNumOrUnderscore(b byte) bool {
	return isAlphaOrUnderscore(b) || (b >= '0' && b <= '9')
}

// isGraphQLRequest returns true if the request appears to be a GraphQL request
// based on Content-Type header or request path.
func isGraphQLRequest() bool {
	ct := getReqHeader("Content-Type")
	if containsLower(ct, "application/graphql") {
		return true
	}
	path := getHostPath()
	if hasPrefix(path, "/graphql") {
		return true
	}
	return false
}

//export on_request_headers
func onRequestHeaders() {
	if !isGraphQLRequest() {
		return
	}

	maxDepth := atoi(getConfig("max_depth"), 10)
	maxAliases := atoi(getConfig("max_aliases"), 5)
	blockedFieldsCfg := getConfig("blocked_fields")
	blockedFields := splitComma(blockedFieldsCfg)
	allowIntrospection := getConfig("introspection") == "true"

	// Read GraphQL query from X-GraphQL-Query header (set by prior middleware
	// that extracts the query from the request body for header-only plugins).
	query := getReqHeader("X-GraphQL-Query")

	if query != "" {
		// Check nesting depth
		depth := countNestingDepth(query)
		if depth > maxDepth {
			logWarn("graphqlprotect: query depth " + itoa(depth) + " exceeds max " + itoa(maxDepth))
			rejectRequest(400, `{"errors":[{"message":"query depth `+itoa(depth)+` exceeds maximum allowed depth of `+itoa(maxDepth)+`"}]}`)
			return
		}

		// Check alias count
		aliases := countAliases(query)
		if aliases > maxAliases {
			logWarn("graphqlprotect: alias count " + itoa(aliases) + " exceeds max " + itoa(maxAliases))
			rejectRequest(400, `{"errors":[{"message":"query contains `+itoa(aliases)+` aliases, maximum allowed is `+itoa(maxAliases)+`"}]}`)
			return
		}

		// Block introspection queries unless explicitly allowed
		if !allowIntrospection {
			if contains(query, "__schema") || contains(query, "__type") {
				logWarn("graphqlprotect: introspection query blocked")
				rejectRequest(400, `{"errors":[{"message":"introspection queries are not allowed"}]}`)
				return
			}
		}

		// Check for blocked fields
		for _, field := range blockedFields {
			if contains(query, field) {
				logWarn("graphqlprotect: blocked field detected: " + field)
				rejectRequest(400, `{"errors":[{"message":"access to field '`+field+`' is not allowed"}]}`)
				return
			}
		}
	}

	// Also check the path for introspection via GET requests with query params
	path := getHostPath()
	if !allowIntrospection {
		if contains(path, "__schema") || contains(path, "__type") {
			logWarn("graphqlprotect: introspection query blocked via path")
			rejectRequest(400, `{"errors":[{"message":"introspection queries are not allowed"}]}`)
			return
		}
	}

	// Mark request as validated
	setReqHeader("X-GraphQL-Validated", "true")
	logInfo("graphqlprotect: request validated")
}

//export on_response_headers
func onResponseHeaders() {}

func main() {}
