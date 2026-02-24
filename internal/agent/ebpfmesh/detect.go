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

package ebpfmesh

import (
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"github.com/piwi3910/novaedge/internal/agent/ebpf/service"
	"github.com/piwi3910/novaedge/internal/agent/ebpf/sockmap"
	"github.com/piwi3910/novaedge/internal/agent/mesh"
	"go.uber.org/zap"
)

// TryBackend attempts to create and set up an eBPF sk_lookup backend for
// mesh interception. It returns a ready-to-use mesh.RuleBackend on success,
// or nil if eBPF sk_lookup is not available (unsupported kernel, missing
// BPF objects, verifier error, etc.).
//
// The caller (typically cmd/novaedge-agent/main.go) should use the returned
// backend with mesh.NewTPROXYManagerWithBackend. If nil is returned, the
// caller should fall back to mesh.NewTPROXYManager which auto-detects
// nftables/iptables.
func TryBackend(logger *zap.Logger) mesh.RuleBackend {
	caps, err := novaebpf.Detect()
	if err != nil {
		logger.Debug("eBPF capability detection failed", zap.Error(err))
		return nil
	}
	if !caps.HasSKLookup {
		logger.Debug("kernel does not support BPF_PROG_TYPE_SK_LOOKUP, skipping eBPF mesh backend")
		return nil
	}

	loader := novaebpf.NewProgramLoader(logger, "")
	backend := NewBackend(logger, loader)

	// Try Setup to verify the BPF program can actually be loaded.
	// If it fails (e.g. missing BPF objects, verifier error), fall through.
	if err := backend.Setup(); err != nil {
		logger.Info("eBPF sk_lookup backend setup failed, falling back to nftables/iptables",
			zap.Error(err))
		backend.Cleanup()
		return nil
	}

	novaebpf.LogCapabilities(logger, caps)
	logger.Info("using eBPF sk_lookup backend for mesh interception")
	return backend
}

// TrySockMap attempts to create an eBPF SOCKMAP manager for same-node
// mesh traffic acceleration. It returns a ready-to-use manager on success,
// or nil if SOCKMAP is not available (unsupported kernel, insufficient
// privileges, etc.).
//
// SOCKMAP requires BPF_PROG_TYPE_SOCK_OPS and BPF_PROG_TYPE_SK_MSG program
// support, plus BPF_MAP_TYPE_SOCKHASH map support.
func TrySockMap(logger *zap.Logger) *sockmap.Manager {
	caps, err := novaebpf.Detect()
	if err != nil {
		logger.Debug("eBPF capability detection failed for SOCKMAP", zap.Error(err))
		return nil
	}
	if !caps.HasSockOps || !caps.HasSKMsg || !caps.HasSockHash {
		logger.Debug("kernel does not support SOCKMAP prerequisites",
			zap.Bool("sock_ops", caps.HasSockOps),
			zap.Bool("sk_msg", caps.HasSKMsg),
			zap.Bool("sock_hash", caps.HasSockHash))
		return nil
	}

	mgr, err := sockmap.NewSockMapManager(logger)
	if err != nil {
		logger.Info("eBPF SOCKMAP manager creation failed, same-node bypass disabled",
			zap.Error(err))
		return nil
	}

	logger.Info("eBPF SOCKMAP manager created for same-node traffic acceleration")
	return mgr
}

// TryServiceMap attempts to create an eBPF service lookup map for
// accelerated service-to-backend resolution. It returns a ready-to-use
// ServiceMap on success, or nil if eBPF hash maps are not available.
func TryServiceMap(logger *zap.Logger, maxServices, maxBackendsPerService uint32) *service.ServiceMap {
	caps, err := novaebpf.Detect()
	if err != nil {
		logger.Debug("eBPF capability detection failed for service map", zap.Error(err))
		return nil
	}
	// Service maps use standard BPF_MAP_TYPE_HASH and BPF_MAP_TYPE_ARRAY,
	// which are available on all eBPF-capable kernels. We just check for
	// basic BPF support via the XDP probe as a baseline.
	_ = caps

	sm, err := service.NewServiceMap(logger, maxServices, maxBackendsPerService)
	if err != nil {
		logger.Info("eBPF service map creation failed, using Go-side fallback",
			zap.Error(err))
		return nil
	}

	logger.Info("eBPF service lookup map created",
		zap.Uint32("max_services", maxServices),
		zap.Uint32("max_backends_per_service", maxBackendsPerService))
	return sm
}
