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

package maglev

import (
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestNewManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m := NewManager(logger, 0)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestNewManagerCustomSize(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m := NewManager(logger, 16381)
	if m == nil {
		t.Fatal("NewManager with custom size returned nil")
	}
}

func TestManagerCloseIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m := NewManager(logger, 0)
	if err := m.Close(); err != nil {
		t.Errorf("Close() on fresh manager returned error: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}
}

func TestManagerStatsEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m := NewManager(logger, 0)
	stats := m.Stats()
	if stats == nil {
		t.Fatal("Stats() returned nil")
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestManagerUpdateTableWithoutInit(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m := NewManager(logger, 0)
	backends := []Backend{
		{ID: 1, Addr: "10.0.0.1", Port: 8080},
	}
	err := m.UpdateTable(backends)
	if err == nil {
		t.Error("expected error calling UpdateTable without Init")
	}
}

func TestBackendType(t *testing.T) {
	be := Backend{
		ID:   1,
		Addr: "10.0.0.1",
		Port: 8080,
	}
	if be.ID != 1 {
		t.Error("unexpected ID")
	}
	if be.Addr != "10.0.0.1" {
		t.Error("unexpected Addr")
	}
	if be.Port != 8080 {
		t.Error("unexpected Port")
	}
}

func TestBackendKeyValue(t *testing.T) {
	key := BackendKey{ID: 42}
	if key.ID != 42 {
		t.Error("unexpected BackendKey ID")
	}

	val := BackendValue{
		IP:   [4]byte{10, 0, 0, 1},
		Port: 80,
	}
	if val.IP[0] != 10 {
		t.Error("unexpected BackendValue IP")
	}
	if val.Port != 80 {
		t.Error("unexpected BackendValue Port")
	}
}

func TestEntry(t *testing.T) {
	entry := Entry{BackendID: 5}
	if entry.BackendID != 5 {
		t.Error("unexpected Entry BackendID")
	}
}

func TestDefaultTableSize(t *testing.T) {
	if DefaultTableSize != 16381 {
		t.Errorf("unexpected DefaultTableSize: %d", DefaultTableSize)
	}
}
