package dataplane

import (
	"testing"
)

func TestValidateForwardingPlane_ValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected ForwardingPlane
	}{
		{"go", ForwardingPlaneGo},
		{"rust", ForwardingPlaneRust},
		{"shadow", ForwardingPlaneShadow},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ValidateForwardingPlane(tc.input)
			if err != nil {
				t.Fatalf("ValidateForwardingPlane(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.expected {
				t.Errorf("ValidateForwardingPlane(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestValidateForwardingPlane_InvalidValues(t *testing.T) {
	invalids := []string{
		"",
		"Go",
		"RUST",
		"Shadow",
		"xdp",
		"native",
		"ebpf",
		"both",
	}

	for _, input := range invalids {
		t.Run(input, func(t *testing.T) {
			_, err := ValidateForwardingPlane(input)
			if err == nil {
				t.Fatalf("ValidateForwardingPlane(%q) expected error, got nil", input)
			}
		})
	}
}

func TestForwardingPlaneConstants(t *testing.T) {
	if ForwardingPlaneGo != "go" {
		t.Errorf("ForwardingPlaneGo = %q, want %q", ForwardingPlaneGo, "go")
	}
	if ForwardingPlaneRust != "rust" {
		t.Errorf("ForwardingPlaneRust = %q, want %q", ForwardingPlaneRust, "rust")
	}
	if ForwardingPlaneShadow != "shadow" {
		t.Errorf("ForwardingPlaneShadow = %q, want %q", ForwardingPlaneShadow, "shadow")
	}
}

func TestDefaultDataplaneSocket(t *testing.T) {
	expected := "/run/novaedge/dataplane.sock"
	if DefaultDataplaneSocket != expected {
		t.Errorf("DefaultDataplaneSocket = %q, want %q", DefaultDataplaneSocket, expected)
	}
}
