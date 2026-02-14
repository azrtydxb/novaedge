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

package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// executeGenerateCommand creates a fresh generate command hierarchy and executes it
// with the given args, capturing stdout output.
func executeGenerateCommand(t *testing.T, args []string) (string, error) {
	t.Helper()

	genCmd := newGenerateCommand()

	// Capture stdout by redirecting os.Stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	genCmd.SetArgs(args)
	execErr := genCmd.Execute()

	// Restore stdout and read captured output
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close pipe reader: %v", err)
	}

	return buf.String(), execErr
}

func TestGenerateStaticPodRequiredFlags(t *testing.T) {
	output, err := executeGenerateCommand(t, []string{
		"static-pod",
		"--vip-address", "10.0.0.100/32",
		"--image", "ghcr.io/piwi3910/novaedge-agent:latest",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify key parts of the static pod manifest
	expectedStrings := []string{
		"apiVersion: v1",
		"kind: Pod",
		"name: novaedge-cpvip",
		"namespace: kube-system",
		"app.kubernetes.io/name: novaedge-cpvip",
		"app.kubernetes.io/component: control-plane-vip",
		"hostNetwork: true",
		"priorityClassName: system-node-critical",
		"image: ghcr.io/piwi3910/novaedge-agent:latest",
		"--control-plane-vip",
		"--cp-vip-address=10.0.0.100/32",
		"--cp-api-port=6443",
		"--cp-health-interval=1s",
		"--cp-health-timeout=3s",
		"NET_ADMIN",
		"NET_RAW",
		"cpu: 50m",
		"memory: 64Mi",
		"cpu: 200m",
		"memory: 128Mi",
		"/healthz",
		"/readyz",
		"port: 9091",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("static pod output missing expected content %q\nOutput:\n%s", expected, output)
		}
	}

	// --node-name should default to hostname
	hostname, hostErr := os.Hostname()
	if hostErr == nil {
		if !strings.Contains(output, "--node-name="+hostname) {
			t.Errorf("static pod output missing default node-name %q\nOutput:\n%s", hostname, output)
		}
	}

	// --interface should NOT appear (not set)
	if strings.Contains(output, "--interface=") {
		t.Errorf("static pod output should not contain --interface when not set\nOutput:\n%s", output)
	}
}

func TestGenerateStaticPodAllFlags(t *testing.T) {
	output, err := executeGenerateCommand(t, []string{
		"static-pod",
		"--vip-address", "192.168.1.50/24",
		"--image", "my-registry/novaedge-agent:v1.2.3",
		"--interface", "eth0",
		"--api-port", "8443",
		"--health-interval", "2s",
		"--health-timeout", "5s",
		"--node-name", "control-plane-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedStrings := []string{
		"image: my-registry/novaedge-agent:v1.2.3",
		"--cp-vip-address=192.168.1.50/24",
		"--node-name=control-plane-1",
		"--interface=eth0",
		"--cp-api-port=8443",
		"--cp-health-interval=2s",
		"--cp-health-timeout=5s",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("static pod output missing expected content %q\nOutput:\n%s", expected, output)
		}
	}
}

func TestGenerateStaticPodMissingVIPAddress(t *testing.T) {
	_, err := executeGenerateCommand(t, []string{
		"static-pod",
		"--image", "ghcr.io/piwi3910/novaedge-agent:latest",
	})
	if err == nil {
		t.Fatal("expected error for missing --vip-address flag, got nil")
	}

	if !strings.Contains(err.Error(), "vip-address") {
		t.Errorf("error should mention vip-address flag, got: %v", err)
	}
}

func TestGenerateStaticPodMissingImage(t *testing.T) {
	_, err := executeGenerateCommand(t, []string{
		"static-pod",
		"--vip-address", "10.0.0.100/32",
	})
	if err == nil {
		t.Fatal("expected error for missing --image flag, got nil")
	}

	if !strings.Contains(err.Error(), "image") {
		t.Errorf("error should mention image flag, got: %v", err)
	}
}

func TestGenerateSystemdUnitRequiredFlags(t *testing.T) {
	output, err := executeGenerateCommand(t, []string{
		"systemd-unit",
		"--vip-address", "10.0.0.100/32",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedStrings := []string{
		"[Unit]",
		"Description=NovaEdge Control Plane VIP Manager",
		"Documentation=https://novaedge.io",
		"After=network-online.target",
		"Wants=network-online.target",
		"[Service]",
		"Type=simple",
		"/usr/local/bin/novaedge-agent",
		"--control-plane-vip",
		"--cp-vip-address=10.0.0.100/32",
		"--cp-api-port=6443",
		"--cp-health-interval=1s",
		"--cp-health-timeout=3s",
		"Restart=always",
		"RestartSec=5",
		"LimitNOFILE=65536",
		"LimitNPROC=4096",
		"[Install]",
		"WantedBy=multi-user.target",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("systemd unit output missing expected content %q\nOutput:\n%s", expected, output)
		}
	}

	// --interface should NOT appear (not set)
	if strings.Contains(output, "--interface=") {
		t.Errorf("systemd unit output should not contain --interface when not set\nOutput:\n%s", output)
	}
}

func TestGenerateSystemdUnitAllFlags(t *testing.T) {
	output, err := executeGenerateCommand(t, []string{
		"systemd-unit",
		"--vip-address", "192.168.1.50/24",
		"--binary-path", "/opt/novaedge/bin/novaedge-agent",
		"--interface", "ens192",
		"--api-port", "8443",
		"--health-interval", "2s",
		"--health-timeout", "5s",
		"--node-name", "control-plane-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedStrings := []string{
		"/opt/novaedge/bin/novaedge-agent",
		"--cp-vip-address=192.168.1.50/24",
		"--node-name=control-plane-1",
		"--interface=ens192",
		"--cp-api-port=8443",
		"--cp-health-interval=2s",
		"--cp-health-timeout=5s",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("systemd unit output missing expected content %q\nOutput:\n%s", expected, output)
		}
	}
}

func TestGenerateSystemdUnitMissingVIPAddress(t *testing.T) {
	_, err := executeGenerateCommand(t, []string{
		"systemd-unit",
	})
	if err == nil {
		t.Fatal("expected error for missing --vip-address flag, got nil")
	}

	if !strings.Contains(err.Error(), "vip-address") {
		t.Errorf("error should mention vip-address flag, got: %v", err)
	}
}
