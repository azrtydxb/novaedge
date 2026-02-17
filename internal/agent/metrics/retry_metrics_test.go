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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRetryCount(t *testing.T) {
	RetryCount.Inc()
	RetryCount.Inc()
	RetryCount.Inc()

	value := testutil.ToFloat64(RetryCount)
	assert.GreaterOrEqual(t, value, float64(3))
}

func TestRetrySuccess(t *testing.T) {
	RetrySuccess.Inc()

	value := testutil.ToFloat64(RetrySuccess)
	assert.GreaterOrEqual(t, value, float64(1))
}

func TestRetryExhausted(t *testing.T) {
	RetryExhausted.Inc()
	RetryExhausted.Inc()

	value := testutil.ToFloat64(RetryExhausted)
	assert.GreaterOrEqual(t, value, float64(2))
}

func TestRetryBudgetExhausted(t *testing.T) {
	RetryBudgetExhausted.Inc()

	value := testutil.ToFloat64(RetryBudgetExhausted)
	assert.GreaterOrEqual(t, value, float64(1))
}
