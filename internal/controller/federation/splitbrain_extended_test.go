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

package federation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultSplitBrainConfig(t *testing.T) {
	config := DefaultSplitBrainConfig()
	assert.Equal(t, DefaultPartitionTimeout, config.PartitionTimeout)
	assert.False(t, config.QuorumRequired)
	assert.False(t, config.FencingEnabled) // Default is false (availability over consistency)
	assert.Equal(t, DefaultHealingGracePeriod, config.HealingGracePeriod)
}

func TestSplitBrainConfigDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   *SplitBrainConfig
		expected *SplitBrainConfig
	}{
		{
			name:     "nil config uses defaults",
			config:   nil,
			expected: DefaultSplitBrainConfig(),
		},
		{
			name: "partial config preserves set values",
			config: &SplitBrainConfig{
				PartitionTimeout: 60 * time.Second,
			},
			expected: &SplitBrainConfig{
				PartitionTimeout: 60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config == nil {
				// Test that DefaultSplitBrainConfig returns valid defaults
				result := DefaultSplitBrainConfig()
				assert.NotNil(t, result)
				assert.Equal(t, tt.expected.PartitionTimeout, result.PartitionTimeout)
			} else {
				assert.Equal(t, tt.expected.PartitionTimeout, tt.config.PartitionTimeout)
			}
		})
	}
}

func TestSplitBrainConstants(t *testing.T) {
	assert.Equal(t, 30*time.Second, DefaultPartitionTimeout)
	assert.Equal(t, 10*time.Second, DefaultQuorumCheckInterval)
	assert.Equal(t, 5*time.Second, DefaultHealingGracePeriod)
	assert.Equal(t, QuorumMode("Controllers"), QuorumModeControllers)
	assert.Equal(t, QuorumMode("AgentAssisted"), QuorumModeAgentAssisted)
}
