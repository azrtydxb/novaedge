//go:build tinygo.wasm

// Package main implements a Request ID propagation WASM plugin for NovaEdge.
// It ensures every request has a unique identifier header, generating one if
// absent, and propagates it to the response.
//
// Configuration keys:
//   - "header_name" → header to use (default "X-Request-ID")
//   - "propagate"   → "true" or "false", propagate to response (default "true")
//   - "prefix"      → optional prefix for generated IDs (default "novaedge")
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

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

// counter is a monotonically increasing sequence used to generate unique IDs.
// Combined with a simple hash of previous values it provides uniqueness within
// a single plugin instance lifetime without requiring crypto primitives.
var counter uint64

// generateID produces a deterministic, collision-resistant identifier using a
// simple counter and mixing function. The result is a 16-character hex string.
func generateID(prefix string) string {
	counter++
	v := counter
	// Mix bits for better distribution (splitmix64-style).
	v ^= v >> 30
	v *= 0xbf58476d1ce4e5b9
	v ^= v >> 27
	v *= 0x94d049bb133111eb
	v ^= v >> 31
	hex := toHex(v)
	if prefix != "" {
		return prefix + "-" + hex
	}
	return hex
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
// Plugin state
// ---------------------------------------------------------------------------

// lastRequestID stores the ID set during on_request_headers so it can be
// propagated during on_response_headers.
var lastRequestID string

// ---------------------------------------------------------------------------
// Exported plugin hooks
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	headerName := configValue("header_name")
	if headerName == "" {
		headerName = "X-Request-ID"
	}

	existing := readHeader(headerName)
	if existing != "" {
		lastRequestID = existing
		logDebug("requestid: using existing " + headerName + "=" + existing)
		return
	}

	prefix := configValue("prefix")
	if prefix == "" {
		prefix = "novaedge"
	}

	id := generateID(prefix)
	writeReqHeader(headerName, id)
	lastRequestID = id
	logInfo("requestid: generated " + headerName + "=" + id)
}

//export on_response_headers
func onResponseHeaders() {
	propagate := configValue("propagate")
	if propagate == "false" {
		return
	}

	headerName := configValue("header_name")
	if headerName == "" {
		headerName = "X-Request-ID"
	}

	if lastRequestID != "" {
		writeRespHeader(headerName, lastRequestID)
	}
}

func main() {}
