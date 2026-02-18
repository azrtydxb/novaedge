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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	agentconfig "github.com/piwi3910/novaedge/internal/agent/config"
)

func TestNewConfigWatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("valid config file", func(t *testing.T) {
		// Create a temporary config file
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(configPath, []byte("version: v1\n"), 0600)
		require.NoError(t, err)

		watcher, err := NewConfigWatcher(configPath, "test-node", logger)
		require.NoError(t, err)
		assert.NotNil(t, watcher)
		assert.Equal(t, "test-node", watcher.nodeName)
		assert.NotNil(t, watcher.converter)
	})

	t.Run("non-existent file", func(t *testing.T) {
		watcher, err := NewConfigWatcher("/non/existent/path.yaml", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, watcher)
		assert.Contains(t, err.Error(), "config file does not exist")
	})

	t.Run("relative path is resolved", func(t *testing.T) {
		// Create a temporary config file
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(configPath, []byte("version: v1\n"), 0600)
		require.NoError(t, err)

		// Change to the temp directory
		oldDir, _ := os.Getwd()
		defer func() { _ = os.Chdir(oldDir) }()
		_ = os.Chdir(tmpDir)

		watcher, err := NewConfigWatcher("config.yaml", "test-node", logger)
		require.NoError(t, err)
		assert.NotNil(t, watcher)
		assert.True(t, strings.HasPrefix(watcher.configPath, "/") || strings.Contains(watcher.configPath, tmpDir))
	})
}

func TestConfigWatcher_ApplyFunc(t *testing.T) {
	// Test that ApplyFunc type is correctly defined
	var applyFunc ApplyFunc = func(snap *agentconfig.Snapshot) error {
		return nil
	}
	assert.NotNil(t, applyFunc)
}
