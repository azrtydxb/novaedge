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

func TestConstants(t *testing.T) {
	if DefaultMaxRecvMsgSize != 16*1024*1024 {
		t.Errorf("DefaultMaxRecvMsgSize = %d, want 16 MiB", DefaultMaxRecvMsgSize)
	}
	if DefaultMaxSendMsgSize != 16*1024*1024 {
		t.Errorf("DefaultMaxSendMsgSize = %d, want 16 MiB", DefaultMaxSendMsgSize)
	}
	if DefaultMaxConcurrentStreams != 100 {
		t.Errorf("DefaultMaxConcurrentStreams = %d, want 100", DefaultMaxConcurrentStreams)
	}
}

func TestServerOptions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	opts := ServerOptions(logger)
	if len(opts) == 0 {
		t.Error("ServerOptions returned no options")
	}
	// Verify we get a reasonable number of options:
	// MaxRecvMsgSize, MaxSendMsgSize, MaxConcurrentStreams,
	// KeepaliveParams, KeepaliveEnforcementPolicy,
	// ChainUnaryInterceptor, ChainStreamInterceptor
	if len(opts) < 7 {
		t.Errorf("ServerOptions returned %d options, expected at least 7", len(opts))
	}
}

func TestClientOptions(t *testing.T) {
	opts := ClientOptions()
	if len(opts) == 0 {
		t.Error("ClientOptions returned no options")
	}
	// Verify we get at least DefaultCallOptions + KeepaliveParams
	if len(opts) < 2 {
		t.Errorf("ClientOptions returned %d options, expected at least 2", len(opts))
	}
}
