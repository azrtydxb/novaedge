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

// Package config handles configuration snapshot management, persistence,
// failover, and file-watching for the NovaEdge node agent.
package config

import (
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"

	snapshotpkg "github.com/azrtydxb/novaedge/internal/pkg/snapshot"
)

// SnapshotExtensions is an alias for the shared type in internal/pkg/snapshot.
// This preserves backward compatibility for existing agent code.
type SnapshotExtensions = snapshotpkg.Extensions

// GetListenerExtensions returns extensions for the specified gateway and listener.
// Returns nil if no extensions are configured.
func (s *Snapshot) GetListenerExtensions(gatewayKey, listenerName string) *pb.ListenerExtensions {
	if s.Extensions == nil || s.Extensions.ListenerExtensions == nil {
		return nil
	}
	key := gatewayKey + "/" + listenerName
	ext, ok := s.Extensions.ListenerExtensions[key]
	if !ok {
		return nil
	}
	return ext
}

// GetClusterExtensions returns extensions for the specified cluster.
// Returns nil if no extensions are configured.
func (s *Snapshot) GetClusterExtensions(clusterKey string) *pb.ClusterExtensions {
	if s.Extensions == nil || s.Extensions.ClusterExtensions == nil {
		return nil
	}
	ext, ok := s.Extensions.ClusterExtensions[clusterKey]
	if !ok {
		return nil
	}
	return ext
}
