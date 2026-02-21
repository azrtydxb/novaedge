//go:build linux

package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// sysctlRecommendation holds a recommended sysctl parameter and its value.
type sysctlRecommendation struct {
	key      string
	value    string
	minValue int64 // for numeric comparisons; -1 means use string comparison
}

// recommendedSysctls defines the kernel parameters recommended for a
// high-performance proxy workload. Each entry specifies the sysctl key,
// the recommended value, and the minimum numeric threshold (or -1 for
// values that require string comparison).
var recommendedSysctls = []sysctlRecommendation{
	{key: "net.core.somaxconn", value: "65535", minValue: 65535},
	{key: "net.core.netdev_max_backlog", value: "65535", minValue: 65535},
	{key: "net.ipv4.tcp_max_syn_backlog", value: "65535", minValue: 65535},
	{key: "net.core.rmem_max", value: "16777216", minValue: 16777216},
	{key: "net.core.wmem_max", value: "16777216", minValue: 16777216},
	{key: "net.ipv4.tcp_rmem", value: "4096 87380 16777216", minValue: -1},
	{key: "net.ipv4.tcp_wmem", value: "4096 65536 16777216", minValue: -1},
	{key: "net.ipv4.tcp_fin_timeout", value: "10", minValue: -1},
	{key: "net.ipv4.tcp_tw_reuse", value: "1", minValue: 1},
	{key: "net.ipv4.ip_local_port_range", value: "1024 65535", minValue: -1},
	{key: "net.ipv4.tcp_fastopen", value: "3", minValue: 3},
	{key: "net.ipv4.tcp_slow_start_after_idle", value: "0", minValue: -1},
	{key: "net.ipv4.tcp_keepalive_time", value: "60", minValue: -1},
	{key: "net.ipv4.tcp_keepalive_intvl", value: "10", minValue: -1},
	{key: "net.ipv4.tcp_keepalive_probes", value: "6", minValue: 6},
	{key: "net.core.optmem_max", value: "65536", minValue: 65536},
	{key: "net.ipv4.tcp_max_tw_buckets", value: "2000000", minValue: 2000000},
	{key: "net.ipv4.tcp_notsent_lowat", value: "16384", minValue: 16384},
}

// CheckKernelParameters reads each recommended sysctl from /proc/sys and logs
// a warning for any parameter that is below the recommended value. This function
// is advisory only and never modifies kernel parameters.
func CheckKernelParameters(logger *zap.Logger) {
	logger.Info("checking kernel tuning parameters")

	for _, rec := range recommendedSysctls {
		current, err := readSysctl(rec.key)
		if err != nil {
			logger.Warn("unable to read sysctl parameter",
				zap.String("sysctl", rec.key),
				zap.Error(err),
			)
			continue
		}

		if isBelowRecommended(current, rec) {
			logger.Warn("kernel parameter below recommended value",
				zap.String("sysctl", rec.key),
				zap.String("current", current),
				zap.String("recommended", rec.value),
			)
		} else {
			logger.Info("kernel parameter OK",
				zap.String("sysctl", rec.key),
				zap.String("current", current),
				zap.String("recommended", rec.value),
			)
		}
	}
}

// GetRecommendedSysctls returns the full set of recommended sysctl key-value
// pairs. This is intended for use in Helm chart init containers or
// documentation generation.
func GetRecommendedSysctls() map[string]string {
	result := make(map[string]string, len(recommendedSysctls))
	for _, rec := range recommendedSysctls {
		result[rec.key] = rec.value
	}
	return result
}

// readSysctl reads the current value of a sysctl parameter from /proc/sys.
// The sysctl key (e.g. "net.core.somaxconn") is converted to a file path
// by replacing dots with directory separators.
func readSysctl(key string) (string, error) {
	// Convert sysctl key to /proc/sys path: net.core.somaxconn -> /proc/sys/net/core/somaxconn
	parts := strings.Split(key, ".")
	elems := append([]string{"/proc", "sys"}, parts...)
	path := filepath.Join(elems...)
	path = filepath.Clean(path)

	data, err := os.ReadFile(path) //#nosec G304 -- path is constructed from a known allowlist of sysctl keys
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	return strings.TrimSpace(string(data)), nil
}

// isBelowRecommended checks whether the current sysctl value is below the
// recommended threshold. For numeric parameters (minValue >= 0) it performs
// an integer comparison. For multi-value or special parameters (minValue == -1)
// it falls back to a string equality check.
func isBelowRecommended(current string, rec sysctlRecommendation) bool {
	// For parameters that need string comparison (multi-value or special semantics).
	if rec.minValue < 0 {
		return normalizeWhitespace(current) != normalizeWhitespace(rec.value)
	}

	currentVal, err := strconv.ParseInt(current, 10, 64)
	if err != nil {
		// If we can't parse the current value as a number, treat it as mismatched.
		return true
	}

	return currentVal < rec.minValue
}

// normalizeWhitespace collapses runs of whitespace into single spaces and trims
// leading/trailing whitespace, allowing tab-separated /proc values to compare
// correctly against space-separated recommendations.
func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
