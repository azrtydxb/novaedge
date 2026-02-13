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
	"testing"
)

func TestNewRetryBudget_Defaults(t *testing.T) {
	b := NewRetryBudget(0, 0)
	if b.BudgetPercent != defaultBudgetPercent {
		t.Errorf("expected BudgetPercent=%f, got %f", defaultBudgetPercent, b.BudgetPercent)
	}
	if b.MinRetryConcurrency != defaultMinRetryConcurrency {
		t.Errorf("expected MinRetryConcurrency=%d, got %d", defaultMinRetryConcurrency, b.MinRetryConcurrency)
	}
}

func TestNewRetryBudget_Custom(t *testing.T) {
	b := NewRetryBudget(10.0, 5)
	if b.BudgetPercent != 10.0 {
		t.Errorf("expected BudgetPercent=10.0, got %f", b.BudgetPercent)
	}
	if b.MinRetryConcurrency != 5 {
		t.Errorf("expected MinRetryConcurrency=5, got %d", b.MinRetryConcurrency)
	}
}

func TestRetryBudget_AllowRetryUnderThreshold(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	// Simulate 100 active requests; budget allows 20 concurrent retries
	for range 100 {
		b.IncActiveRequests()
	}

	// 5 active retries should be allowed (5 < 20)
	for range 5 {
		b.IncActiveRetries()
	}
	if !b.AllowRetry() {
		t.Error("expected AllowRetry=true when 5 retries are active out of 20 budget")
	}
}

func TestRetryBudget_RejectRetryOverThreshold(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	// Simulate 50 active requests; budget allows 10 concurrent retries
	for range 50 {
		b.IncActiveRequests()
	}

	// Saturate the budget: 10 active retries
	for range 10 {
		b.IncActiveRetries()
	}

	if b.AllowRetry() {
		t.Error("expected AllowRetry=false when 10 retries are active with budget of 10")
	}
}

func TestRetryBudget_MinRetryConcurrencyFloor(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	// With 0 active requests, 20% of 0 = 0, but MinRetryConcurrency = 3
	if !b.AllowRetry() {
		t.Error("expected AllowRetry=true with 0 active requests due to MinRetryConcurrency floor")
	}

	// With 1 active request, 20% of 1 = 0, but MinRetryConcurrency = 3
	b.IncActiveRequests()
	b.IncActiveRetries()
	if !b.AllowRetry() {
		t.Error("expected AllowRetry=true with 1 retry and MinRetryConcurrency=3")
	}

	b.IncActiveRetries()
	if !b.AllowRetry() {
		t.Error("expected AllowRetry=true with 2 retries and MinRetryConcurrency=3")
	}

	// 3 active retries should hit the floor
	b.IncActiveRetries()
	if b.AllowRetry() {
		t.Error("expected AllowRetry=false with 3 retries at MinRetryConcurrency=3")
	}
}

func TestRetryBudget_LowTrafficMinConcurrency(t *testing.T) {
	b := NewRetryBudget(10.0, 5)

	// With 10 active requests, 10% = 1, but MinRetryConcurrency = 5
	for range 10 {
		b.IncActiveRequests()
	}

	for i := range 5 {
		if !b.AllowRetry() {
			t.Errorf("expected AllowRetry=true with %d retries at MinRetryConcurrency=5", i)
		}
		b.IncActiveRetries()
	}

	if b.AllowRetry() {
		t.Error("expected AllowRetry=false with 5 retries at MinRetryConcurrency=5")
	}
}

func TestRetryBudget_IncDecRequests(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	b.IncActiveRequests()
	b.IncActiveRequests()
	if b.ActiveRequests() != 2 {
		t.Errorf("expected 2 active requests, got %d", b.ActiveRequests())
	}

	b.DecActiveRequests()
	if b.ActiveRequests() != 1 {
		t.Errorf("expected 1 active request, got %d", b.ActiveRequests())
	}
}

func TestRetryBudget_IncDecRetries(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	b.IncActiveRetries()
	b.IncActiveRetries()
	if b.ActiveRetries() != 2 {
		t.Errorf("expected 2 active retries, got %d", b.ActiveRetries())
	}

	b.DecActiveRetries()
	if b.ActiveRetries() != 1 {
		t.Errorf("expected 1 active retry, got %d", b.ActiveRetries())
	}
}

func TestRetryBudget_BudgetRecoverAfterDecrement(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	// 10 active requests, budget = max(3, 2) = 3
	for range 10 {
		b.IncActiveRequests()
	}

	// Fill budget
	for range 3 {
		b.IncActiveRetries()
	}
	if b.AllowRetry() {
		t.Error("expected budget exhausted")
	}

	// Complete one retry
	b.DecActiveRetries()
	if !b.AllowRetry() {
		t.Error("expected budget to recover after decrementing retry")
	}
}

func TestRetryBudget_ConcurrentAccess(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	const numGoroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent request tracking
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.IncActiveRequests()
				_ = b.AllowRetry()
				b.DecActiveRequests()
			}
		}()
	}

	// Concurrent retry tracking
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.IncActiveRetries()
				_ = b.AllowRetry()
				b.DecActiveRetries()
			}
		}()
	}

	wg.Wait()

	// After all goroutines complete, counters should be back to zero
	if b.ActiveRequests() != 0 {
		t.Errorf("expected 0 active requests after concurrent test, got %d", b.ActiveRequests())
	}
	if b.ActiveRetries() != 0 {
		t.Errorf("expected 0 active retries after concurrent test, got %d", b.ActiveRetries())
	}
}

func TestGetClusterRetryBudget(t *testing.T) {
	resetClusterRetryBudgets()
	defer resetClusterRetryBudgets()

	b1 := getClusterRetryBudget("cluster-a")
	b2 := getClusterRetryBudget("cluster-a")

	if b1 != b2 {
		t.Error("expected same budget instance for same cluster key")
	}

	b3 := getClusterRetryBudget("cluster-b")
	if b1 == b3 {
		t.Error("expected different budget instances for different cluster keys")
	}
}

func TestGetClusterRetryBudget_ConcurrentCreation(t *testing.T) {
	resetClusterRetryBudgets()
	defer resetClusterRetryBudgets()

	const numGoroutines = 50
	budgets := make([]*RetryBudget, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			budgets[idx] = getClusterRetryBudget("concurrent-cluster")
		}(i)
	}

	wg.Wait()

	// All goroutines should have gotten the same budget instance
	for i := 1; i < numGoroutines; i++ {
		if budgets[i] != budgets[0] {
			t.Error("expected all goroutines to get the same budget instance")
			break
		}
	}
}

func TestRetryBudget_HighTrafficScaling(t *testing.T) {
	b := NewRetryBudget(20.0, 3)

	// Simulate 1000 active requests; budget = max(3, 200) = 200
	for range 1000 {
		b.IncActiveRequests()
	}

	// 199 retries should be allowed
	for range 199 {
		b.IncActiveRetries()
	}
	if !b.AllowRetry() {
		t.Error("expected AllowRetry=true with 199 retries out of 200 budget")
	}

	// 200th retry fills the budget
	b.IncActiveRetries()
	if b.AllowRetry() {
		t.Error("expected AllowRetry=false with 200 retries at 200 budget")
	}
}
