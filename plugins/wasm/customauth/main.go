//go:build tinygo.wasm

// Package main implements a custom authentication WASM plugin for NovaEdge.
// It supports two modes: API key validation and HMAC signature verification.
//
// Configuration keys:
//   - "mode"           → "apikey" or "hmac" (default "apikey")
//   - "api_keys"       → comma-separated list of valid API keys
//   - "key_header"     → header containing the API key (default "X-API-Key")
//   - "key_query_param"→ query parameter containing the API key (default "api_key")
//   - "hmac_header"    → header containing the HMAC signature (default "X-HMAC-Signature")
//   - "hmac_secret"    → shared secret for HMAC verification
//   - "reject_status"  → HTTP status code for rejected requests (default "401")
//   - "reject_body"    → response body for rejected requests (default '{"error":"unauthorized"}')
//   - "realm"          → authentication realm for WWW-Authenticate header (default "novaedge")
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

//go:wasmimport novaedge get_path
func getPath(bufPtr, bufCap uint32) uint32

//go:wasmimport novaedge get_method
func getMethod(bufPtr, bufCap uint32) uint32

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
	buf := make([]byte, 4096)
	n := getRequestHeader(
		uint32(uintptr(unsafe.Pointer(&nameB[0]))), uint32(len(nameB)),
		uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)),
	)
	if n == 0 || n > uint32(len(buf)) {
		return ""
	}
	return string(buf[:n])
}

func writeReqHeader(name, value string) {
	np, nl := ptrLen(name)
	vp, vl := ptrLen(value)
	setRequestHeader(np, nl, vp, vl)
}

func writeRespHeader(name, value string) {
	np, nl := ptrLen(name)
	vp, vl := ptrLen(value)
	setResponseHeader(np, nl, vp, vl)
}

func readPath() string {
	buf := make([]byte, 8192)
	n := getPath(
		uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)),
	)
	if n == 0 || n > uint32(len(buf)) {
		return ""
	}
	return string(buf[:n])
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

func reject(status int, body string) {
	realm := configValue("realm")
	if realm == "" {
		realm = "novaedge"
	}
	writeRespHeader("WWW-Authenticate", "Bearer realm=\""+realm+"\"")
	writeRespHeader("Content-Type", "application/json")
	bp, bl := ptrLen(body)
	sendResponse(uint32(status), bp, bl)
}

// ---------------------------------------------------------------------------
// API key authentication
// ---------------------------------------------------------------------------

// extractAPIKeyFromQuery parses the query string from the path to find the
// specified parameter value.
func extractAPIKeyFromQuery(path, paramName string) string {
	// Find '?' to start of query string.
	qIdx := -1
	for i := 0; i < len(path); i++ {
		if path[i] == '?' {
			qIdx = i
			break
		}
	}
	if qIdx < 0 {
		return ""
	}
	query := path[qIdx+1:]

	// Parse key=value pairs separated by '&'.
	start := 0
	for i := 0; i <= len(query); i++ {
		if i == len(query) || query[i] == '&' {
			pair := query[start:i]
			eqIdx := -1
			for j := 0; j < len(pair); j++ {
				if pair[j] == '=' {
					eqIdx = j
					break
				}
			}
			if eqIdx > 0 && pair[:eqIdx] == paramName {
				return pair[eqIdx+1:]
			}
			start = i + 1
		}
	}
	return ""
}

func validateAPIKey() bool {
	keyHeader := configValue("key_header")
	if keyHeader == "" {
		keyHeader = "X-API-Key"
	}
	queryParam := configValue("key_query_param")
	if queryParam == "" {
		queryParam = "api_key"
	}

	// Try header first, then query parameter.
	key := readHeader(keyHeader)
	if key == "" {
		path := readPath()
		key = extractAPIKeyFromQuery(path, queryParam)
	}
	if key == "" {
		logWarn("customauth: no API key provided")
		return false
	}

	validKeys := configValue("api_keys")
	if validKeys == "" {
		logError("customauth: no api_keys configured, rejecting all requests")
		return false
	}

	// Check if the provided key matches any configured key.
	start := 0
	for i := 0; i <= len(validKeys); i++ {
		if i == len(validKeys) || validKeys[i] == ',' {
			candidate := trimSpaces(validKeys[start:i])
			if candidate != "" && constantTimeEqual(key, candidate) {
				return true
			}
			start = i + 1
		}
	}

	logWarn("customauth: invalid API key provided")
	return false
}

// constantTimeEqual performs a constant-time comparison of two strings to
// prevent timing side-channel attacks.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// ---------------------------------------------------------------------------
// HMAC authentication
// ---------------------------------------------------------------------------

func validateHMAC() bool {
	hmacHeader := configValue("hmac_header")
	if hmacHeader == "" {
		hmacHeader = "X-HMAC-Signature"
	}

	signature := readHeader(hmacHeader)
	if signature == "" {
		logWarn("customauth: no HMAC signature provided in " + hmacHeader)
		return false
	}

	secret := configValue("hmac_secret")
	if secret == "" {
		logError("customauth: no hmac_secret configured, rejecting all requests")
		return false
	}

	// Build the signing payload from method + path.
	method := readMethod()
	path := readPath()
	payload := method + ":" + path

	// Compute HMAC-like hash. Since TinyGo WASM has no crypto libraries
	// available, we use a simple keyed hash (FNV-1a with key mixing).
	// In production, the host should provide a crypto host function.
	expected := computeKeyedHash(payload, secret)

	if !constantTimeEqual(signature, expected) {
		logWarn("customauth: HMAC signature mismatch")
		return false
	}

	return true
}

// computeKeyedHash produces a hex-encoded keyed hash of the data using the
// secret. It uses FNV-1a with key material mixed in as a simple keyed hash
// suitable for WASM environments without crypto primitives.
func computeKeyedHash(data, key string) string {
	// FNV-1a 64-bit offset basis and prime.
	const offset64 uint64 = 14695981039346656037
	const prime64 uint64 = 1099511628211

	h := offset64

	// Mix in the key first.
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= prime64
	}
	// Separator.
	h ^= 0xff
	h *= prime64
	// Mix in the data.
	for i := 0; i < len(data); i++ {
		h ^= uint64(data[i])
		h *= prime64
	}

	return toHex(h)
}

const hexChars = "0123456789abcdef"

func toHex(v uint64) string {
	buf := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		buf[i] = hexChars[v&0xf]
		v >>= 4
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Exported plugin hooks
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	mode := configValue("mode")
	if mode == "" {
		mode = "apikey"
	}

	rejectStatus := atoi(configValue("reject_status"), 401)
	rejectBody := configValue("reject_body")
	if rejectBody == "" {
		rejectBody = `{"error":"unauthorized"}`
	}

	var valid bool
	switch mode {
	case "apikey":
		valid = validateAPIKey()
	case "hmac":
		valid = validateHMAC()
	default:
		logError("customauth: unknown mode: " + mode)
		reject(500, `{"error":"invalid auth plugin configuration"}`)
		return
	}

	if !valid {
		logInfo("customauth: rejecting request with status " + configValue("reject_status"))
		reject(rejectStatus, rejectBody)
		return
	}

	// Mark request as authenticated for downstream middleware.
	writeReqHeader("X-Auth-Status", "authenticated")
	writeReqHeader("X-Auth-Method", mode)
	logInfo("customauth: request authenticated via " + mode)
}

//export on_response_headers
func onResponseHeaders() {
	// No response-phase processing needed for authentication.
}

func main() {}
