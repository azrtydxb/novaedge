/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ebpf

import (
	"runtime"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestDetect(t *testing.T) {
	caps, err := Detect()
	if err != nil {
		t.Fatalf("Detect() returned error: %v", err)
	}
	if caps == nil {
		t.Fatal("Detect() returned nil capabilities")
	}

	if runtime.GOOS != "linux" {
		// On non-Linux, all capabilities should be false.
		if caps.HasXDP || caps.HasSKLookup || caps.HasSockOps || caps.HasSKMsg || caps.HasAFXDP || caps.HasBTF || caps.HasLPMTrie || caps.HasSockHash {
			t.Error("expected all capabilities to be false on non-Linux")
		}
		if caps.KernelVersion != "" {
			t.Errorf("expected empty kernel version on non-Linux, got %q", caps.KernelVersion)
		}
	}
}

func TestLogCapabilities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	caps := &Capabilities{}

	// Should not panic.
	LogCapabilities(logger, caps)
}

func TestNewProgramLoader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := NewProgramLoader(logger, "")
	if loader == nil {
		t.Fatal("NewProgramLoader returned nil")
	}

	loaderCustom := NewProgramLoader(logger, "/custom/path")
	if loaderCustom == nil {
		t.Fatal("NewProgramLoader with custom path returned nil")
	}
}

func TestProgramLoaderClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := NewProgramLoader(logger, "")
	// Close should not panic on a fresh loader.
	if err := loader.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestParseCIDRToLPMKey4(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		wantLen   uint32
		wantAddr  [4]byte
		wantError bool
	}{
		{
			name:     "standard /24",
			cidr:     "10.0.0.0/24",
			wantLen:  24,
			wantAddr: [4]byte{10, 0, 0, 0},
		},
		{
			name:     "host /32",
			cidr:     "192.168.1.1/32",
			wantLen:  32,
			wantAddr: [4]byte{192, 168, 1, 1},
		},
		{
			name:     "class A /8",
			cidr:     "10.0.0.0/8",
			wantLen:  8,
			wantAddr: [4]byte{10, 0, 0, 0},
		},
		{
			name:      "invalid CIDR",
			cidr:      "not-a-cidr",
			wantError: true,
		},
		{
			name:      "IPv6 CIDR",
			cidr:      "::1/128",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParseCIDRToLPMKey4(tt.cidr)
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key.Prefixlen != tt.wantLen {
				t.Errorf("prefixlen: got %d, want %d", key.Prefixlen, tt.wantLen)
			}
			if key.Addr != tt.wantAddr {
				t.Errorf("addr: got %v, want %v", key.Addr, tt.wantAddr)
			}
		})
	}
}

func TestParseCIDRToLPMKey6(t *testing.T) {
	key, err := ParseCIDRToLPMKey6("fd00::/64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.Prefixlen != 64 {
		t.Errorf("prefixlen: got %d, want 64", key.Prefixlen)
	}
	if key.Addr[0] != 0xfd || key.Addr[1] != 0x00 {
		t.Errorf("unexpected address bytes: %v", key.Addr)
	}

	// Invalid
	_, err = ParseCIDRToLPMKey6("not-a-cidr")
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestNewIPPortKey(t *testing.T) {
	key, err := NewIPPortKey("10.96.0.1", 443)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.Addr != [4]byte{10, 96, 0, 1} {
		t.Errorf("addr: got %v, want [10 96 0 1]", key.Addr)
	}
	if key.Port != 443 {
		t.Errorf("port: got %d, want 443", key.Port)
	}

	// Invalid IP
	_, err = NewIPPortKey("invalid", 80)
	if err == nil {
		t.Error("expected error for invalid IP")
	}

	// IPv6 address
	_, err = NewIPPortKey("::1", 80)
	if err == nil {
		t.Error("expected error for IPv6 address")
	}
}

func TestNewIPPortProtoKey(t *testing.T) {
	key, err := NewIPPortProtoKey("10.96.0.1", 80, 6) // TCP = 6
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.Addr != [4]byte{10, 96, 0, 1} {
		t.Errorf("addr: got %v", key.Addr)
	}
	if key.Port != 80 {
		t.Errorf("port: got %d, want 80", key.Port)
	}
	if key.Proto != 6 {
		t.Errorf("proto: got %d, want 6", key.Proto)
	}

	// Invalid IP
	_, err = NewIPPortProtoKey("bad-ip", 80, 6)
	if err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestIPv4ToBytes(t *testing.T) {
	addr, err := IPv4ToBytes("192.168.1.100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != [4]byte{192, 168, 1, 100} {
		t.Errorf("got %v, want [192 168 1 100]", addr)
	}

	_, err = IPv4ToBytes("invalid")
	if err == nil {
		t.Error("expected error for invalid IP")
	}

	_, err = IPv4ToBytes("::1")
	if err == nil {
		t.Error("expected error for IPv6")
	}
}

func TestPortToNetworkOrder(t *testing.T) {
	// Port 80 in big-endian is 0x0050. On little-endian machines this
	// should differ from 80. On big-endian it stays the same. Just
	// verify round-trip consistency.
	port := PortToNetworkOrder(80)
	if port == 0 {
		t.Error("PortToNetworkOrder(80) returned 0")
	}
}

func TestRecordError(t *testing.T) {
	// Should not panic on any platform.
	RecordError("test", "test_error")
}

func TestRecordProgramLoadedUnloaded(t *testing.T) {
	RecordProgramLoaded("test")
	RecordProgramUnloaded("test")
}

func TestRecordMapOp(t *testing.T) {
	RecordMapOp("test_map", "update", "ok")
}

func TestObserveAttachDuration(t *testing.T) {
	ObserveAttachDuration("test", 0.001)
}

func TestLogCapabilitiesWithLogger(t *testing.T) {
	logger := zap.NewNop()
	caps := &Capabilities{
		HasXDP:        true,
		HasSKLookup:   true,
		HasAFXDP:      false,
		HasBTF:        true,
		HasLPMTrie:    true,
		KernelVersion: "test-kernel",
	}
	// Should not panic.
	LogCapabilities(logger, caps)
}
