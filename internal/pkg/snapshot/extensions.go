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

// Package snapshot provides shared types used by both the controller
// snapshot builder and the node agent config layer.
package snapshot

import (
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// Extensions carries mTLS, PROXY protocol, and OCSP configuration
// that extends the base proto ConfigSnapshot. These are keyed by listener
// or cluster identifiers for easy lookup.
type Extensions struct {
	// ListenerExtensions maps "gateway/listener" -> extensions
	ListenerExtensions map[string]*pb.ListenerExtensions
	// ClusterExtensions maps "namespace/name" -> extensions
	ClusterExtensions map[string]*pb.ClusterExtensions
}
