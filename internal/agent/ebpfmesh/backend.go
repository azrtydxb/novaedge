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
	"fmt"
	"os"
	"time"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"github.com/azrtydxb/novaedge/internal/agent/mesh"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"go.uber.org/zap"
)

const subsystem = "mesh"

// Backend implements mesh.RuleBackend using BPF_PROG_TYPE_SK_LOOKUP to
// redirect mesh-intercepted connections to the TPROXY listener. This
// eliminates the need for nftables/iptables NAT REDIRECT rules.
//
// The BPF program uses a SOCKMAP to hold the listener socket reference.
// After Setup(), call SetListenerFD() to register the TPROXY listener
// socket so the BPF program can redirect connections to it.
type Backend struct {
	logger      *zap.Logger
	loader      *novaebpf.ProgramLoader
	servicesMap *ebpf.Map
	socketMap   *ebpf.Map // SOCKMAP holding the TPROXY listener socket
	prog        *ebpf.Program
	netlinkLink *link.NetNsLink
}

// NewBackend creates an eBPF mesh redirect backend. The loader is used for
// lifecycle management and metrics.
func NewBackend(logger *zap.Logger, loader *novaebpf.ProgramLoader) *Backend {
	return &Backend{
		logger: logger.With(zap.String("component", "ebpf-mesh")),
		loader: loader,
	}
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "ebpf-sk-lookup"
}

// Setup loads the BPF sk_lookup program and attaches it to the network
// namespace. After Setup returns successfully, the program will intercept
// socket lookups for connections matching entries in the mesh_services map.
//
// After Setup(), the caller must call SetListenerFD() with the TPROXY
// listener's file descriptor to enable connection redirection.
func (b *Backend) Setup() error {
	start := time.Now()

	// Load the BPF collection from the embedded ELF.
	spec, err := loadMeshRedirect()
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("loading mesh redirect BPF spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("creating mesh redirect BPF collection: %w", err)
	}

	prog := coll.Programs["mesh_redirect_prog"]
	if prog == nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("mesh_redirect_prog not found in BPF collection")
	}

	svcMap := coll.Maps["mesh_services"]
	if svcMap == nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("mesh_services map not found in BPF collection")
	}

	sockMap := coll.Maps["tproxy_socket"]
	if sockMap == nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("tproxy_socket SOCKMAP not found in BPF collection")
	}

	// Attach the sk_lookup program to the current network namespace.
	// Open the current netns to get a file descriptor.
	nsFile, err := os.Open("/proc/self/ns/net")
	if err != nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("opening current network namespace: %w", err)
	}
	nsFD := int(nsFile.Fd())

	netlinkLink, err := link.AttachNetNs(nsFD, prog)
	nsFile.Close()
	if err != nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("attaching sk_lookup program to netns: %w", err)
	}

	b.prog = prog
	b.servicesMap = svcMap
	b.socketMap = sockMap
	b.netlinkLink = netlinkLink

	novaebpf.RecordProgramLoaded(subsystem)
	novaebpf.ObserveAttachDuration(subsystem, time.Since(start).Seconds())
	b.logger.Info("eBPF sk_lookup program attached for mesh interception")

	return nil
}

// SetListenerFD registers the TPROXY listener socket in the BPF SOCKMAP.
// This must be called after Setup() and after the listener socket is created.
// The fd should be the file descriptor of the TCP listener accepting
// mesh-intercepted connections.
func (b *Backend) SetListenerFD(fd int) error {
	if b.socketMap == nil {
		return fmt.Errorf("eBPF mesh backend not set up")
	}

	key := uint32(0)
	val := uint64(fd)
	if err := b.socketMap.Update(key, val, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("tproxy_socket", "update", "error")
		return fmt.Errorf("registering listener socket in SOCKMAP: %w", err)
	}

	novaebpf.RecordMapOp("tproxy_socket", "update", "ok")
	b.logger.Info("TPROXY listener socket registered in BPF SOCKMAP", zap.Int("fd", fd))
	return nil
}

// ApplyRules reconciles the BPF mesh_services map to match the desired set
// of intercept targets. Entries not in the desired set are removed; new
// entries are added. The tproxyPort is stored as the redirect target for
// all entries.
func (b *Backend) ApplyRules(targets []mesh.InterceptTarget, tproxyPort int32) error {
	if b.servicesMap == nil {
		return fmt.Errorf("eBPF mesh backend not set up")
	}

	// Build desired map state.
	desired := make(map[meshSvcKey]meshSvcValue, len(targets))
	for _, t := range targets {
		key, err := makeServiceKey(t.ClusterIP, t.Port)
		if err != nil {
			b.logger.Warn("skipping invalid intercept target",
				zap.String("ip", t.ClusterIP),
				zap.Int32("port", t.Port),
				zap.Error(err))
			continue
		}
		desired[key] = meshSvcValue{
			RedirectPort: uint32(tproxyPort),
		}
	}

	// Collect existing keys.
	var existingKey meshSvcKey
	var existingVal meshSvcValue
	toDelete := make([]meshSvcKey, 0)
	iter := b.servicesMap.Iterate()
	for iter.Next(&existingKey, &existingVal) {
		if _, ok := desired[existingKey]; !ok {
			keyToDelete := existingKey
			toDelete = append(toDelete, keyToDelete)
		}
	}

	// Delete stale entries.
	for _, k := range toDelete {
		if err := b.servicesMap.Delete(k); err != nil {
			novaebpf.RecordMapOp("mesh_services", "delete", "error")
			b.logger.Warn("failed to delete stale mesh service entry", zap.Error(err))
		} else {
			novaebpf.RecordMapOp("mesh_services", "delete", "ok")
		}
	}

	// Upsert desired entries.
	for k, v := range desired {
		if err := b.servicesMap.Update(k, v, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("mesh_services", "update", "error")
			return fmt.Errorf("updating mesh_services map: %w", err)
		}
		novaebpf.RecordMapOp("mesh_services", "update", "ok")
	}

	b.logger.Info("eBPF mesh services map reconciled",
		zap.Int("active", len(desired)),
		zap.Int("deleted", len(toDelete)))

	return nil
}

// Cleanup detaches the BPF program and closes all resources.
func (b *Backend) Cleanup() error {
	if b.netlinkLink != nil {
		if err := b.netlinkLink.Close(); err != nil {
			b.logger.Warn("failed to close sk_lookup link", zap.Error(err))
		}
		b.netlinkLink = nil
	}
	if b.prog != nil {
		b.prog.Close()
		b.prog = nil
		novaebpf.RecordProgramUnloaded(subsystem)
	}
	if b.servicesMap != nil {
		b.servicesMap.Close()
		b.servicesMap = nil
	}
	if b.socketMap != nil {
		b.socketMap.Close()
		b.socketMap = nil
	}
	b.logger.Info("eBPF mesh backend cleaned up")
	return nil
}
