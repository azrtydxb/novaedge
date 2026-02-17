//go:build tinygo.wasm

// Package main implements a surrogate-key cache invalidation WASM plugin.
// On requests it handles PURGE methods by validating source IPs and extracting
// surrogate keys. On responses it propagates surrogate keys and cache tags
// from the backend for the cache layer to consume.
package main

import "unsafe"

// ---------------------------------------------------------------------------
// Host-function imports (module "novaedge")
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
func sendResponse(statusCode, bodyPtr, bodyLen uint32)

// ---------------------------------------------------------------------------
// Exported ABI functions
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Helper utilities
// ---------------------------------------------------------------------------

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

func getRespHeader(name string) string {
	return readHostString(getResponseHeader, name)
}

func rejectRequest(status uint32, body string) {
	bp, bl := ptrLen(body)
	sendResponse(status, bp, bl)
}

func readMethod() string {
	buf := make([]byte, 16)
	bufPtr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	n := getMethod(bufPtr, uint32(len(buf)))
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

func readPath() string {
	buf := make([]byte, 4096)
	bufPtr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	n := getPath(bufPtr, uint32(len(buf)))
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

// ---------------------------------------------------------------------------
// Plugin helpers
// ---------------------------------------------------------------------------

// splitCSV splits a comma-separated string into trimmed tokens.
func splitCSV(s string) []string {
	if len(s) == 0 {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			token := trimSpace(s[start:i])
			if len(token) > 0 {
				result = append(result, token)
			}
			start = i + 1
		}
	}
	return result
}

// trimSpace trims leading and trailing spaces and tabs.
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

// contains checks whether a slice contains a given string.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// splitSpaces splits a space-separated string into tokens.
func splitSpaces(s string) []string {
	if len(s) == 0 {
		return nil
	}
	var result []string
	start := -1
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' || s[i] == '\t' {
			if start >= 0 {
				result = append(result, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Request phase: handle PURGE methods and extract surrogate keys
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	purgeMethodsStr := getConfig("purge_methods")
	if purgeMethodsStr == "" {
		purgeMethodsStr = "PURGE"
	}
	purgeMethods := splitCSV(purgeMethodsStr)

	surrogateHeader := getConfig("surrogate_header")
	if surrogateHeader == "" {
		surrogateHeader = "Surrogate-Key"
	}

	allowedPurgeIPsStr := getConfig("allowed_purge_ips")
	if allowedPurgeIPsStr == "" {
		allowedPurgeIPsStr = "127.0.0.1"
	}
	allowedPurgeIPs := splitCSV(allowedPurgeIPsStr)

	method := readMethod()

	// Check if this is a purge request.
	isPurge := false
	for _, m := range purgeMethods {
		if m == method {
			isPurge = true
			break
		}
	}

	if !isPurge {
		// Not a purge request; pass through. Tag with surrogate keys if present
		// in the request for cache-aware routing.
		keys := getReqHeader(surrogateHeader)
		if keys != "" {
			setReqHeader("X-Cache-Surrogate-Keys", keys)
			logInfo("cacheinvalidation: forwarding surrogate keys: " + keys)
		}
		return
	}

	// Validate source IP for purge requests.
	clientIP := getReqHeader("X-Real-IP")
	if clientIP == "" {
		clientIP = getReqHeader("X-Forwarded-For")
		// Take just the first IP from X-Forwarded-For.
		for i := 0; i < len(clientIP); i++ {
			if clientIP[i] == ',' {
				clientIP = trimSpace(clientIP[:i])
				break
			}
		}
	}

	if !contains(allowedPurgeIPs, clientIP) {
		logWarn("cacheinvalidation: purge rejected from IP " + clientIP)
		rejectRequest(403, `{"error":"purge not allowed from this IP"}`)
		return
	}

	// Extract surrogate keys from the request.
	keys := getReqHeader(surrogateHeader)
	if keys == "" {
		logWarn("cacheinvalidation: PURGE request without surrogate keys")
		rejectRequest(400, `{"error":"missing surrogate keys for purge"}`)
		return
	}

	// Set the purge instruction header for the cache layer.
	setReqHeader("X-Cache-Purge", keys)
	setReqHeader("X-Cache-Purge-Method", method)

	path := readPath()
	logInfo("cacheinvalidation: purge request from " + clientIP + " path=" + path + " keys=" + keys)
}

// ---------------------------------------------------------------------------
// Response phase: propagate surrogate keys and set cache tags
// ---------------------------------------------------------------------------

//export on_response_headers
func onResponseHeaders() {
	surrogateHeader := getConfig("surrogate_header")
	if surrogateHeader == "" {
		surrogateHeader = "Surrogate-Key"
	}

	cacheTagsHeader := getConfig("cache_tags_header")
	if cacheTagsHeader == "" {
		cacheTagsHeader = "Cache-Tag"
	}

	// Read surrogate keys from the backend response.
	backendKeys := getRespHeader(surrogateHeader)
	if backendKeys == "" {
		return
	}

	// Propagate surrogate keys to the response for downstream caches.
	setRespHeader(surrogateHeader, backendKeys)

	// Convert space-separated surrogate keys to comma-separated cache tags.
	keysList := splitSpaces(backendKeys)
	if len(keysList) > 0 {
		tags := keysList[0]
		for i := 1; i < len(keysList); i++ {
			tags = tags + "," + keysList[i]
		}
		setRespHeader("X-Cache-Tags", tags)
		setRespHeader(cacheTagsHeader, tags)
	}

	logInfo("cacheinvalidation: propagated surrogate keys: " + backendKeys)
}

func main() {}
