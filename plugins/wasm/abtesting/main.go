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

// contains checks if haystack contains needle.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// fnv32a computes a simple FNV-1a hash of a string, returning a uint32.
func fnv32a(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// extractCookieValue extracts a specific cookie value from the Cookie header.
func extractCookieValue(cookieHeader, name string) string {
	if cookieHeader == "" || name == "" {
		return ""
	}
	// Cookie header format: "name1=value1; name2=value2"
	search := name + "="
	i := 0
	for i < len(cookieHeader) {
		// Skip whitespace and semicolons
		for i < len(cookieHeader) && (cookieHeader[i] == ' ' || cookieHeader[i] == ';') {
			i++
		}
		if i >= len(cookieHeader) {
			break
		}
		// Check if this position starts with our cookie name
		if i+len(search) <= len(cookieHeader) && cookieHeader[i:i+len(search)] == search {
			// Found it; extract value until semicolon or end
			valStart := i + len(search)
			valEnd := valStart
			for valEnd < len(cookieHeader) && cookieHeader[valEnd] != ';' {
				valEnd++
			}
			return cookieHeader[valStart:valEnd]
		}
		// Skip to next cookie
		for i < len(cookieHeader) && cookieHeader[i] != ';' {
			i++
		}
	}
	return ""
}

//export on_request_headers
func onRequestHeaders() {
	experimentName := getConfig("experiment_name")
	if experimentName == "" {
		experimentName = "default"
	}

	variantsCfg := getConfig("variants")
	variants := splitComma(variantsCfg)
	if len(variants) == 0 {
		variants = []string{"control", "variant_a"}
	}

	cookieName := getConfig("cookie_name")
	if cookieName == "" {
		cookieName = "ab_bucket"
	}

	headerName := getConfig("header_name")
	if headerName == "" {
		headerName = "X-AB-Variant"
	}

	trafficSplit := atoi(getConfig("traffic_split"), 50)
	if trafficSplit < 0 {
		trafficSplit = 0
	}
	if trafficSplit > 100 {
		trafficSplit = 100
	}

	// Check if user already has an assignment via cookie
	cookieHeader := getReqHeader("Cookie")
	existingBucket := extractCookieValue(cookieHeader, cookieName)

	// Also check if an upstream already set the variant header
	existingHeader := getReqHeader(headerName)

	var assignedVariant string

	if existingBucket != "" {
		// Validate that the existing bucket is one of the configured variants
		valid := false
		for _, v := range variants {
			if existingBucket == v {
				valid = true
				break
			}
		}
		if valid {
			assignedVariant = existingBucket
		}
	}

	if assignedVariant == "" && existingHeader != "" {
		for _, v := range variants {
			if existingHeader == v {
				assignedVariant = existingHeader
				break
			}
		}
	}

	// If no existing assignment, compute deterministically from request headers
	if assignedVariant == "" {
		userAgent := getReqHeader("User-Agent")
		forwardedFor := getReqHeader("X-Forwarded-For")
		remoteIP := getReqHeader("X-Real-IP")

		// Build a fingerprint from available request identity signals
		fingerprint := userAgent + "|" + forwardedFor + "|" + remoteIP
		hash := fnv32a(fingerprint)

		// Use traffic_split for 2-variant case; for 3+ variants distribute evenly
		// after the first variant gets its configured percentage.
		bucketPct := hash % 100

		if len(variants) == 2 {
			if bucketPct < uint32(trafficSplit) {
				assignedVariant = variants[0]
			} else {
				assignedVariant = variants[1]
			}
		} else {
			// For multi-variant: first variant gets trafficSplit%, rest share equally
			if bucketPct < uint32(trafficSplit) {
				assignedVariant = variants[0]
			} else {
				remaining := len(variants) - 1
				if remaining <= 0 {
					assignedVariant = variants[0]
				} else {
					// Distribute remaining percentage equally among other variants
					remainingPct := bucketPct - uint32(trafficSplit)
					segmentSize := uint32(100-trafficSplit) / uint32(remaining)
					if segmentSize == 0 {
						segmentSize = 1
					}
					idx := int(remainingPct / segmentSize)
					if idx >= remaining {
						idx = remaining - 1
					}
					assignedVariant = variants[1+idx]
				}
			}
		}
	}

	// Set headers for downstream routing decisions
	setReqHeader(headerName, assignedVariant)
	setReqHeader("X-AB-Experiment", experimentName)

	logInfo("abtesting: experiment=" + experimentName + " variant=" + assignedVariant)
}

//export on_response_headers
func onResponseHeaders() {
	// Propagate experiment assignment to response headers for client-side tracking
	experimentName := getConfig("experiment_name")
	if experimentName == "" {
		experimentName = "default"
	}

	headerName := getConfig("header_name")
	if headerName == "" {
		headerName = "X-AB-Variant"
	}

	// Read the variant that was set during request phase from the request header
	// (the host should forward it). If not available, read from response header
	// in case the backend set it.
	variant := getReqHeader(headerName)
	if variant == "" {
		return
	}

	setRespHeader(headerName, variant)
	setRespHeader("X-AB-Experiment", experimentName)

	// Set a cookie for sticky assignment on subsequent requests
	cookieName := getConfig("cookie_name")
	if cookieName == "" {
		cookieName = "ab_bucket"
	}

	cookieValue := cookieName + "=" + variant + "; Path=/; Max-Age=86400; SameSite=Lax"
	setRespHeader("Set-Cookie", cookieValue)
}

func main() {}
