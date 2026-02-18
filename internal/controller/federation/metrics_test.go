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

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestFederationPeersTotal(t *testing.T) {
	// Set a value
	FederationPeersTotal.Set(5)
	value := testutil.ToFloat64(FederationPeersTotal)
	assert.Equal(t, float64(5), value)

	// Increment
	FederationPeersTotal.Inc()
	value = testutil.ToFloat64(FederationPeersTotal)
	assert.Equal(t, float64(6), value)

	// Decrement
	FederationPeersTotal.Dec()
	value = testutil.ToFloat64(FederationPeersTotal)
	assert.Equal(t, float64(5), value)
}

func TestFederationPeersHealthy(t *testing.T) {
	FederationPeersHealthy.Set(3)
	value := testutil.ToFloat64(FederationPeersHealthy)
	assert.Equal(t, float64(3), value)
}

func TestFederationPeersConnected(t *testing.T) {
	FederationPeersConnected.Set(2)
	value := testutil.ToFloat64(FederationPeersConnected)
	assert.Equal(t, float64(2), value)
}

func TestFederationPhaseGauge(t *testing.T) {
	FederationPhaseGauge.WithLabelValues("active").Set(1)
	FederationPhaseGauge.WithLabelValues("inactive").Set(0)

	value := testutil.ToFloat64(FederationPhaseGauge.WithLabelValues("active"))
	assert.Equal(t, float64(1), value)

	value = testutil.ToFloat64(FederationPhaseGauge.WithLabelValues("inactive"))
	assert.Equal(t, float64(0), value)
}

func TestFederationSyncTotal(t *testing.T) {
	// Increment counters
	FederationSyncTotal.WithLabelValues("full", "outbound").Inc()
	FederationSyncTotal.WithLabelValues("incremental", "inbound").Inc()

	value := testutil.ToFloat64(FederationSyncTotal.WithLabelValues("full", "outbound"))
	assert.Equal(t, float64(1), value)

	value = testutil.ToFloat64(FederationSyncTotal.WithLabelValues("incremental", "inbound"))
	assert.Equal(t, float64(1), value)
}

func TestFederationChangesReceived(t *testing.T) {
	FederationChangesReceived.WithLabelValues("peer1", "ProxyGateway").Inc()
	FederationChangesReceived.WithLabelValues("peer1", "ProxyRoute").Inc()
	FederationChangesReceived.WithLabelValues("peer2", "ProxyBackend").Inc()

	value := testutil.ToFloat64(FederationChangesReceived.WithLabelValues("peer1", "ProxyGateway"))
	assert.Equal(t, float64(1), value)

	value = testutil.ToFloat64(FederationChangesReceived.WithLabelValues("peer1", "ProxyRoute"))
	assert.Equal(t, float64(1), value)

	value = testutil.ToFloat64(FederationChangesReceived.WithLabelValues("peer2", "ProxyBackend"))
	assert.Equal(t, float64(1), value)
}

func TestFederationChangesSent(t *testing.T) {
	FederationChangesSent.WithLabelValues("peer1", "ProxyGateway").Inc()
	FederationChangesSent.WithLabelValues("peer1", "ProxyGateway").Inc()

	value := testutil.ToFloat64(FederationChangesSent.WithLabelValues("peer1", "ProxyGateway"))
	assert.Equal(t, float64(2), value)
}

func TestFederationConflictsTotal(t *testing.T) {
	FederationConflictsTotal.WithLabelValues("resolved").Inc()
	FederationConflictsTotal.WithLabelValues("pending").Inc()

	value := testutil.ToFloat64(FederationConflictsTotal.WithLabelValues("resolved"))
	assert.Equal(t, float64(1), value)

	value = testutil.ToFloat64(FederationConflictsTotal.WithLabelValues("pending"))
	assert.Equal(t, float64(1), value)
}

func TestFederationConflictsPending(t *testing.T) {
	FederationConflictsPending.Set(3)
	value := testutil.ToFloat64(FederationConflictsPending)
	assert.Equal(t, float64(3), value)
}
