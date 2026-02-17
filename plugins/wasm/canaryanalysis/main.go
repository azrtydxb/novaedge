//go:build tinygo.wasm

// Package main implements a canary analysis and auto-rollback WASM plugin.
// It tags requests with canary version headers based on traffic split configuration
// and tracks success/failure on responses for canary monitoring.
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

// simpleHash computes a deterministic hash of a string for traffic splitting.
func simpleHash(s string) uint32 {
	var h uint32
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	return h
}

// parseUint parses a simple base-10 unsigned integer from a string.
func parseUint(s string) uint32 {
	if len(s) == 0 {
		return 0
	}
	var n uint32
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + uint32(c-'0')
	}
	return n
}

// uintToStr converts a uint32 to its decimal string representation.
func uintToStr(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// Request phase: tag requests with canary version based on traffic split
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	canaryVersion := getConfig("canary_version")
	if canaryVersion == "" {
		logError("canaryanalysis: canary_version config is required")
		return
	}

	stableVersion := getConfig("stable_version")
	if stableVersion == "" {
		stableVersion = "v1"
	}

	canaryHeader := getConfig("canary_header")
	if canaryHeader == "" {
		canaryHeader = "X-Canary-Version"
	}

	trafficPctStr := getConfig("traffic_percent")
	trafficPct := uint32(10)
	if trafficPctStr != "" {
		trafficPct = parseUint(trafficPctStr)
	}
	if trafficPct > 100 {
		trafficPct = 100
	}

	// If the request already has an explicit canary header, respect it.
	existing := getReqHeader(canaryHeader)
	if existing != "" {
		logInfo("canaryanalysis: request already tagged with version " + existing)
		return
	}

	// Build a client identifier from common headers for deterministic hashing.
	clientID := getReqHeader("X-Forwarded-For")
	if clientID == "" {
		clientID = getReqHeader("X-Real-IP")
	}
	if clientID == "" {
		clientID = getReqHeader("User-Agent")
	}

	// Determine canary assignment using hash-based traffic splitting.
	bucket := simpleHash(clientID) % 100
	assignedVersion := stableVersion
	if bucket < trafficPct {
		assignedVersion = canaryVersion
	}

	setReqHeader(canaryHeader, assignedVersion)
	setReqHeader("X-Canary-Bucket", uintToStr(bucket))
	logInfo("canaryanalysis: assigned version " + assignedVersion + " (bucket " + uintToStr(bucket) + "/" + uintToStr(trafficPct) + "%)")
}

// ---------------------------------------------------------------------------
// Response phase: track success/failure for the canary version
// ---------------------------------------------------------------------------

//export on_response_headers
func onResponseHeaders() {
	canaryHeader := getConfig("canary_header")
	if canaryHeader == "" {
		canaryHeader = "X-Canary-Version"
	}

	errorThresholdStr := getConfig("error_threshold")
	errorThreshold := uint32(5)
	if errorThresholdStr != "" {
		errorThreshold = parseUint(errorThresholdStr)
	}

	// Read the version that was assigned during the request phase.
	version := getRespHeader(canaryHeader)
	if version == "" {
		// Fallback: the routing layer may have propagated it differently.
		version = getRespHeader("X-Served-By-Version")
	}

	// Determine success or failure from response status header.
	statusStr := getRespHeader(":status")
	status := parseUint(statusStr)

	result := "success"
	if status >= 500 {
		result = "error"
		logWarn("canaryanalysis: version " + version + " returned error status " + statusStr)
	} else if status >= 400 {
		result = "client-error"
	}

	setRespHeader("X-Canary-Result", result)
	if version != "" {
		setRespHeader("X-Canary-Served", version)
	}
	setRespHeader("X-Canary-Error-Threshold", uintToStr(errorThreshold)+"%")

	logInfo("canaryanalysis: response result=" + result + " version=" + version + " threshold=" + uintToStr(errorThreshold) + "%")
}

func main() {}
