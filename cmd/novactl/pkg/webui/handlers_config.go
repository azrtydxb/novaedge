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

package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	namespaceAll = "all"
)

// SnapshotStore stores configuration snapshots in memory.
type SnapshotStore struct {
	snapshots []ConfigSnapshot
	mu        sync.RWMutex
	maxSize   int
}

// ConfigSnapshot represents a point-in-time configuration snapshot.
type ConfigSnapshot struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Config    string    `json:"config"`
	Comment   string    `json:"comment"`
}

// NewSnapshotStore creates a new SnapshotStore with the given maximum capacity.
func NewSnapshotStore(maxSize int) *SnapshotStore {
	if maxSize <= 0 {
		maxSize = 50
	}
	return &SnapshotStore{
		snapshots: []ConfigSnapshot{},
		maxSize:   maxSize,
	}
}

// generateID generates a random hex ID for snapshots.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("snap-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Save stores a new configuration snapshot and returns it.
func (ss *SnapshotStore) Save(config, comment string) *ConfigSnapshot {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	snapshot := ConfigSnapshot{
		ID:        generateID(),
		Timestamp: time.Now(),
		Config:    config,
		Comment:   comment,
	}

	ss.snapshots = append(ss.snapshots, snapshot)

	// Trim to maxSize, keeping the most recent entries
	if len(ss.snapshots) > ss.maxSize {
		ss.snapshots = ss.snapshots[len(ss.snapshots)-ss.maxSize:]
	}

	return &snapshot
}

// List returns all stored snapshots (ordered oldest to newest).
func (ss *SnapshotStore) List() []ConfigSnapshot {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	result := make([]ConfigSnapshot, len(ss.snapshots))
	copy(result, ss.snapshots)
	return result
}

// Get returns a snapshot by ID, or nil if not found.
func (ss *SnapshotStore) Get(id string) *ConfigSnapshot {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	for i := range ss.snapshots {
		if ss.snapshots[i].ID == id {
			snap := ss.snapshots[i]
			return &snap
		}
	}
	return nil
}

// snapshotRequest represents the JSON body for creating a snapshot.
type snapshotRequest struct {
	Comment   string `json:"comment"`
	Namespace string `json:"namespace"`
}

// handleConfigSnapshots handles GET (list) and POST (create) for /api/v1/config/snapshots
func (s *Server) handleConfigSnapshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		snapshots := s.snapshotStore.List()
		writeJSON(w, http.StatusOK, snapshots)

	case http.MethodPost:
		if s.backend == nil {
			writeError(w, http.StatusServiceUnavailable, "backend not initialized")
			return
		}

		var req snapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Allow empty body; default to "all" namespace and empty comment
			req.Namespace = namespaceAll
		}
		if req.Namespace == "" {
			req.Namespace = namespaceAll
		}

		ctx := r.Context()
		data, err := s.backend.ExportConfig(ctx, req.Namespace)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to export config: %v", err))
			return
		}

		snapshot := s.snapshotStore.Save(string(data), req.Comment)
		writeJSON(w, http.StatusCreated, snapshot)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleConfigSnapshot handles GET /api/v1/config/snapshots/{id}
func (s *Server) handleConfigSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/config/snapshots/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "snapshot ID is required")
		return
	}

	snapshot := s.snapshotStore.Get(id)
	if snapshot == nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

// ConfigDiffResponse holds two snapshot configs for client-side diffing.
type ConfigDiffResponse struct {
	From ConfigSnapshot `json:"from"`
	To   ConfigSnapshot `json:"to"`
}

// handleConfigDiff handles GET /api/v1/config/diff?from={id}&to={id}
func (s *Server) handleConfigDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	fromID := q.Get("from")
	toID := q.Get("to")

	if fromID == "" || toID == "" {
		writeError(w, http.StatusBadRequest, "both 'from' and 'to' query parameters are required")
		return
	}

	fromSnap := s.snapshotStore.Get(fromID)
	if fromSnap == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("snapshot %q not found", fromID))
		return
	}

	toSnap := s.snapshotStore.Get(toID)
	if toSnap == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("snapshot %q not found", toID))
		return
	}

	writeJSON(w, http.StatusOK, ConfigDiffResponse{
		From: *fromSnap,
		To:   *toSnap,
	})
}

// handleConfigRollback handles POST /api/v1/config/rollback/{id}
func (s *Server) handleConfigRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/config/rollback/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "snapshot ID is required")
		return
	}

	snapshot := s.snapshotStore.Get(id)
	if snapshot == nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}

	ctx := r.Context()
	result, err := s.backend.ImportConfig(ctx, []byte(snapshot.Config), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to rollback config: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}
