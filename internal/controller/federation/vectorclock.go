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
	"encoding/json"
	"sync"
)

// VectorClock implements a vector clock for causal ordering of events
// in a distributed system. It tracks logical time across multiple nodes.
type VectorClock struct {
	mu     sync.RWMutex
	clocks map[string]int64
}

// NewVectorClock creates a new vector clock
func NewVectorClock() *VectorClock {
	return &VectorClock{
		clocks: make(map[string]int64),
	}
}

// NewVectorClockFromMap creates a vector clock from an existing map
func NewVectorClockFromMap(clocks map[string]int64) *VectorClock {
	vc := &VectorClock{
		clocks: make(map[string]int64),
	}
	for k, v := range clocks {
		vc.clocks[k] = v
	}
	return vc
}

// Increment increments the clock for the given member and returns the new value
func (vc *VectorClock) Increment(member string) int64 {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.clocks[member]++
	return vc.clocks[member]
}

// Get returns the current clock value for a member
func (vc *VectorClock) Get(member string) int64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.clocks[member]
}

// Set sets the clock value for a member
func (vc *VectorClock) Set(member string, value int64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.clocks[member] = value
}

// Merge merges another vector clock into this one, taking the max of each entry
func (vc *VectorClock) Merge(other *VectorClock) {
	if other == nil {
		return
	}

	// Copy other's entries under RLock into a local snapshot to avoid
	// ABBA deadlock when two goroutines call Merge(vc1, vc2) and Merge(vc2, vc1)
	// concurrently.
	other.mu.RLock()
	snapshot := make(map[string]int64, len(other.clocks))
	for k, v := range other.clocks {
		snapshot[k] = v
	}
	other.mu.RUnlock()

	vc.mu.Lock()
	defer vc.mu.Unlock()
	for k, v := range snapshot {
		if v > vc.clocks[k] {
			vc.clocks[k] = v
		}
	}
}

// MergeMap merges a map of clocks into this vector clock
func (vc *VectorClock) MergeMap(clocks map[string]int64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	for member, value := range clocks {
		if value > vc.clocks[member] {
			vc.clocks[member] = value
		}
	}
}

// Compare compares this vector clock with another
// Returns:
//
//	 1 if this clock is strictly greater (happened after)
//	-1 if this clock is strictly less (happened before)
//	 0 if the clocks are concurrent (no causal ordering)
func (vc *VectorClock) Compare(other *VectorClock) int {
	if other == nil {
		return 1
	}
	other.mu.RLock()
	defer other.mu.RUnlock()
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	hasGreater := false
	hasLess := false

	// Collect all members from both clocks
	members := make(map[string]bool)
	for m := range vc.clocks {
		members[m] = true
	}
	for m := range other.clocks {
		members[m] = true
	}

	for member := range members {
		thisVal := vc.clocks[member]
		otherVal := other.clocks[member]

		if thisVal > otherVal {
			hasGreater = true
		} else if thisVal < otherVal {
			hasLess = true
		}
	}

	if hasGreater && !hasLess {
		return 1 // This happened after
	} else if hasLess && !hasGreater {
		return -1 // This happened before
	}
	return 0 // Concurrent
}

// CompareMap compares this vector clock with a map representation
func (vc *VectorClock) CompareMap(other map[string]int64) int {
	return vc.Compare(NewVectorClockFromMap(other))
}

// HappenedBefore returns true if this clock happened strictly before the other
func (vc *VectorClock) HappenedBefore(other *VectorClock) bool {
	return vc.Compare(other) == -1
}

// HappenedAfter returns true if this clock happened strictly after the other
func (vc *VectorClock) HappenedAfter(other *VectorClock) bool {
	return vc.Compare(other) == 1
}

// Concurrent returns true if the clocks are concurrent (no causal ordering)
func (vc *VectorClock) Concurrent(other *VectorClock) bool {
	return vc.Compare(other) == 0
}

// Copy returns a copy of this vector clock
func (vc *VectorClock) Copy() *VectorClock {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return NewVectorClockFromMap(vc.clocks)
}

// ToMap returns a copy of the internal clock map
func (vc *VectorClock) ToMap() map[string]int64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	result := make(map[string]int64)
	for k, v := range vc.clocks {
		result[k] = v
	}
	return result
}

// String returns a string representation of the vector clock
func (vc *VectorClock) String() string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	data, _ := json.Marshal(vc.clocks)
	return string(data)
}

// Equal returns true if the clocks are equal
func (vc *VectorClock) Equal(other *VectorClock) bool {
	if other == nil {
		return false
	}
	other.mu.RLock()
	defer other.mu.RUnlock()
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	if len(vc.clocks) != len(other.clocks) {
		return false
	}

	for k, v := range vc.clocks {
		if other.clocks[k] != v {
			return false
		}
	}
	return true
}

// IsZero returns true if all clock values are zero
func (vc *VectorClock) IsZero() bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	for _, v := range vc.clocks {
		if v != 0 {
			return false
		}
	}
	return true
}
