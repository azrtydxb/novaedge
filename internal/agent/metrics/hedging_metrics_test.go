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

func TestHedgingRequestsTotal(t *testing.T) {
	HedgingRequestsTotal.Inc()
	HedgingRequestsTotal.Inc()

	value := testutil.ToFloat64(HedgingRequestsTotal)
	assert.Equal(t, float64(2), value)
}

func TestHedgingWins(t *testing.T) {
	HedgingWins.Inc()

	value := testutil.ToFloat64(HedgingWins)
	assert.GreaterOrEqual(t, value, float64(1))
}

func TestHedgingCancelled(t *testing.T) {
	HedgingCancelled.Inc()
	HedgingCancelled.Inc()
	HedgingCancelled.Inc()

	value := testutil.ToFloat64(HedgingCancelled)
	assert.GreaterOrEqual(t, value, float64(3))
}
