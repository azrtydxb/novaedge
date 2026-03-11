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
{{- if eq .VIPMode "bgp" }}
    - "--cp-vip-mode=bgp"
    - "--cp-bgp-local-as={{ .BGPLocalAS }}"
{{- if .BGPRouterID }}
    - "--cp-bgp-router-id={{ .BGPRouterID }}"
{{- end }}
{{- range .BGPPeers }}
    - "--cp-bgp-peer={{ . }}"
{{- end }}
{{- if .BFDEnabled }}
    - "--cp-bfd-enabled=true"
    - "--cp-bfd-detect-mult={{ .BFDDetectMult }}"
    - "--cp-bfd-tx-interval={{ .BFDTxInterval }}"
    - "--cp-bfd-rx-interval={{ .BFDRxInterval }}"
{{- end }}
{{- end }}
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
  --interface={{ .Interface }}{{ end }}{{ if eq .VIPMode "bgp" }} \
  --cp-vip-mode=bgp \
  --cp-bgp-local-as={{ .BGPLocalAS }}{{ if .BGPRouterID }} \
  --cp-bgp-router-id={{ .BGPRouterID }}{{ end }}{{ range .BGPPeers }} \
  --cp-bgp-peer={{ . }}{{ end }}{{ if .BFDEnabled }} \
  --cp-bfd-enabled=true \
  --cp-bfd-detect-mult={{ .BFDDetectMult }} \
  --cp-bfd-tx-interval={{ .BFDTxInterval }} \
  --cp-bfd-rx-interval={{ .BFDRxInterval }}{{ end }}{{ end }} \
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

// generateData holds the template data for both static pod and systemd unit generation.
type generateData struct {
	// Common fields
	VIPAddress     string
	NodeName       string
	Interface      string
	APIPort        int
	HealthInterval string
	HealthTimeout  string

	// Static pod specific
	Image string

	// Systemd unit specific
	BinaryPath string

	// BGP/BFD fields
	VIPMode       string
	BGPLocalAS    uint
	BGPRouterID   string
	BGPPeers      []string
	BFDEnabled    bool
	BFDDetectMult int
	BFDTxInterval string
	BFDRxInterval string
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

func addBGPFlags(cmd *cobra.Command, vipMode *string, bgpLocalAS *uint, bgpRouterID *string, bgpPeers *[]string, bfdEnabled *bool, bfdDetectMult *int, bfdTxInterval, bfdRxInterval *string) {
	cmd.Flags().StringVar(vipMode, "vip-mode", "l2", "VIP mode: l2 or bgp")
	cmd.Flags().UintVar(bgpLocalAS, "bgp-local-as", 0, "Local BGP AS number (required for bgp mode)")
	cmd.Flags().StringVar(bgpRouterID, "bgp-router-id", "", "BGP router ID")
	cmd.Flags().StringSliceVar(bgpPeers, "bgp-peer", nil, "BGP peer in format IP:AS[:PORT] (repeatable, comma-separated)")
	cmd.Flags().BoolVar(bfdEnabled, "bfd-enabled", false, "Enable BFD")
	cmd.Flags().IntVar(bfdDetectMult, "bfd-detect-mult", 3, "BFD detect multiplier")
	cmd.Flags().StringVar(bfdTxInterval, "bfd-tx-interval", "300ms", "BFD minimum TX interval")
	cmd.Flags().StringVar(bfdRxInterval, "bfd-rx-interval", "300ms", "BFD minimum RX interval")
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

		vipMode       string
		bgpLocalAS    uint
		bgpRouterID   string
		bgpPeers      []string
		bfdEnabled    bool
		bfdDetectMult int
		bfdTxInterval string
		bfdRxInterval string
	)

	cmd := &cobra.Command{
		Use:   "static-pod",
		Short: "Generate a Kubernetes static pod manifest for control-plane VIP mode",
		Long: `Generate a Kubernetes static pod manifest for running the novaedge-agent
in control-plane VIP mode. The manifest is written to stdout so it can be
redirected to a file (e.g., /etc/kubernetes/manifests/novaedge-cpvip.yaml).`,
		Example: `  # Generate a static pod manifest (L2 mode)
  novactl generate static-pod \
    --vip-address 10.0.0.100/32 \
    --image ghcr.io/azrtydxb/novaedge-agent:latest

  # Generate with BGP+BFD mode
  novactl generate static-pod \
    --vip-address 10.0.0.100/32 \
    --image ghcr.io/azrtydxb/novaedge-agent:latest \
    --vip-mode bgp \
    --bgp-local-as 65000 \
    --bgp-peer 10.0.0.2:65000:179,10.0.0.3:65000:179 \
    --bfd-enabled`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runGenerateStaticPod(vipAddress, image, iface, apiPort, healthInterval, healthTimeout, nodeName,
				vipMode, bgpLocalAS, bgpRouterID, bgpPeers, bfdEnabled, bfdDetectMult, bfdTxInterval, bfdRxInterval)
		},
	}

	cmd.Flags().StringVar(&vipAddress, "vip-address", "", "VIP address in CIDR notation (e.g., 10.0.0.100/32)")
	cmd.Flags().StringVar(&image, "image", "", "Container image (e.g., ghcr.io/azrtydxb/novaedge-agent:latest)")
	cmd.Flags().StringVar(&iface, "interface", "", "Network interface (omit for auto-detect)")
	cmd.Flags().IntVar(&apiPort, "api-port", 6443, "kube-apiserver port")
	cmd.Flags().StringVar(&healthInterval, "health-interval", "1s", "Health check interval")
	cmd.Flags().StringVar(&healthTimeout, "health-timeout", "3s", "Health check timeout")
	cmd.Flags().StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")

	addBGPFlags(cmd, &vipMode, &bgpLocalAS, &bgpRouterID, &bgpPeers, &bfdEnabled, &bfdDetectMult, &bfdTxInterval, &bfdRxInterval)

	cobra.CheckErr(cmd.MarkFlagRequired("vip-address"))
	cobra.CheckErr(cmd.MarkFlagRequired("image"))

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

		vipMode       string
		bgpLocalAS    uint
		bgpRouterID   string
		bgpPeers      []string
		bfdEnabled    bool
		bfdDetectMult int
		bfdTxInterval string
		bfdRxInterval string
	)

	cmd := &cobra.Command{
		Use:   "systemd-unit",
		Short: "Generate a systemd unit file for control-plane VIP mode",
		Long: `Generate a systemd unit file for running the novaedge-agent in control-plane
VIP mode. The unit file is written to stdout so it can be redirected to a file
(e.g., /etc/systemd/system/novaedge-cpvip.service).`,
		Example: `  # Generate a systemd unit file (L2 mode)
  novactl generate systemd-unit \
    --vip-address 10.0.0.100/32

  # Generate with BGP+BFD mode
  novactl generate systemd-unit \
    --vip-address 10.0.0.100/32 \
    --vip-mode bgp \
    --bgp-local-as 65000 \
    --bgp-peer 10.0.0.2:65000:179,10.0.0.3:65000:179 \
    --bfd-enabled`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runGenerateSystemdUnit(vipAddress, binaryPath, iface, apiPort, healthInterval, healthTimeout, nodeName,
				vipMode, bgpLocalAS, bgpRouterID, bgpPeers, bfdEnabled, bfdDetectMult, bfdTxInterval, bfdRxInterval)
		},
	}

	cmd.Flags().StringVar(&vipAddress, "vip-address", "", "VIP address in CIDR notation (e.g., 10.0.0.100/32)")
	cmd.Flags().StringVar(&binaryPath, "binary-path", "/usr/local/bin/novaedge-agent", "Path to agent binary")
	cmd.Flags().StringVar(&iface, "interface", "", "Network interface (omit for auto-detect)")
	cmd.Flags().IntVar(&apiPort, "api-port", 6443, "kube-apiserver port")
	cmd.Flags().StringVar(&healthInterval, "health-interval", "1s", "Health check interval")
	cmd.Flags().StringVar(&healthTimeout, "health-timeout", "3s", "Health check timeout")
	cmd.Flags().StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")

	addBGPFlags(cmd, &vipMode, &bgpLocalAS, &bgpRouterID, &bgpPeers, &bfdEnabled, &bfdDetectMult, &bfdTxInterval, &bfdRxInterval)

	cobra.CheckErr(cmd.MarkFlagRequired("vip-address"))

	return cmd
}

func runGenerateStaticPod(vipAddress, image, iface string, apiPort int, healthInterval, healthTimeout, nodeName string,
	vipMode string, bgpLocalAS uint, bgpRouterID string, bgpPeers []string, bfdEnabled bool, bfdDetectMult int, bfdTxInterval, bfdRxInterval string) error {
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to determine hostname: %w", err)
		}
		nodeName = hostname
	}

	data := generateData{
		Image:          image,
		VIPAddress:     vipAddress,
		NodeName:       nodeName,
		Interface:      iface,
		APIPort:        apiPort,
		HealthInterval: healthInterval,
		HealthTimeout:  healthTimeout,
		VIPMode:        vipMode,
		BGPLocalAS:     bgpLocalAS,
		BGPRouterID:    bgpRouterID,
		BGPPeers:       bgpPeers,
		BFDEnabled:     bfdEnabled,
		BFDDetectMult:  bfdDetectMult,
		BFDTxInterval:  bfdTxInterval,
		BFDRxInterval:  bfdRxInterval,
	}

	return renderTemplate(staticPodTemplate, "static-pod", data)
}

func runGenerateSystemdUnit(vipAddress, binaryPath, iface string, apiPort int, healthInterval, healthTimeout, nodeName string,
	vipMode string, bgpLocalAS uint, bgpRouterID string, bgpPeers []string, bfdEnabled bool, bfdDetectMult int, bfdTxInterval, bfdRxInterval string) error {
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to determine hostname: %w", err)
		}
		nodeName = hostname
	}

	data := generateData{
		BinaryPath:     binaryPath,
		VIPAddress:     vipAddress,
		NodeName:       nodeName,
		Interface:      iface,
		APIPort:        apiPort,
		HealthInterval: healthInterval,
		HealthTimeout:  healthTimeout,
		VIPMode:        vipMode,
		BGPLocalAS:     bgpLocalAS,
		BGPRouterID:    bgpRouterID,
		BGPPeers:       bgpPeers,
		BFDEnabled:     bfdEnabled,
		BFDDetectMult:  bfdDetectMult,
		BFDTxInterval:  bfdTxInterval,
		BFDRxInterval:  bfdRxInterval,
	}

	return renderTemplate(systemdUnitTemplate, "systemd-unit", data)
}

func renderTemplate(tmplText, name string, data any) error {
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
