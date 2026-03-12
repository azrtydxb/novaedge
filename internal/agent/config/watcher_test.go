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

package config

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestNewWatcher(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	w, err := NewWatcher(ctx, "node-1", "v1.0.0", "localhost:9090", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if w.nodeName != "node-1" {
		t.Errorf("expected nodeName 'node-1', got %q", w.nodeName)
	}
	if w.agentVersion != "v1.0.0" {
		t.Errorf("expected agentVersion 'v1.0.0', got %q", w.agentVersion)
	}
	if w.controllerAddr != "localhost:9090" {
		t.Errorf("expected controllerAddr 'localhost:9090', got %q", w.controllerAddr)
	}
	if w.tlsEnabled {
		t.Error("expected TLS to be disabled by default")
	}
}

func TestNewWatcherWithTLS_Success(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tlsConfig := &TLSConfig{
		CertFile: "/etc/certs/tls.crt",
		KeyFile:  "/etc/certs/tls.key",
		CAFile:   "/etc/certs/ca.crt",
	}

	w, err := NewWatcherWithTLS(ctx, "node-1", "v1.0.0", "localhost:9090", tlsConfig, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if !w.tlsEnabled {
		t.Error("expected TLS to be enabled")
	}
	if w.tlsCertFile != "/etc/certs/tls.crt" {
		t.Errorf("expected cert file path, got %q", w.tlsCertFile)
	}
}

func TestNewWatcherWithTLS_NilConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	_, err := NewWatcherWithTLS(ctx, "node-1", "v1.0.0", "localhost:9090", nil, logger)
	if err == nil {
		t.Fatal("expected error for nil TLS config")
	}
}

func TestNewWatcherWithTLS_IncompleteCert(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tlsConfig := &TLSConfig{
		CertFile: "/etc/certs/tls.crt",
		KeyFile:  "",
		CAFile:   "/etc/certs/ca.crt",
	}

	_, err := NewWatcherWithTLS(ctx, "node-1", "v1.0.0", "localhost:9090", tlsConfig, logger)
	if err == nil {
		t.Fatal("expected error for incomplete TLS config")
	}
}

func TestNewRemoteWatcher_Success(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tlsConfig := &TLSConfig{
		CertFile: "/etc/certs/tls.crt",
		KeyFile:  "/etc/certs/tls.key",
		CAFile:   "/etc/certs/ca.crt",
	}
	clusterConfig := &ClusterConfig{
		Name:   "cluster-west",
		Region: "us-west-2",
		Zone:   "us-west-2a",
		Labels: map[string]string{"env": "prod"},
	}

	w, err := NewRemoteWatcher(ctx, "node-1", "v1.0.0", "hub:9090", tlsConfig, clusterConfig, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if !w.IsRemote() {
		t.Error("expected IsRemote to return true")
	}
	if w.GetClusterName() != "cluster-west" {
		t.Errorf("expected cluster name 'cluster-west', got %q", w.GetClusterName())
	}
	if w.clusterRegion != "us-west-2" {
		t.Errorf("expected region 'us-west-2', got %q", w.clusterRegion)
	}
}

func TestNewRemoteWatcher_NilTLSConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	clusterConfig := &ClusterConfig{Name: "cluster-west"}

	_, err := NewRemoteWatcher(ctx, "node-1", "v1.0.0", "hub:9090", nil, clusterConfig, logger)
	if err == nil {
		t.Fatal("expected error for nil TLS config")
	}
}

func TestNewRemoteWatcher_NilClusterConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	tlsConfig := &TLSConfig{
		CertFile: "/etc/certs/tls.crt",
		KeyFile:  "/etc/certs/tls.key",
		CAFile:   "/etc/certs/ca.crt",
	}

	_, err := NewRemoteWatcher(ctx, "node-1", "v1.0.0", "hub:9090", tlsConfig, nil, logger)
	if err == nil {
		t.Fatal("expected error for nil cluster config")
	}
}

func TestNewRemoteWatcher_EmptyClusterName(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	tlsConfig := &TLSConfig{
		CertFile: "/etc/certs/tls.crt",
		KeyFile:  "/etc/certs/tls.key",
		CAFile:   "/etc/certs/ca.crt",
	}
	clusterConfig := &ClusterConfig{Name: ""}

	_, err := NewRemoteWatcher(ctx, "node-1", "v1.0.0", "hub:9090", tlsConfig, clusterConfig, logger)
	if err == nil {
		t.Fatal("expected error for empty cluster name")
	}
}

func TestWatcher_IsRemote_LocalAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	w, err := NewWatcher(ctx, "node-1", "v1.0.0", "localhost:9090", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.IsRemote() {
		t.Error("expected IsRemote to return false for local agent")
	}
	if w.GetClusterName() != "" {
		t.Errorf("expected empty cluster name for local agent, got %q", w.GetClusterName())
	}
}
