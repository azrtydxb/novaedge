//go:build tinygo.wasm

// Package main implements a GeoIP header enrichment WASM plugin for NovaEdge.
// It reads client IP from X-Real-IP or X-Forwarded-For, performs a prefix-based
// lookup using plugin configuration, and sets X-Geo-Country, X-Geo-City, and
// X-Geo-Region response headers.
//
// Configuration keys:
//   - "ip_prefix:<prefix>" → "country_code" (e.g. "ip_prefix:203.0" → "AU")
//   - "city:<country_code>" → "city_name" (e.g. "city:AU" → "Sydney")
//   - "region:<country_code>" → "region_name" (e.g. "region:AU" → "Oceania")
//   - "default_country" → fallback country code (default "XX")
//   - "default_city" → fallback city name (default "Unknown")
//   - "default_region" → fallback region name (default "Unknown")
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

func getHeader(name string) string {
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
// GeoIP logic
// ---------------------------------------------------------------------------

// extractClientIP returns the client IP from X-Real-IP or the first entry in
// X-Forwarded-For.
func extractClientIP() string {
	ip := getHeader("X-Real-IP")
	if ip != "" {
		return ip
	}
	xff := getHeader("X-Forwarded-For")
	if xff == "" {
		return ""
	}
	// Take the first IP in the comma-separated list.
	for i := 0; i < len(xff); i++ {
		if xff[i] == ',' {
			return trimSpaces(xff[:i])
		}
	}
	return trimSpaces(xff)
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

// ipPrefixes returns progressively shorter dotted prefixes to try.
// For "203.0.113.5" it yields: "203.0.113.5", "203.0.113", "203.0", "203".
func ipPrefixes(ip string) []string {
	var prefixes []string
	prefixes = append(prefixes, ip)
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' {
			prefixes = append(prefixes, ip[:i])
		}
	}
	return prefixes
}

func lookupCountry(ip string) string {
	for _, prefix := range ipPrefixes(ip) {
		cc := configValue("ip_prefix:" + prefix)
		if cc != "" {
			return cc
		}
	}
	def := configValue("default_country")
	if def != "" {
		return def
	}
	return "XX"
}

func lookupCity(country string) string {
	city := configValue("city:" + country)
	if city != "" {
		return city
	}
	def := configValue("default_city")
	if def != "" {
		return def
	}
	return "Unknown"
}

func lookupRegion(country string) string {
	region := configValue("region:" + country)
	if region != "" {
		return region
	}
	def := configValue("default_region")
	if def != "" {
		return def
	}
	return "Unknown"
}

// ---------------------------------------------------------------------------
// Exported plugin hooks
// ---------------------------------------------------------------------------

//export on_request_headers
func onRequestHeaders() {
	ip := extractClientIP()
	if ip == "" {
		logDebug("geoip: no client IP found in X-Real-IP or X-Forwarded-For")
		return
	}

	country := lookupCountry(ip)
	city := lookupCity(country)
	region := lookupRegion(country)

	setReqHeader("X-Geo-Country", country)
	setReqHeader("X-Geo-City", city)
	setReqHeader("X-Geo-Region", region)

	logInfo("geoip: enriched ip=" + ip + " country=" + country + " city=" + city + " region=" + region)
}

//export on_response_headers
func onResponseHeaders() {
	// Pass geo headers to the response so downstream clients can inspect them.
	country := getHeader("X-Geo-Country")
	if country != "" {
		setRespHeader("X-Geo-Country", country)
		setRespHeader("X-Geo-City", getHeader("X-Geo-City"))
		setRespHeader("X-Geo-Region", getHeader("X-Geo-Region"))
	}
}

func main() {}
