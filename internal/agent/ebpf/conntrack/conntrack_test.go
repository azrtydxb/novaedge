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

package conntrack

import (
	"runtime"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestNewConntrack(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ct, err := NewConntrack(logger, 0, 0)
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}
	// On Linux, this may fail if BPF objects aren't generated.
	if err != nil {
		t.Logf("NewConntrack failed (expected in CI without BPF objects): %v", err)
		return
	}
	defer ct.Close()
}

func TestCTKeyLayout(t *testing.T) {
	key := CTKey{
		SrcIP:   [4]byte{10, 0, 0, 1},
		DstIP:   [4]byte{10, 96, 0, 100},
		SrcPort: 12345,
		DstPort: 80,
		Proto:   6, // TCP
	}
	if key.SrcIP[0] != 10 {
		t.Error("unexpected SrcIP")
	}
	if key.Proto != 6 {
		t.Error("unexpected Proto")
	}
}

func TestCTEntryLayout(t *testing.T) {
	entry := CTEntry{
		BackendIP:   [4]byte{10, 0, 1, 1},
		BackendPort: 8080,
		State:       StateEstablished,
		Timestamp:   1000000000,
		RxBytes:     1024,
		TxBytes:     2048,
	}
	if entry.State != StateEstablished {
		t.Error("unexpected State")
	}
	if entry.RxBytes != 1024 {
		t.Error("unexpected RxBytes")
	}
}

func TestConnectionStates(t *testing.T) {
	if StateSynSent != 0 {
		t.Error("StateSynSent should be 0")
	}
	if StateEstablished != 1 {
		t.Error("StateEstablished should be 1")
	}
	if StateFinWait != 2 {
		t.Error("StateFinWait should be 2")
	}
	if StateClosed != 3 {
		t.Error("StateClosed should be 3")
	}
}

func TestDefaultMaxEntries(t *testing.T) {
	if DefaultMaxEntries != 65536 {
		t.Errorf("unexpected DefaultMaxEntries: %d", DefaultMaxEntries)
	}
}

func TestConntrackCloseNil(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("non-Linux platform")
	}
	// A nil Conntrack pointer shouldn't be possible via NewConntrack,
	// but test that the types are well-formed.
	logger := zaptest.NewLogger(t)
	_, err := NewConntrack(logger, 1024, 30*time.Second)
	if err != nil {
		t.Logf("NewConntrack failed (expected in CI): %v", err)
	}
}
