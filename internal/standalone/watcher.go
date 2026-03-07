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

package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	agentconfig "github.com/azrtydxb/novaedge/internal/agent/config"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errConfigFileDoesNotExist = errors.New("config file does not exist")
)

// ConfigWatcher watches a config file for changes and provides ConfigSnapshots
type ConfigWatcher struct {
	configPath   string
	nodeName     string
	logger       *zap.Logger
	converter    *Converter
	mu           sync.RWMutex
	lastHash     string
	lastConfig   *Config
	lastSnapshot *pb.ConfigSnapshot
}

// NewConfigWatcher creates a new config file watcher
func NewConfigWatcher(configPath, nodeName string, logger *zap.Logger) (*ConfigWatcher, error) {
	// Resolve absolute path
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", errConfigFileDoesNotExist, absPath)
	}

	return &ConfigWatcher{
		configPath: absPath,
		nodeName:   nodeName,
		logger:     logger,
		converter:  NewConverter(),
	}, nil
}

// ApplyFunc is called when configuration changes
type ApplyFunc func(*agentconfig.Snapshot) error

// Start begins watching the config file and calls applyFunc on changes
func (w *ConfigWatcher) Start(ctx context.Context, applyFunc ApplyFunc) error {
	// Load initial config
	if err := w.loadAndApply(applyFunc); err != nil {
		return fmt.Errorf("failed to load initial config: %w", err)
	}

	// Create file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	// Watch the directory containing the config file
	// (watching the file directly doesn't work well with editors that replace files)
	configDir := filepath.Dir(w.configPath)
	if err := watcher.Add(configDir); err != nil {
		return fmt.Errorf("failed to watch config directory: %w", err)
	}

	w.logger.Info("Watching config file for changes",
		zap.String("path", w.configPath),
		zap.String("directory", configDir))

	// Also do periodic polling as a fallback
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	configFileName := filepath.Base(w.configPath)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Config watcher stopped")
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Only process events for our config file
			if filepath.Base(event.Name) != configFileName {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.logger.Info("Config file changed, reloading",
					zap.String("event", event.Op.String()))

				// Small delay to ensure file is fully written
				time.Sleep(100 * time.Millisecond)

				if err := w.loadAndApply(applyFunc); err != nil {
					w.logger.Error("Failed to reload config", zap.Error(err))
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Error("File watcher error", zap.Error(err))

		case <-ticker.C:
			// Periodic check for changes (fallback)
			if changed, _ := w.hasChanged(); changed {
				w.logger.Info("Config file changed (detected by polling), reloading")
				if err := w.loadAndApply(applyFunc); err != nil {
					w.logger.Error("Failed to reload config", zap.Error(err))
				}
			}
		}
	}
}

// loadAndApply loads the config file and applies it
func (w *ConfigWatcher) loadAndApply(applyFunc ApplyFunc) error {
	standaloneConfig, err := LoadConfig(w.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	snapshot, err := w.converter.ToSnapshot(standaloneConfig, w.nodeName)
	if err != nil {
		return fmt.Errorf("failed to convert config: %w", err)
	}

	// Update stored state
	w.mu.Lock()
	w.lastConfig = standaloneConfig
	w.lastSnapshot = snapshot
	hash, _ := w.computeHash()
	w.lastHash = hash
	w.mu.Unlock()

	w.logger.Info("Applying new configuration",
		zap.String("version", snapshot.Version),
		zap.Int("listeners", len(standaloneConfig.Listeners)),
		zap.Int("routes", len(standaloneConfig.Routes)),
		zap.Int("backends", len(standaloneConfig.Backends)),
		zap.Int("vips", len(standaloneConfig.VIPs)),
		zap.Int("policies", len(standaloneConfig.Policies)))

	// Wrap in agentconfig.Snapshot for compatibility with agent components
	wrapped := &agentconfig.Snapshot{ConfigSnapshot: snapshot}
	return applyFunc(wrapped)
}

// hasChanged checks if the config file has changed
func (w *ConfigWatcher) hasChanged() (bool, error) {
	hash, err := w.computeHash()
	if err != nil {
		return false, err
	}

	w.mu.RLock()
	lastHash := w.lastHash
	w.mu.RUnlock()

	return hash != lastHash, nil
}

// computeHash computes SHA256 hash of the config file
func (w *ConfigWatcher) computeHash() (string, error) {
	data, err := os.ReadFile(w.configPath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// GetCurrentConfig returns the current configuration
func (w *ConfigWatcher) GetCurrentConfig() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastConfig
}

// GetCurrentSnapshot returns the current ConfigSnapshot
func (w *ConfigWatcher) GetCurrentSnapshot() *pb.ConfigSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastSnapshot
}
