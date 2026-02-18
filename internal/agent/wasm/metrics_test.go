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

package wasm

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordPluginExecution(t *testing.T) {
	// Reset the metrics before testing
	wasmPluginExecutionDuration.Reset()
	wasmPluginExecutionTotal.Reset()
	wasmPluginErrorsTotal.Reset()

	t.Run("successful execution", func(t *testing.T) {
		// Reset metrics
		wasmPluginExecutionTotal.Reset()
		wasmPluginErrorsTotal.Reset()

		RecordPluginExecution("test-plugin", "request", 100*time.Millisecond, nil)

		// Verify execution total counter was incremented
		count := testutil.ToFloat64(wasmPluginExecutionTotal.WithLabelValues("test-plugin", "request", "success"))
		assert.Equal(t, float64(1), count)
	})

	t.Run("execution with error", func(t *testing.T) {
		// Reset metrics
		wasmPluginExecutionTotal.Reset()
		wasmPluginErrorsTotal.Reset()

		testErr := errors.New("test error")
		RecordPluginExecution("error-plugin", "response", 50*time.Millisecond, testErr)

		// Verify execution total counter was incremented with error status
		count := testutil.ToFloat64(wasmPluginExecutionTotal.WithLabelValues("error-plugin", "response", "error"))
		assert.Equal(t, float64(1), count)

		// Verify error counter was incremented
		errorCount := testutil.ToFloat64(wasmPluginErrorsTotal.WithLabelValues("error-plugin", "response"))
		assert.Equal(t, float64(1), errorCount)
	})
}

func TestRecordPluginError(t *testing.T) {
	// Reset the metric before testing
	wasmPluginErrorsTotal.Reset()

	RecordPluginError("error-plugin", "request")

	// Verify error counter was incremented
	count := testutil.ToFloat64(wasmPluginErrorsTotal.WithLabelValues("error-plugin", "request"))
	assert.Equal(t, float64(1), count)

	// Increment again
	RecordPluginError("error-plugin", "request")
	count = testutil.ToFloat64(wasmPluginErrorsTotal.WithLabelValues("error-plugin", "request"))
	assert.Equal(t, float64(2), count)
}

func TestSetPluginsLoaded(t *testing.T) {
	SetPluginsLoaded(5)

	count := testutil.ToFloat64(wasmPluginsLoaded)
	assert.Equal(t, float64(5), count)

	// Update the value
	SetPluginsLoaded(10)
	count = testutil.ToFloat64(wasmPluginsLoaded)
	assert.Equal(t, float64(10), count)

	// Set to zero
	SetPluginsLoaded(0)
	count = testutil.ToFloat64(wasmPluginsLoaded)
	assert.Equal(t, float64(0), count)
}

func TestSetInstancePoolSize(t *testing.T) {
	SetInstancePoolSize("pool-plugin", 4)

	count := testutil.ToFloat64(wasmInstancePoolSize.WithLabelValues("pool-plugin"))
	assert.Equal(t, float64(4), count)

	// Update the value
	SetInstancePoolSize("pool-plugin", 8)
	count = testutil.ToFloat64(wasmInstancePoolSize.WithLabelValues("pool-plugin"))
	assert.Equal(t, float64(8), count)
}

func TestRecordPluginTimeout(t *testing.T) {
	// Reset the metric before testing
	wasmPluginTimeoutsTotal.Reset()

	RecordPluginTimeout("timeout-plugin", "request")

	// Verify timeout counter was incremented
	count := testutil.ToFloat64(wasmPluginTimeoutsTotal.WithLabelValues("timeout-plugin", "request"))
	assert.Equal(t, float64(1), count)

	// Increment again
	RecordPluginTimeout("timeout-plugin", "request")
	count = testutil.ToFloat64(wasmPluginTimeoutsTotal.WithLabelValues("timeout-plugin", "request"))
	assert.Equal(t, float64(2), count)
}

func TestMetricsWithDifferentLabels(t *testing.T) {
	// Reset all metrics
	wasmPluginExecutionTotal.Reset()
	wasmPluginErrorsTotal.Reset()
	wasmPluginTimeoutsTotal.Reset()

	// Record multiple executions with different labels
	RecordPluginExecution("plugin-a", "request", 10*time.Millisecond, nil)
	RecordPluginExecution("plugin-a", "response", 20*time.Millisecond, nil)
	RecordPluginExecution("plugin-b", "request", 15*time.Millisecond, errors.New("error"))

	// Verify different label combinations create separate time series
	countARequest := testutil.ToFloat64(wasmPluginExecutionTotal.WithLabelValues("plugin-a", "request", "success"))
	countAResponse := testutil.ToFloat64(wasmPluginExecutionTotal.WithLabelValues("plugin-a", "response", "success"))
	countBRequest := testutil.ToFloat64(wasmPluginExecutionTotal.WithLabelValues("plugin-b", "request", "error"))

	assert.Equal(t, float64(1), countARequest)
	assert.Equal(t, float64(1), countAResponse)
	assert.Equal(t, float64(1), countBRequest)
}
