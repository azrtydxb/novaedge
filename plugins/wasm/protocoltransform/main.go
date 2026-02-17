//go:build tinygo.wasm

// Package main implements a protocol transformation WASM plugin for
// SOAP/XML to REST/JSON conversion. It detects SOAP and XML requests,
// sets transformation hint headers for the host middleware to perform
// the actual body transformation, and maps SOAPAction headers to
// REST-style path hints.
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

// containsSubstring checks if haystack contains needle (case-sensitive).
func containsSubstring(haystack, needle string) bool {
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

// toLower converts ASCII uppercase to lowercase.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// extractSOAPAction extracts the action name from a SOAPAction header value.
// SOAPAction values often look like "http://example.com/service/GetUser" or
// "urn:example:GetUser". This extracts the last path/fragment segment.
func extractSOAPAction(action string) string {
	if len(action) == 0 {
		return ""
	}
	// Strip surrounding quotes if present.
	if action[0] == '"' && len(action) > 1 && action[len(action)-1] == '"' {
		action = action[1 : len(action)-1]
	}
	if len(action) == 0 {
		return ""
	}
	// Find the last separator (/ or :).
	lastSep := -1
	for i := len(action) - 1; i >= 0; i-- {
		if action[i] == '/' || action[i] == ':' {
			lastSep = i
			break
		}
	}
	if lastSep >= 0 && lastSep < len(action)-1 {
		return action[lastSep+1:]
	}
	return action
}

// isSOAPContentType checks if the content type indicates a SOAP request.
func isSOAPContentType(ct string) bool {
	lower := toLower(ct)
	return containsSubstring(lower, "text/xml") ||
		containsSubstring(lower, "application/soap+xml")
}

// isXMLContentType checks if the content type indicates an XML request.
func isXMLContentType(ct string) bool {
	lower := toLower(ct)
	return containsSubstring(lower, "text/xml") ||
		containsSubstring(lower, "application/xml") ||
		containsSubstring(lower, "application/soap+xml")
}

// ---------------------------------------------------------------------------
// Request phase: detect SOAP/XML and set transformation hints
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	mode := getConfig("mode")
	if mode == "" {
		mode = "soap-to-rest"
	}

	soapActionHeader := getConfig("soap_action_header")
	if soapActionHeader == "" {
		soapActionHeader = "SOAPAction"
	}

	targetContentType := getConfig("target_content_type")
	if targetContentType == "" {
		targetContentType = "application/json"
	}

	stripNamespace := getConfig("strip_namespace")
	if stripNamespace == "" {
		stripNamespace = "true"
	}

	contentType := getReqHeader("Content-Type")
	if contentType == "" {
		return
	}

	needsTransform := false

	switch mode {
	case "soap-to-rest":
		if !isSOAPContentType(contentType) {
			return
		}
		needsTransform = true

		// Extract SOAPAction and map to REST-style action hint.
		soapAction := getReqHeader(soapActionHeader)
		if soapAction != "" {
			actionName := extractSOAPAction(soapAction)
			if actionName != "" {
				setReqHeader("X-Transform-Action", actionName)
				logInfo("protocoltransform: mapped SOAPAction to action=" + actionName)
			}
		}

		// Set method mapping hint: SOAP POST becomes a REST-style operation.
		method := readMethod()
		if method == "POST" {
			setReqHeader("X-Transform-Original-Method", "POST")
		}

		logInfo("protocoltransform: SOAP request detected, mode=" + mode + " content-type=" + contentType)

	case "xml-to-json":
		if !isXMLContentType(contentType) {
			return
		}
		needsTransform = true

		logInfo("protocoltransform: XML request detected, mode=" + mode + " content-type=" + contentType)

	default:
		logError("protocoltransform: unknown mode: " + mode)
		return
	}

	if needsTransform {
		setReqHeader("X-Transform-Mode", mode)
		setReqHeader("X-Transform-Target-Type", targetContentType)
		setReqHeader("X-Transform-Strip-Namespace", stripNamespace)
		setReqHeader("X-Transform-Original-Content-Type", contentType)

		path := readPath()
		logInfo("protocoltransform: transformation hints set for path=" + path)
	}
}

// ---------------------------------------------------------------------------
// Response phase: set content-type transformation hints for middleware
// ---------------------------------------------------------------------------

//export on_response_headers
func onResponseHeaders() {
	mode := getConfig("mode")
	if mode == "" {
		mode = "soap-to-rest"
	}

	targetContentType := getConfig("target_content_type")
	if targetContentType == "" {
		targetContentType = "application/json"
	}

	stripNamespace := getConfig("strip_namespace")
	if stripNamespace == "" {
		stripNamespace = "true"
	}

	// Check if the response needs transformation based on its content type.
	respContentType := getRespHeader("Content-Type")
	if respContentType == "" {
		return
	}

	needsTransform := false

	switch mode {
	case "soap-to-rest":
		if isSOAPContentType(respContentType) {
			needsTransform = true
		}
	case "xml-to-json":
		if isXMLContentType(respContentType) {
			needsTransform = true
		}
	default:
		return
	}

	if needsTransform {
		setRespHeader("X-Transform-Mode", mode)
		setRespHeader("X-Transform-Target-Type", targetContentType)
		setRespHeader("X-Transform-Strip-Namespace", stripNamespace)
		setRespHeader("X-Transform-Original-Content-Type", respContentType)

		logInfo("protocoltransform: response transformation hints set, mode=" + mode +
			" from=" + respContentType + " to=" + targetContentType)
	}
}

func main() {}
