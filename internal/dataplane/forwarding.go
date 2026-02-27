package dataplane

import "fmt"

// ForwardingPlane defines which forwarding implementation handles traffic.
type ForwardingPlane string

const (
	// ForwardingPlaneGo uses the existing Go forwarding path (default).
	ForwardingPlaneGo ForwardingPlane = "go"

	// ForwardingPlaneRust delegates all forwarding to the Rust dataplane daemon.
	ForwardingPlaneRust ForwardingPlane = "rust"

	// ForwardingPlaneShadow runs both Go (primary) and Rust (secondary) in
	// parallel, comparing flow events to validate the Rust implementation.
	ForwardingPlaneShadow ForwardingPlane = "shadow"

	// DefaultDataplaneSocket is the default Unix domain socket path for the
	// Rust dataplane daemon.
	DefaultDataplaneSocket = "/run/novaedge/dataplane.sock"
)

// ValidateForwardingPlane checks that the given string is a valid forwarding
// plane value and returns the typed constant.
func ValidateForwardingPlane(s string) (ForwardingPlane, error) {
	switch ForwardingPlane(s) {
	case ForwardingPlaneGo, ForwardingPlaneRust, ForwardingPlaneShadow:
		return ForwardingPlane(s), nil
	default:
		return "", fmt.Errorf("invalid --forwarding-plane value %q: must be go, rust, or shadow", s)
	}
}
