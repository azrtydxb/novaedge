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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultFailoverConfig(t *testing.T) {
	config := DefaultFailoverConfig()
	assert.NotNil(t, config)
	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.Equal(t, 10*time.Second, config.HealthCheckInterval)
	assert.Equal(t, int32(3), config.FailureThreshold)
	assert.Equal(t, 60*time.Second, config.RecoveryDelay)
	assert.True(t, config.LatencyAware)
	assert.True(t, config.AutonomousModeEnabled)
	assert.Equal(t, "/var/lib/novaedge/config-cache", config.ConfigPersistPath)
}

func TestFailoverConfig_Defaults(t *testing.T) {
	// Test that defaults are sensible
	config := DefaultFailoverConfig()

	// Timeout should be reasonable for network operations
	assert.GreaterOrEqual(t, config.Timeout.Milliseconds(), int64(5000), "Timeout should be at least 5 seconds")
	assert.LessOrEqual(t, config.Timeout.Milliseconds(), int64(60000), "Timeout should be at most 60 seconds")

	// Health check interval should be less than timeout
	assert.Less(t, config.HealthCheckInterval.Milliseconds(), config.Timeout.Milliseconds(),
		"Health check interval should be less than timeout")

	// Failure threshold should be at least 1
	assert.GreaterOrEqual(t, config.FailureThreshold, int32(1), "Failure threshold should be at least 1")

	// Recovery delay should allow time for system stabilization
	assert.GreaterOrEqual(t, config.RecoveryDelay.Milliseconds(), int64(30000),
		"Recovery delay should be at least 30 seconds")
}
