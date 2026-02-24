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

package health

import "sync"

// Aggregator computes node-wide health state from per-CPU BPF counters.
// It tracks the previous poll's counters to compute deltas, providing a
// sliding window view of backend health.
type Aggregator struct {
	mu       sync.Mutex
	previous map[BackendKey]AggregatedHealth
}

// NewAggregator creates a new health state aggregator.
func NewAggregator() *Aggregator {
	return &Aggregator{
		previous: make(map[BackendKey]AggregatedHealth),
	}
}

// Aggregate takes per-backend per-CPU health counters and produces
// node-wide aggregated health data with deltas from the last poll.
func (a *Aggregator) Aggregate(perCPU map[BackendKey][]BackendHealth) map[BackendKey]AggregatedHealth {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := make(map[BackendKey]AggregatedHealth, len(perCPU))

	for key, cpuValues := range perCPU {
		agg := sumPerCPU(cpuValues)

		// Compute deltas from previous poll.
		if prev, ok := a.previous[key]; ok {
			agg.DeltaTotal = saturatingSub(agg.TotalConns, prev.TotalConns)
			agg.DeltaFailed = saturatingSub(agg.FailedConns, prev.FailedConns)
			agg.DeltaTimeout = saturatingSub(agg.TimeoutConns, prev.TimeoutConns)
			agg.DeltaSuccess = saturatingSub(agg.SuccessConns, prev.SuccessConns)
		} else {
			// First poll: deltas equal absolute counters.
			agg.DeltaTotal = agg.TotalConns
			agg.DeltaFailed = agg.FailedConns
			agg.DeltaTimeout = agg.TimeoutConns
			agg.DeltaSuccess = agg.SuccessConns
		}

		result[key] = agg
	}

	// Store current values for next delta computation.
	a.previous = make(map[BackendKey]AggregatedHealth, len(result))
	for k, v := range result {
		a.previous[k] = v
	}

	return result
}

// sumPerCPU aggregates per-CPU health counters into a single
// AggregatedHealth struct.
func sumPerCPU(cpuValues []BackendHealth) AggregatedHealth {
	var agg AggregatedHealth
	var totalRTTNS uint64

	for _, v := range cpuValues {
		agg.TotalConns += v.TotalConns
		agg.FailedConns += v.FailedConns
		agg.TimeoutConns += v.TimeoutConns
		agg.SuccessConns += v.SuccessConns
		totalRTTNS += v.TotalRTTNS

		if v.LastSuccessNS > agg.LastSuccessNS {
			agg.LastSuccessNS = v.LastSuccessNS
		}
		if v.LastFailureNS > agg.LastFailureNS {
			agg.LastFailureNS = v.LastFailureNS
		}
	}

	// Compute failure rate.
	if agg.TotalConns > 0 {
		agg.FailureRate = float64(agg.FailedConns+agg.TimeoutConns) / float64(agg.TotalConns)
	}

	// Compute average RTT.
	if agg.SuccessConns > 0 {
		agg.AvgRTTNS = totalRTTNS / agg.SuccessConns
	}

	return agg
}

// saturatingSub returns a - b, clamped to 0 if b > a (handles counter
// wraps or resets gracefully).
func saturatingSub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}
