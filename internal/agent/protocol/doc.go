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

// Package protocol provides protocol detection utilities for identifying
// the type of HTTP traffic (standard HTTP, WebSocket, gRPC, etc.).
//
// Protocol detection enables the router to apply protocol-specific handling:
//   - gRPC requests require HTTP/2 and special header preservation
//   - WebSocket requires connection upgrade and bidirectional proxying
//   - Standard HTTP uses simple request/response proxying
//
// # gRPC Detection
//
// gRPC requests are identified by:
//   - HTTP/2 protocol (required for gRPC)
//   - Content-Type header starting with "application/grpc"
//
// Example:
//
//	if protocol.IsGRPCRequest(req) {
//	    // Handle as gRPC - preserve grpc-* headers, enable streaming
//	}
//
// # WebSocket Detection
//
// WebSocket upgrade requests are identified by:
//   - "Upgrade: websocket" header
//   - "Connection: upgrade" header
//
// Example:
//
//	if protocol.IsWebSocketUpgrade(req) {
//	    // Upgrade connection and start bidirectional proxying
//	}
//
// # Future Protocol Support
//
// This package will be extended to support:
//   - HTTP/3 and QUIC detection
//   - WebTransport identification
//   - Custom protocol detection via headers
//
// Protocol detection is performed early in the request handling pipeline
// to route requests to appropriate handlers.
//
// DEPRECATED: This package will be removed once --forwarding-plane=rust is
// validated and the Rust dataplane handles all protocol detection natively.
// See docs/plans/forwarding-deprecation.md for the removal timeline.
package protocol
