//go:build tinygo.wasm

// Package main implements a multi-tenant routing and isolation WASM plugin.
// It extracts tenant identifiers from headers, path prefixes, or subdomains
// and enforces tenant isolation via allowlists and downstream routing headers.
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

// extractTenantFromPath extracts the first path segment as the tenant ID.
// For "/acme/api/v1/foo", it returns "acme".
func extractTenantFromPath(path string) string {
	if len(path) == 0 {
		return ""
	}
	// Skip leading slash.
	start := 0
	if path[0] == '/' {
		start = 1
	}
	end := start
	for end < len(path) && path[end] != '/' {
		end++
	}
	if end == start {
		return ""
	}
	return path[start:end]
}

// stripFirstPathSegment removes the first path segment from the path.
// "/acme/api/v1" becomes "/api/v1".
func stripFirstPathSegment(path string) string {
	if len(path) == 0 {
		return "/"
	}
	start := 0
	if path[0] == '/' {
		start = 1
	}
	idx := start
	for idx < len(path) && path[idx] != '/' {
		idx++
	}
	if idx >= len(path) {
		return "/"
	}
	return path[idx:]
}

// extractSubdomain extracts the first subdomain from a Host header value.
// "acme.example.com" returns "acme". "example.com" returns "".
func extractSubdomain(host string) string {
	// Strip port if present.
	for i := 0; i < len(host); i++ {
		if host[i] == ':' {
			host = host[:i]
			break
		}
	}
	// Count dots to determine if there is a subdomain.
	dotCount := 0
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			dotCount++
		}
	}
	// Need at least 2 dots for a subdomain (sub.example.com).
	if dotCount < 2 {
		return ""
	}
	// Extract everything before the first dot.
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			return host[:i]
		}
	}
	return ""
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

// ---------------------------------------------------------------------------
// Request phase: extract tenant and enforce isolation
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	tenantHeader := getConfig("tenant_header")
	if tenantHeader == "" {
		tenantHeader = "X-Tenant-ID"
	}

	tenantSource := getConfig("tenant_source")
	if tenantSource == "" {
		tenantSource = "header"
	}

	stripPrefix := getConfig("path_prefix_strip")
	if stripPrefix == "" {
		stripPrefix = "true"
	}

	allowedTenantsCSV := getConfig("allowed_tenants")
	defaultTenant := getConfig("default_tenant")

	var tenantID string

	switch tenantSource {
	case "header":
		tenantID = getReqHeader(tenantHeader)
	case "path":
		path := readPath()
		tenantID = extractTenantFromPath(path)
		// Strip the tenant prefix from the path so downstream sees a clean path.
		if tenantID != "" && stripPrefix == "true" {
			newPath := stripFirstPathSegment(path)
			setReqHeader("X-Original-Path", path)
			setReqHeader("X-Rewritten-Path", newPath)
			logInfo("multitenant: stripped path prefix, original=" + path + " rewritten=" + newPath)
		}
	case "subdomain":
		host := getReqHeader("Host")
		tenantID = extractSubdomain(host)
	default:
		logError("multitenant: unknown tenant_source: " + tenantSource)
		tenantID = getReqHeader(tenantHeader)
	}

	// Apply default tenant if none found.
	if tenantID == "" {
		if defaultTenant != "" {
			tenantID = defaultTenant
			logInfo("multitenant: using default tenant " + defaultTenant)
		} else {
			logWarn("multitenant: no tenant ID found, rejecting request")
			rejectRequest(400, `{"error":"missing tenant identifier"}`)
			return
		}
	}

	// Validate against allowed tenants list if configured.
	if allowedTenantsCSV != "" {
		allowed := splitCSV(allowedTenantsCSV)
		if !contains(allowed, tenantID) {
			logWarn("multitenant: tenant " + tenantID + " not in allowed list")
			rejectRequest(403, `{"error":"tenant not authorized"}`)
			return
		}
	}

	// Set tenant headers for downstream routing and isolation enforcement.
	setReqHeader("X-Tenant-ID", tenantID)
	setReqHeader("X-Tenant-Isolated", "true")

	logInfo("multitenant: routed to tenant " + tenantID + " (source=" + tenantSource + ")")
}

//export on_response_headers
func onResponseHeaders() {}

func main() {}
