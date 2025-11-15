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

package protocol

import (
	"net/http"
	"strings"
)

// IsGRPCRequest checks if an HTTP request is a gRPC request
func IsGRPCRequest(r *http.Request) bool {
	// gRPC requires HTTP/2
	if r.ProtoMajor != 2 {
		return false
	}

	// Check Content-Type header for gRPC
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/grpc")
}

// IsWebSocketUpgrade checks if a request is a WebSocket upgrade request
func IsWebSocketUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") == "websocket" &&
		strings.ToLower(r.Header.Get("Connection")) == "upgrade"
}
