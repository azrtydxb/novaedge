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

package router

import (
	"sync"
	"sync/atomic"
)

// defaultBudgetPercent is the default percentage of active requests allowed as concurrent retries.
const defaultBudgetPercent = 20.0

// defaultMinRetryConcurrency is the minimum number of concurrent retries always allowed,
// even when active request count is very low.
const defaultMinRetryConcurrency = 3

// RetryBudget limits concurrent retries as a percentage of active requests per cluster.
// This prevents cascading retry storms where retries amplify load during failures.
// The budget allows retries when: activeRetries < max(MinRetryConcurrency, activeRequests * BudgetPercent / 100)
type RetryBudget struct {
	// BudgetPercent is the maximum percentage of active requests that may be retries.
	BudgetPercent float64

	// MinRetryConcurrency is the floor for allowed concurrent retries regardless of active request count.
	// This ensures that at low traffic, a minimum number of retries are always allowed.
	MinRetryConcurrency int64

	activeRequests atomic.Int64
	activeRetries  atomic.Int64
}

// NewRetryBudget creates a RetryBudget with the given settings.
// If budgetPercent <= 0, defaultBudgetPercent is used.
// If minRetryConcurrency <= 0, defaultMinRetryConcurrency is used.
func NewRetryBudget(budgetPercent float64, minRetryConcurrency int64) *RetryBudget {
	if budgetPercent <= 0 {
		budgetPercent = defaultBudgetPercent
	}
	if minRetryConcurrency <= 0 {
		minRetryConcurrency = defaultMinRetryConcurrency
	}
	return &RetryBudget{
		BudgetPercent:       budgetPercent,
		MinRetryConcurrency: minRetryConcurrency,
	}
}

// AllowRetry returns true if the retry budget permits another concurrent retry.
// The decision is: activeRetries < max(MinRetryConcurrency, activeRequests * BudgetPercent / 100)
func (b *RetryBudget) AllowRetry() bool {
	currentRetries := b.activeRetries.Load()
	currentRequests := b.activeRequests.Load()

	budgetLimit := int64(float64(currentRequests) * b.BudgetPercent / 100.0)
	if budgetLimit < b.MinRetryConcurrency {
		budgetLimit = b.MinRetryConcurrency
	}

	return currentRetries < budgetLimit
}

// IncActiveRequests increments the active request counter.
func (b *RetryBudget) IncActiveRequests() {
	b.activeRequests.Add(1)
}

// DecActiveRequests decrements the active request counter.
func (b *RetryBudget) DecActiveRequests() {
	b.activeRequests.Add(-1)
}

// IncActiveRetries increments the active retry counter.
func (b *RetryBudget) IncActiveRetries() {
	b.activeRetries.Add(1)
}

// DecActiveRetries decrements the active retry counter.
func (b *RetryBudget) DecActiveRetries() {
	b.activeRetries.Add(-1)
}

// ActiveRequests returns the current number of active requests.
func (b *RetryBudget) ActiveRequests() int64 {
	return b.activeRequests.Load()
}

// ActiveRetries returns the current number of active retries.
func (b *RetryBudget) ActiveRetries() int64 {
	return b.activeRetries.Load()
}

// clusterRetryBudgets stores a RetryBudget per cluster, keyed by cluster name.
var clusterRetryBudgets sync.Map

// getClusterRetryBudget returns the RetryBudget for a cluster, creating one with defaults if absent.
func getClusterRetryBudget(clusterKey string) *RetryBudget {
	if val, ok := clusterRetryBudgets.Load(clusterKey); ok {
		budget, _ := val.(*RetryBudget)
		return budget
	}
	budget := NewRetryBudget(defaultBudgetPercent, defaultMinRetryConcurrency)
	actual, _ := clusterRetryBudgets.LoadOrStore(clusterKey, budget)
	stored, _ := actual.(*RetryBudget)
	return stored
}

// resetClusterRetryBudgets clears all stored cluster budgets. Used for testing.
func resetClusterRetryBudgets() {
	clusterRetryBudgets.Range(func(key, _ any) bool {
		clusterRetryBudgets.Delete(key)
		return true
	})
}
