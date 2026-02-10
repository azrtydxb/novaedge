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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// ConfigCacheFile is the name of the cached config file
	ConfigCacheFile = "config-snapshot.json"

	// MetadataFile is the name of the metadata file
	MetadataFile = "metadata.json"

	// VectorClockFile stores the vector clock state
	VectorClockFile = "vectorclock.json"

	// ChangeQueueFile stores pending local changes for sync
	ChangeQueueFile = "change-queue.json"

	// MaxChangeQueueSize is the maximum number of changes to queue
	MaxChangeQueueSize = 1000
)

// ConfigPersistence handles saving and loading config for autonomous mode
type ConfigPersistence struct {
	basePath string
	logger   *zap.Logger
	mu       sync.RWMutex

	// Cached data
	metadata    *PersistenceMetadata
	vectorClock map[string]int64
	changeQueue []*LocalChange
}

// PersistenceMetadata contains metadata about the persisted config
type PersistenceMetadata struct {
	// Version of the persisted config
	Version string `json:"version"`

	// ControllerName that provided the config
	ControllerName string `json:"controllerName"`

	// FederationID the config belongs to
	FederationID string `json:"federationId"`

	// PersistTime is when the config was persisted
	PersistTime time.Time `json:"persistTime"`

	// ContentHash for integrity checking
	ContentHash string `json:"contentHash"`

	// SequenceNumber from the federation metadata
	SequenceNumber int64 `json:"sequenceNumber"`

	// ResourceCounts for quick inspection
	ResourceCounts map[string]int `json:"resourceCounts"`
}

// LocalChange represents a change made in autonomous mode
type LocalChange struct {
	// ID is a unique identifier
	ID string `json:"id"`

	// Timestamp of the change
	Timestamp time.Time `json:"timestamp"`

	// Type of change
	Type string `json:"type"`

	// ResourceType (ProxyGateway, ProxyRoute, etc.)
	ResourceType string `json:"resourceType"`

	// Namespace of the resource
	Namespace string `json:"namespace"`

	// Name of the resource
	Name string `json:"name"`

	// Data is the serialized resource
	Data []byte `json:"data"`
}

// NewConfigPersistence creates a new config persistence handler
func NewConfigPersistence(basePath string, logger *zap.Logger) (*ConfigPersistence, error) {
	basePath = filepath.Clean(basePath)
	if err := os.MkdirAll(basePath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create persistence directory: %w", err)
	}

	cp := &ConfigPersistence{
		basePath:    basePath,
		logger:      logger.Named("persistence"),
		changeQueue: make([]*LocalChange, 0),
	}

	// Load existing metadata if available
	if err := cp.loadMetadata(); err != nil {
		cp.logger.Debug("No existing metadata found", zap.Error(err))
	}

	// Load vector clock
	if err := cp.loadVectorClock(); err != nil {
		cp.logger.Debug("No existing vector clock found", zap.Error(err))
		cp.vectorClock = make(map[string]int64)
	}

	// Load change queue
	if err := cp.loadChangeQueue(); err != nil {
		cp.logger.Debug("No existing change queue found", zap.Error(err))
	}

	return cp, nil
}

// SaveSnapshot persists a config snapshot
func (cp *ConfigPersistence) SaveSnapshot(snapshot *pb.ConfigSnapshot) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Serialize snapshot using protojson for readability
	data, err := protojson.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Calculate hash
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Write snapshot file
	snapshotPath := filepath.Join(cp.basePath, ConfigCacheFile)
	if err := os.WriteFile(snapshotPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Create metadata
	cp.metadata = &PersistenceMetadata{
		Version:     snapshot.Version,
		PersistTime: time.Now(),
		ContentHash: hashStr,
		ResourceCounts: map[string]int{
			"gateways": len(snapshot.Gateways),
			"routes":   len(snapshot.Routes),
			"clusters": len(snapshot.Clusters),
			"policies": len(snapshot.Policies),
			"vips":     len(snapshot.VipAssignments),
		},
	}

	// Extract federation metadata
	if snapshot.FederationMetadata != nil {
		cp.metadata.ControllerName = snapshot.FederationMetadata.OriginController
		cp.metadata.FederationID = snapshot.FederationMetadata.FederationId
		cp.metadata.SequenceNumber = snapshot.FederationMetadata.SequenceNumber
		cp.vectorClock = snapshot.FederationMetadata.VectorClock
	}

	// Save metadata
	if err := cp.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Save vector clock
	if err := cp.saveVectorClock(); err != nil {
		return fmt.Errorf("failed to save vector clock: %w", err)
	}

	cp.logger.Info("Persisted config snapshot",
		zap.String("version", snapshot.Version),
		zap.String("hash", hashStr[:16]),
		zap.Int("gateways", len(snapshot.Gateways)),
	)

	return nil
}

// LoadSnapshot loads a persisted config snapshot
func (cp *ConfigPersistence) LoadSnapshot() (*pb.ConfigSnapshot, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	snapshotPath := filepath.Join(cp.basePath, ConfigCacheFile)
	data, err := os.ReadFile(filepath.Clean(snapshotPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}

	// Verify hash if metadata exists
	if cp.metadata != nil {
		hash := sha256.Sum256(data)
		hashStr := hex.EncodeToString(hash[:])
		if hashStr != cp.metadata.ContentHash {
			return nil, fmt.Errorf("snapshot hash mismatch")
		}
	}

	snapshot := &pb.ConfigSnapshot{}
	if err := protojson.Unmarshal(data, snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	cp.logger.Info("Loaded persisted config snapshot",
		zap.String("version", snapshot.Version),
		zap.Int("gateways", len(snapshot.Gateways)),
	)

	return snapshot, nil
}

// GetMetadata returns the persistence metadata
func (cp *ConfigPersistence) GetMetadata() *PersistenceMetadata {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.metadata
}

// GetVectorClock returns the persisted vector clock
func (cp *ConfigPersistence) GetVectorClock() map[string]int64 {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	result := make(map[string]int64)
	for k, v := range cp.vectorClock {
		result[k] = v
	}
	return result
}

// QueueLocalChange queues a change made in autonomous mode
func (cp *ConfigPersistence) QueueLocalChange(change *LocalChange) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if len(cp.changeQueue) >= MaxChangeQueueSize {
		// Remove oldest change
		cp.changeQueue = cp.changeQueue[1:]
	}

	cp.changeQueue = append(cp.changeQueue, change)

	return cp.saveChangeQueue()
}

// GetPendingChanges returns queued changes for sync
func (cp *ConfigPersistence) GetPendingChanges() []*LocalChange {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	result := make([]*LocalChange, len(cp.changeQueue))
	copy(result, cp.changeQueue)
	return result
}

// ClearPendingChanges removes synced changes from the queue
func (cp *ConfigPersistence) ClearPendingChanges(ids []string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	var remaining []*LocalChange
	for _, change := range cp.changeQueue {
		if !idSet[change.ID] {
			remaining = append(remaining, change)
		}
	}

	cp.changeQueue = remaining
	return cp.saveChangeQueue()
}

// HasCachedConfig returns true if there's a cached config
func (cp *ConfigPersistence) HasCachedConfig() bool {
	snapshotPath := filepath.Join(cp.basePath, ConfigCacheFile)
	_, err := os.Stat(snapshotPath)
	return err == nil
}

// ConfigAge returns how old the cached config is
func (cp *ConfigPersistence) ConfigAge() time.Duration {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	if cp.metadata == nil {
		return 0
	}
	return time.Since(cp.metadata.PersistTime)
}

// saveMetadata saves metadata to disk
func (cp *ConfigPersistence) saveMetadata() error {
	data, err := json.MarshalIndent(cp.metadata, "", "  ")
	if err != nil {
		return err
	}

	metadataPath := filepath.Join(cp.basePath, MetadataFile)
	return os.WriteFile(metadataPath, data, 0600)
}

// loadMetadata loads metadata from disk
func (cp *ConfigPersistence) loadMetadata() error {
	metadataPath := filepath.Join(cp.basePath, MetadataFile)
	data, err := os.ReadFile(filepath.Clean(metadataPath))
	if err != nil {
		return err
	}

	cp.metadata = &PersistenceMetadata{}
	return json.Unmarshal(data, cp.metadata)
}

// saveVectorClock saves vector clock to disk
func (cp *ConfigPersistence) saveVectorClock() error {
	data, err := json.MarshalIndent(cp.vectorClock, "", "  ")
	if err != nil {
		return err
	}

	vcPath := filepath.Join(cp.basePath, VectorClockFile)
	return os.WriteFile(vcPath, data, 0600)
}

// loadVectorClock loads vector clock from disk
func (cp *ConfigPersistence) loadVectorClock() error {
	vcPath := filepath.Join(cp.basePath, VectorClockFile)
	data, err := os.ReadFile(filepath.Clean(vcPath))
	if err != nil {
		return err
	}

	cp.vectorClock = make(map[string]int64)
	return json.Unmarshal(data, &cp.vectorClock)
}

// saveChangeQueue saves change queue to disk
func (cp *ConfigPersistence) saveChangeQueue() error {
	data, err := json.MarshalIndent(cp.changeQueue, "", "  ")
	if err != nil {
		return err
	}

	queuePath := filepath.Join(cp.basePath, ChangeQueueFile)
	return os.WriteFile(queuePath, data, 0600)
}

// loadChangeQueue loads change queue from disk
func (cp *ConfigPersistence) loadChangeQueue() error {
	queuePath := filepath.Join(cp.basePath, ChangeQueueFile)
	data, err := os.ReadFile(filepath.Clean(queuePath))
	if err != nil {
		return err
	}

	cp.changeQueue = make([]*LocalChange, 0)
	return json.Unmarshal(data, &cp.changeQueue)
}

// Cleanup removes all persisted data
func (cp *ConfigPersistence) Cleanup() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	files := []string{ConfigCacheFile, MetadataFile, VectorClockFile, ChangeQueueFile}
	for _, file := range files {
		path := filepath.Join(cp.basePath, file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	cp.metadata = nil
	cp.vectorClock = make(map[string]int64)
	cp.changeQueue = nil

	return nil
}
