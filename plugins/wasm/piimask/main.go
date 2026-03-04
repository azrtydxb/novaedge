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

// configBool reads a config key and returns its boolean value with a default.
func configBool(key string, defaultVal bool) bool {
	v := getConfig(key)
	if v == "" {
		return defaultVal
	}
	return v == "true" || v == "1" || v == "yes"
}

// isDigit returns true if the byte is an ASCII digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// looksLikeEmail checks if a string contains an email-like pattern.
func looksLikeEmail(value string) bool {
	atIdx := -1
	for i := 0; i < len(value); i++ {
		if value[i] == '@' {
			atIdx = i
			break
		}
	}
	if atIdx < 1 || atIdx >= len(value)-1 {
		return false
	}
	hasDot := false
	for i := atIdx + 1; i < len(value); i++ {
		if value[i] == '.' {
			hasDot = true
			break
		}
	}
	return hasDot
}

// maskEmail masks an email address, keeping only the first character of local
// part and domain visible: user@example.com -> u***@e******.com
func maskEmail(value string, maskChar byte) string {
	atIdx := -1
	for i := 0; i < len(value); i++ {
		if value[i] == '@' {
			atIdx = i
			break
		}
	}
	if atIdx < 1 {
		return value
	}
	result := make([]byte, len(value))
	result[0] = value[0]
	for i := 1; i < atIdx; i++ {
		result[i] = maskChar
	}
	result[atIdx] = '@'
	for i := atIdx + 1; i < len(value); i++ {
		if value[i] == '.' {
			result[i] = '.'
		} else {
			result[i] = maskChar
		}
	}
	return string(result)
}

// looksLikePhone checks if a value looks like a phone number (7-15 digits with
// optional separators).
func looksLikePhone(value string) bool {
	digitCount := 0
	for i := 0; i < len(value); i++ {
		if isDigit(value[i]) {
			digitCount++
		} else if value[i] != '-' && value[i] != ' ' && value[i] != '(' &&
			value[i] != ')' && value[i] != '+' {
			return false
		}
	}
	return digitCount >= 7 && digitCount <= 15
}

// maskPhone masks a phone number, showing only the last 4 digits.
func maskPhone(value string, maskChar byte) string {
	result := make([]byte, len(value))
	revDigitIdx := 0
	for i := len(value) - 1; i >= 0; i-- {
		if isDigit(value[i]) {
			if revDigitIdx < 4 {
				result[i] = value[i]
			} else {
				result[i] = maskChar
			}
			revDigitIdx++
		} else {
			result[i] = value[i]
		}
	}
	return string(result)
}

// looksLikeSSN checks for patterns like 123-45-6789 or 123456789 (exactly 9
// digits with optional dashes).
func looksLikeSSN(value string) bool {
	digits := 0
	for i := 0; i < len(value); i++ {
		if isDigit(value[i]) {
			digits++
		} else if value[i] != '-' {
			return false
		}
	}
	return digits == 9
}

// maskSSN masks an SSN, showing only the last 4 digits: ***-**-6789
func maskSSN(value string, maskChar byte) string {
	result := make([]byte, len(value))
	revDigitIdx := 0
	for i := len(value) - 1; i >= 0; i-- {
		if isDigit(value[i]) {
			if revDigitIdx < 4 {
				result[i] = value[i]
			} else {
				result[i] = maskChar
			}
			revDigitIdx++
		} else {
			result[i] = value[i]
		}
	}
	return string(result)
}

// looksLikeCreditCard checks for 13-19 digit sequences with optional
// dashes or spaces as separators.
func looksLikeCreditCard(value string) bool {
	digits := 0
	for i := 0; i < len(value); i++ {
		if isDigit(value[i]) {
			digits++
		} else if value[i] != '-' && value[i] != ' ' {
			return false
		}
	}
	return digits >= 13 && digits <= 19
}

// maskCreditCard masks a credit card number, showing only the last 4 digits.
func maskCreditCard(value string, maskChar byte) string {
	result := make([]byte, len(value))
	revDigitIdx := 0
	for i := len(value) - 1; i >= 0; i-- {
		if isDigit(value[i]) {
			if revDigitIdx < 4 {
				result[i] = value[i]
			} else {
				result[i] = maskChar
			}
			revDigitIdx++
		} else {
			result[i] = value[i]
		}
	}
	return string(result)
}

// itoa converts a non-negative integer to its string representation.
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

// Response headers to scan for PII content.
var piiScanHeaders = []string{
	"X-User-Email",
	"X-User-Phone",
	"X-User-SSN",
	"X-User-Card",
	"X-Customer-Email",
	"X-Customer-Phone",
	"X-Account-Number",
	"X-Contact-Info",
	"X-Personal-Data",
	"Set-Cookie",
}

//export on_request_headers
func onRequestHeaders() {}

// piiMaskConfig holds the PII masking settings loaded from the plugin configuration.
type piiMaskConfig struct {
	maskEmail      bool
	maskPhone      bool
	maskSSN        bool
	maskCreditCard bool
	maskChar       byte
}

// loadPIIMaskConfig reads masking settings from the plugin configuration.
func loadPIIMaskConfig() piiMaskConfig {
	c := piiMaskConfig{
		maskEmail:      configBool("mask_email", true),
		maskPhone:      configBool("mask_phone", true),
		maskSSN:        configBool("mask_ssn", true),
		maskCreditCard: configBool("mask_creditcard", true),
		maskChar:       '*',
	}
	if raw := getConfig("mask_char"); len(raw) > 0 {
		c.maskChar = raw[0]
	}
	return c
}

// buildActiveRules builds a comma-separated list of active PII masking rules.
func buildActiveRules(c piiMaskConfig) string {
	var parts []string
	if c.maskEmail {
		parts = append(parts, "email")
	}
	if c.maskPhone {
		parts = append(parts, "phone")
	}
	if c.maskSSN {
		parts = append(parts, "ssn")
	}
	if c.maskCreditCard {
		parts = append(parts, "creditcard")
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ","
		}
		result += p
	}
	return result
}

// maskHeaderValue applies PII masking rules to a single header value and returns
// the (possibly masked) value and whether any masking was applied.
func maskHeaderValue(original string, c piiMaskConfig) (string, bool) {
	value := original
	masked := false

	if c.maskEmail && looksLikeEmail(value) {
		value = maskEmail(value, c.maskChar)
		masked = true
	}

	// Only apply phone masking if it was not already matched as email
	if c.maskPhone && !looksLikeEmail(original) && looksLikePhone(value) {
		value = maskPhone(value, c.maskChar)
		masked = true
	}

	if c.maskSSN && looksLikeSSN(original) {
		value = maskSSN(value, c.maskChar)
		masked = true
	}

	if c.maskCreditCard && looksLikeCreditCard(original) {
		value = maskCreditCard(value, c.maskChar)
		masked = true
	}

	return value, masked
}

//export on_response_headers
func onResponseHeaders() {
	cfg := loadPIIMaskConfig()

	// Build active rules list for the X-PII-Policy header so host middleware
	// can apply body-level masking that the WASM plugin cannot perform directly.
	if activeRules := buildActiveRules(cfg); activeRules != "" {
		setRespHeader("X-PII-Policy", activeRules)
	}

	maskedCount := 0
	for _, hdr := range piiScanHeaders {
		value := getRespHeader(hdr)
		if value == "" {
			continue
		}

		if newValue, masked := maskHeaderValue(value, cfg); masked {
			setRespHeader(hdr, newValue)
			maskedCount++
		}
	}

	if maskedCount > 0 {
		setRespHeader("X-PII-Masked", "true")
		logInfo("piimask: masked PII in " + itoa(maskedCount) + " response header(s)")
	}
}

func main() {}
