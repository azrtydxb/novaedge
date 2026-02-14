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
	"testing"
)

func TestWebSocketConstants(t *testing.T) {
	// Test WebSocket read buffer size
	expectedReadBuffer := 4096
	if DefaultWebSocketReadBufferSize != expectedReadBuffer {
		t.Errorf("DefaultWebSocketReadBufferSize = %d, want %d", DefaultWebSocketReadBufferSize, expectedReadBuffer)
	}

	// Test WebSocket write buffer size
	expectedWriteBuffer := 4096
	if DefaultWebSocketWriteBufferSize != expectedWriteBuffer {
		t.Errorf("DefaultWebSocketWriteBufferSize = %d, want %d", DefaultWebSocketWriteBufferSize, expectedWriteBuffer)
	}
}

func TestMaxRequestBodySize(t *testing.T) {
	// Test max request body size (10MB)
	expectedMaxBody := 10 * 1024 * 1024
	if DefaultMaxRequestBodySize != expectedMaxBody {
		t.Errorf("DefaultMaxRequestBodySize = %d, want %d", DefaultMaxRequestBodySize, expectedMaxBody)
	}

	// Verify it's 10MB
	if DefaultMaxRequestBodySize != 10485760 {
		t.Errorf("DefaultMaxRequestBodySize = %d, want 10485760 (10MB)", DefaultMaxRequestBodySize)
	}
}
