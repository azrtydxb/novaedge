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

func getRespHeader(name string) string {
	return readHostString(getResponseHeader, name)
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

// getHostMethod reads the request method from the host.
func getHostMethod() string {
	buf := make([]byte, 16)
	bufPtr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	n := getMethod(bufPtr, uint32(len(buf)))
	if n == 0 {
		return ""
	}
	return string(buf[:n])
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

// trimSpace trims leading and trailing whitespace.
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

// hasPrefix checks if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// extractPathSegment extracts a path segment at the given index (0-based).
// For path "/api/v1/orders/123", segment 0 is "api", segment 3 is "123".
func extractPathSegment(path string, index int) string {
	seg := 0
	start := 0
	// Skip leading slash
	if len(path) > 0 && path[0] == '/' {
		start = 1
	}
	for i := start; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' || path[i] == '?' {
			if seg == index {
				return path[start:i]
			}
			seg++
			start = i + 1
			if i == len(path) || path[i] == '?' {
				break
			}
		}
	}
	return ""
}

// sanitizeHeaderValue replaces characters that are not safe for header values.
func sanitizeHeaderValue(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c < 127 {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

//export on_request_headers
func onRequestHeaders() {
	metricPrefix := getConfig("metric_prefix")
	if metricPrefix == "" {
		metricPrefix = "business"
	}

	extractHeaders := splitComma(getConfig("extract_headers"))
	pathPattern := getConfig("path_pattern")

	method := getHostMethod()
	path := getHostPath()

	// Set the method as a metric label
	setReqHeader("X-Business-Metric-Method", method)

	// Check path pattern match
	pathMatched := "false"
	if pathPattern == "" || hasPrefix(path, pathPattern) {
		pathMatched = "true"
	}
	setReqHeader("X-Business-Metric-Path-Match", pathMatched)

	// Extract path-based labels: first two segments as resource and action
	resource := extractPathSegment(path, 1) // e.g., "orders" from /api/orders
	if resource != "" {
		setReqHeader("X-Business-Metric-Resource", sanitizeHeaderValue(resource))
	}

	action := extractPathSegment(path, 2) // e.g., "123" or action name
	if action != "" {
		setReqHeader("X-Business-Metric-Action", sanitizeHeaderValue(action))
	}

	// Extract configured request headers as metric labels
	extractedCount := 0
	for _, hdr := range extractHeaders {
		value := getReqHeader(hdr)
		if value != "" {
			labelName := "X-Business-Metric-Label-" + sanitizeHeaderValue(hdr)
			setReqHeader(labelName, sanitizeHeaderValue(value))
			extractedCount++
		}
	}

	// Set metric prefix for downstream collectors
	setReqHeader("X-Business-Metric-Prefix", metricPrefix)

	// Log structured metric event for request phase
	logInfo("businessmetrics: request event=" + metricPrefix + "_request" +
		" method=" + method +
		" path=" + path +
		" resource=" + resource +
		" path_matched=" + pathMatched)
}

//export on_response_headers
func onResponseHeaders() {
	metricPrefix := getConfig("metric_prefix")
	if metricPrefix == "" {
		metricPrefix = "business"
	}

	// Read the status category from X-Status-Code header (set by host, or we
	// categorize from the status header if available).
	statusCode := getRespHeader("X-Response-Status")
	if statusCode == "" {
		statusCode = getRespHeader(":status")
	}

	// Categorize status codes into buckets for metric labels
	var statusCategory string
	if len(statusCode) >= 1 {
		switch statusCode[0] {
		case '2':
			statusCategory = "success"
		case '3':
			statusCategory = "redirect"
		case '4':
			statusCategory = "client_error"
		case '5':
			statusCategory = "server_error"
		default:
			statusCategory = "unknown"
		}
	} else {
		statusCategory = "unknown"
	}

	// Set response metric headers for the metrics collector
	setRespHeader("X-Business-Metric-Status", statusCode)
	setRespHeader("X-Business-Metric-Status-Category", statusCategory)
	setRespHeader("X-Business-Metric-Prefix", metricPrefix)

	// Propagate request-phase labels into the response for unified collection
	resource := getReqHeader("X-Business-Metric-Resource")
	if resource != "" {
		setRespHeader("X-Business-Metric-Resource", resource)
	}

	method := getReqHeader("X-Business-Metric-Method")
	if method != "" {
		setRespHeader("X-Business-Metric-Method", method)
	}

	pathMatch := getReqHeader("X-Business-Metric-Path-Match")
	if pathMatch != "" {
		setRespHeader("X-Business-Metric-Path-Match", pathMatch)
	}

	// Log structured metric event for response phase
	logInfo("businessmetrics: response event=" + metricPrefix + "_response" +
		" status=" + statusCode +
		" category=" + statusCategory +
		" resource=" + resource)
}

func main() {}
