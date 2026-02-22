//go:build linux

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
	"fmt"
	"os"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/features"
	"go.uber.org/zap"
)

// Capabilities describes the eBPF features available on the running kernel.
type Capabilities struct {
	// HasXDP indicates support for BPF_PROG_TYPE_XDP programs.
	HasXDP bool
	// HasSKLookup indicates support for BPF_PROG_TYPE_SK_LOOKUP programs
	// used for transparent service mesh redirection.
	HasSKLookup bool
	// HasAFXDP indicates support for AF_XDP (XSK) sockets for zero-copy
	// packet I/O.
	HasAFXDP bool
	// HasBTF indicates that the kernel exposes BTF type information,
	// enabling CO-RE (Compile Once – Run Everywhere) BPF programs.
	HasBTF bool
	// HasLPMTrie indicates support for BPF_MAP_TYPE_LPM_TRIE maps used
	// for CIDR-based lookups.
	HasLPMTrie bool
	// KernelVersion is the running kernel version string from /proc/version.
	KernelVersion string
}

// Detect probes the running kernel for eBPF capabilities and returns the
// result. Individual feature probes that fail are reported as unsupported
// rather than causing an overall error; only fundamental failures (e.g.
// unable to read kernel version) produce an error.
func Detect() (*Capabilities, error) {
	caps := &Capabilities{}

	kv, err := readKernelVersion()
	if err != nil {
		return nil, fmt.Errorf("reading kernel version: %w", err)
	}
	caps.KernelVersion = kv

	// Probe program types.
	caps.HasXDP = features.HaveProgramType(ebpf.XDP) == nil
	caps.HasSKLookup = features.HaveProgramType(ebpf.SkLookup) == nil

	// Probe map types.
	caps.HasLPMTrie = features.HaveMapType(ebpf.LPMTrie) == nil

	// AF_XDP requires XDP + XskMap support.
	caps.HasAFXDP = caps.HasXDP && features.HaveMapType(ebpf.XSKMap) == nil

	// BTF support check.
	if _, err := btfEnabled(); err == nil {
		caps.HasBTF = true
	}

	return caps, nil
}

// LogCapabilities writes a summary of detected eBPF capabilities at Info
// level so operators can verify what acceleration features are available.
func LogCapabilities(logger *zap.Logger, caps *Capabilities) {
	logger.Info("eBPF capability detection complete",
		zap.String("kernel", caps.KernelVersion),
		zap.Bool("xdp", caps.HasXDP),
		zap.Bool("sk_lookup", caps.HasSKLookup),
		zap.Bool("af_xdp", caps.HasAFXDP),
		zap.Bool("btf", caps.HasBTF),
		zap.Bool("lpm_trie", caps.HasLPMTrie),
	)
}

// readKernelVersion returns the kernel version string from /proc/version.
func readKernelVersion() (string, error) {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// btfEnabled checks if the kernel exposes BTF information by looking for
// the vmlinux BTF file.
func btfEnabled() (bool, error) {
	_, err := os.Stat("/sys/kernel/btf/vmlinux")
	if err != nil {
		return false, err
	}
	return true, nil
}
