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
	"net/http"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// CanaryHeaderName is the HTTP header used to force canary backend selection.
	// When a request carries this header with value "true", traffic is routed to
	// the backend with the lowest weight (i.e. the canary backend).
	CanaryHeaderName = "X-Canary"

	// headerValueTrue is the constant for the boolean "true" header value.
	headerValueTrue = "true"
)

// selectBackendForRequest picks a BackendRef considering both canary header
// routing and weighted random selection.
//
// Selection logic:
//  1. If the request has "X-Canary: true" header AND there are multiple backends
//     with different weights, the backend with the lowest weight (canary) is chosen.
//  2. Otherwise, weighted random selection is used (existing behavior).
func selectBackendForRequest(backends []*pb.BackendRef, req *http.Request) *pb.BackendRef {
	if len(backends) == 0 {
		return nil
	}

	if len(backends) == 1 {
		return backends[0]
	}

	// Check for canary header override
	if req.Header.Get(CanaryHeaderName) == headerValueTrue {
		return selectCanaryBackend(backends)
	}

	// Default: weighted random selection
	return selectWeightedBackend(backends)
}

// selectCanaryBackend returns the backend with the lowest weight, which by
// convention is the canary deployment target. If all backends have equal weight,
// the last one in the list is returned (preserving order-based convention).
func selectCanaryBackend(backends []*pb.BackendRef) *pb.BackendRef {
	if len(backends) == 0 {
		return nil
	}

	canary := backends[0]
	minWeight := effectiveWeight(canary)

	for _, b := range backends[1:] {
		w := effectiveWeight(b)
		if w < minWeight {
			minWeight = w
			canary = b
		}
	}

	return canary
}

// effectiveWeight returns the weight for a backend ref, defaulting to 1 if unset.
func effectiveWeight(b *pb.BackendRef) int32 {
	if b.Weight <= 0 {
		return 1
	}
	return b.Weight
}
