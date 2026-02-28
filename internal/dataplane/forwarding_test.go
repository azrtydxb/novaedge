package dataplane

import (
	"testing"
)

func TestDefaultDataplaneSocket(t *testing.T) {
	expected := "/run/novaedge/dataplane.sock"
	if DefaultDataplaneSocket != expected {
		t.Errorf("DefaultDataplaneSocket = %q, want %q", DefaultDataplaneSocket, expected)
	}
}
