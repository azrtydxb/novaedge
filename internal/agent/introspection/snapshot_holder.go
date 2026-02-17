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

package introspection

import (
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// SnapshotHolder stores the latest ConfigSnapshot for introspection queries.
// It is safe for concurrent use.
type SnapshotHolder struct {
	snapshot atomic.Pointer[pb.ConfigSnapshot]
}

// NewSnapshotHolder creates a new SnapshotHolder.
func NewSnapshotHolder() *SnapshotHolder {
	return &SnapshotHolder{}
}

// Store stores the latest config snapshot.
func (h *SnapshotHolder) Store(snap *pb.ConfigSnapshot) {
	h.snapshot.Store(snap)
}

// GetCurrentSnapshot returns the latest stored config snapshot.
func (h *SnapshotHolder) GetCurrentSnapshot() *pb.ConfigSnapshot {
	return h.snapshot.Load()
}
