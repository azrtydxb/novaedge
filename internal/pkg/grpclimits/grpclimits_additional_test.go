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

package grpclimits

import (
	"testing"

	"go.uber.org/zap"
)

func TestDefaultMaxRecvMsgSize(t *testing.T) {
	expected := 16 * 1024 * 1024 // 16 MiB
	if DefaultMaxRecvMsgSize != expected {
		t.Errorf("DefaultMaxRecvMsgSize = %v, want %v", DefaultMaxRecvMsgSize, expected)
	}
}

func TestDefaultMaxSendMsgSize(t *testing.T) {
	expected := 16 * 1024 * 1024 // 16 MiB
	if DefaultMaxSendMsgSize != expected {
		t.Errorf("DefaultMaxSendMsgSize = %v, want %v", DefaultMaxSendMsgSize, expected)
	}
}

func TestDefaultMaxConcurrentStreams(t *testing.T) {
	if DefaultMaxConcurrentStreams != 100 {
		t.Errorf("DefaultMaxConcurrentStreams = %v, want 100", DefaultMaxConcurrentStreams)
	}
}

func TestServerOptions_ReturnsOptions(t *testing.T) {
	logger := zap.NewNop()
	opts := ServerOptions(logger)
	
	if len(opts) == 0 {
		t.Error("ServerOptions() returned empty slice")
	}
}

func TestClientOptions_ReturnsOptions(t *testing.T) {
	opts := ClientOptions()
	
	if len(opts) == 0 {
		t.Error("ClientOptions() returned empty slice")
	}
}

func TestServerOptions_WithNilLogger(t *testing.T) {
	// Should not panic with nil logger
	opts := ServerOptions(nil)
	if len(opts) == 0 {
		t.Error("ServerOptions() returned empty slice")
	}
}
