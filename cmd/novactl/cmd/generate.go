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
	"fmt"
	"os"
	"text/template"

	"github.com/spf13/cobra"
)

// staticPodTemplate is the text/template for generating a Kubernetes static pod manifest.
const staticPodTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: novaedge-cpvip
  namespace: kube-system
  labels:
    app.kubernetes.io/name: novaedge-cpvip
    app.kubernetes.io/component: control-plane-vip
spec:
  hostNetwork: true
  priorityClassName: system-node-critical
  containers:
  - name: novaedge-agent
    image: {{ .Image }}
    args:
    - "--control-plane-vip"
    - "--cp-vip-address={{ .VIPAddress }}"
    - "--node-name={{ .NodeName }}"
{{- if .Interface }}
    - "--interface={{ .Interface }}"
{{- end }}
    - "--cp-api-port={{ .APIPort }}"
    - "--cp-health-interval={{ .HealthInterval }}"
    - "--cp-health-timeout={{ .HealthTimeout }}"
    securityContext:
      capabilities:
        add:
        - NET_ADMIN
        - NET_RAW
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
      limits:
        cpu: 200m
        memory: 128Mi
    livenessProbe:
      httpGet:
        path: /healthz
        port: 9091
      initialDelaySeconds: 10
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /readyz
        port: 9091
      initialDelaySeconds: 5
      periodSeconds: 5
`

// systemdUnitTemplate is the text/template for generating a systemd unit file.
const systemdUnitTemplate = `[Unit]
Description=NovaEdge Control Plane VIP Manager
Documentation=https://novaedge.io
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{ .BinaryPath }} \
  --control-plane-vip \
  --cp-vip-address={{ .VIPAddress }} \
  --node-name={{ .NodeName }}{{ if .Interface }} \
  --interface={{ .Interface }}{{ end }} \
  --cp-api-port={{ .APIPort }} \
  --cp-health-interval={{ .HealthInterval }} \
  --cp-health-timeout={{ .HealthTimeout }}
Restart=always
RestartSec=5
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
`

// staticPodData holds the template data for static pod generation.
type staticPodData struct {
	Image          string
	VIPAddress     string
	NodeName       string
	Interface      string
	APIPort        int
	HealthInterval string
	HealthTimeout  string
}

// systemdUnitData holds the template data for systemd unit generation.
type systemdUnitData struct {
	BinaryPath     string
	VIPAddress     string
	NodeName       string
	Interface      string
	APIPort        int
	HealthInterval string
	HealthTimeout  string
}

func newGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate deployment manifests for NovaEdge",
		Long:  `Generate static pod manifests and systemd unit files for deploying novaedge-agent in control-plane VIP mode.`,
		// Override PersistentPreRunE to skip kubeconfig loading for generate commands.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}

	cmd.AddCommand(newGenerateStaticPodCommand())
	cmd.AddCommand(newGenerateSystemdUnitCommand())

	return cmd
}

func newGenerateStaticPodCommand() *cobra.Command {
	var (
		vipAddress     string
		image          string
		iface          string
		apiPort        int
		healthInterval string
		healthTimeout  string
		nodeName       string
	)

	cmd := &cobra.Command{
		Use:   "static-pod",
		Short: "Generate a Kubernetes static pod manifest for control-plane VIP mode",
		Long: `Generate a Kubernetes static pod manifest for running the novaedge-agent
in control-plane VIP mode. The manifest is written to stdout so it can be
redirected to a file (e.g., /etc/kubernetes/manifests/novaedge-cpvip.yaml).`,
		Example: `  # Generate a static pod manifest
  novactl generate static-pod \
    --vip-address 10.0.0.100/32 \
    --image ghcr.io/piwi3910/novaedge-agent:latest

  # Generate with all options
  novactl generate static-pod \
    --vip-address 10.0.0.100/32 \
    --image ghcr.io/piwi3910/novaedge-agent:latest \
    --interface eth0 \
    --api-port 6443 \
    --health-interval 2s \
    --health-timeout 5s \
    --node-name cp-node-1`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runGenerateStaticPod(vipAddress, image, iface, apiPort, healthInterval, healthTimeout, nodeName)
		},
	}

	cmd.Flags().StringVar(&vipAddress, "vip-address", "", "VIP address in CIDR notation (e.g., 10.0.0.100/32)")
	cmd.Flags().StringVar(&image, "image", "", "Container image (e.g., ghcr.io/piwi3910/novaedge-agent:latest)")
	cmd.Flags().StringVar(&iface, "interface", "", "Network interface (omit for auto-detect)")
	cmd.Flags().IntVar(&apiPort, "api-port", 6443, "kube-apiserver port")
	cmd.Flags().StringVar(&healthInterval, "health-interval", "1s", "Health check interval")
	cmd.Flags().StringVar(&healthTimeout, "health-timeout", "3s", "Health check timeout")
	cmd.Flags().StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")

	_ = cmd.MarkFlagRequired("vip-address")
	_ = cmd.MarkFlagRequired("image")

	return cmd
}

func newGenerateSystemdUnitCommand() *cobra.Command {
	var (
		vipAddress     string
		binaryPath     string
		iface          string
		apiPort        int
		healthInterval string
		healthTimeout  string
		nodeName       string
	)

	cmd := &cobra.Command{
		Use:   "systemd-unit",
		Short: "Generate a systemd unit file for control-plane VIP mode",
		Long: `Generate a systemd unit file for running the novaedge-agent in control-plane
VIP mode. The unit file is written to stdout so it can be redirected to a file
(e.g., /etc/systemd/system/novaedge-cpvip.service).`,
		Example: `  # Generate a systemd unit file
  novactl generate systemd-unit \
    --vip-address 10.0.0.100/32

  # Generate with custom binary path and all options
  novactl generate systemd-unit \
    --vip-address 10.0.0.100/32 \
    --binary-path /opt/novaedge/bin/novaedge-agent \
    --interface eth0 \
    --api-port 6443 \
    --health-interval 2s \
    --health-timeout 5s \
    --node-name cp-node-1`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runGenerateSystemdUnit(vipAddress, binaryPath, iface, apiPort, healthInterval, healthTimeout, nodeName)
		},
	}

	cmd.Flags().StringVar(&vipAddress, "vip-address", "", "VIP address in CIDR notation (e.g., 10.0.0.100/32)")
	cmd.Flags().StringVar(&binaryPath, "binary-path", "/usr/local/bin/novaedge-agent", "Path to agent binary")
	cmd.Flags().StringVar(&iface, "interface", "", "Network interface (omit for auto-detect)")
	cmd.Flags().IntVar(&apiPort, "api-port", 6443, "kube-apiserver port")
	cmd.Flags().StringVar(&healthInterval, "health-interval", "1s", "Health check interval")
	cmd.Flags().StringVar(&healthTimeout, "health-timeout", "3s", "Health check timeout")
	cmd.Flags().StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")

	_ = cmd.MarkFlagRequired("vip-address")

	return cmd
}

func runGenerateStaticPod(vipAddress, image, iface string, apiPort int, healthInterval, healthTimeout, nodeName string) error {
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to determine hostname: %w", err)
		}
		nodeName = hostname
	}

	data := staticPodData{
		Image:          image,
		VIPAddress:     vipAddress,
		NodeName:       nodeName,
		Interface:      iface,
		APIPort:        apiPort,
		HealthInterval: healthInterval,
		HealthTimeout:  healthTimeout,
	}

	return renderTemplate(staticPodTemplate, "static-pod", data)
}

func runGenerateSystemdUnit(vipAddress, binaryPath, iface string, apiPort int, healthInterval, healthTimeout, nodeName string) error {
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to determine hostname: %w", err)
		}
		nodeName = hostname
	}

	data := systemdUnitData{
		BinaryPath:     binaryPath,
		VIPAddress:     vipAddress,
		NodeName:       nodeName,
		Interface:      iface,
		APIPort:        apiPort,
		HealthInterval: healthInterval,
		HealthTimeout:  healthTimeout,
	}

	return renderTemplate(systemdUnitTemplate, "systemd-unit", data)
}

func renderTemplate(tmplText, name string, data interface{}) error {
	tmpl, err := template.New(name).Parse(tmplText)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	_, err = fmt.Fprint(os.Stdout, buf.String())
	return err
}
