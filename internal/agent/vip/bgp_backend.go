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

package vip

import (
	"context"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// BGPBackend abstracts the BGP implementation used for VIP announcements.
// Two implementations exist:
//   - BGPHandler: built-in GoBGP server (default)
//   - NovaRouteBGPHandler: delegates to a NovaRoute agent via gRPC
type BGPBackend interface {
	// Start initialises the backend. Called once at startup.
	Start(ctx context.Context) error

	// AddVIP announces a VIP via BGP, configuring peers and routes.
	AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error

	// RemoveVIP withdraws a VIP from BGP announcements.
	RemoveVIP(ctx context.Context, assignment *pb.VIPAssignment) error

	// Stop gracefully shuts down the backend, deregistering from upstream
	// services and closing connections.
	Stop(ctx context.Context) error
}
