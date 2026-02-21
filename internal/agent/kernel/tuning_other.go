//go:build !linux

package kernel

import "go.uber.org/zap"

// CheckKernelParameters is a no-op on non-Linux platforms. Kernel tuning
// parameters are only available via /proc/sys on Linux.
func CheckKernelParameters(logger *zap.Logger) {
	logger.Debug("kernel parameter checking is only supported on Linux, skipping")
}

// GetRecommendedSysctls returns an empty map on non-Linux platforms.
func GetRecommendedSysctls() map[string]string {
	return map[string]string{}
}
