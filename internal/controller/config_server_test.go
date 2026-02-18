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

package controller

import (
	"testing"
)

func TestConditionReasonConstants(t *testing.T) {
	if ConditionReasonValid != "Valid" {
		t.Errorf("ConditionReasonValid = %q, want %q", ConditionReasonValid, "Valid")
	}
	if ConditionReasonValidationFailed != "ValidationFailed" {
		t.Errorf("ConditionReasonValidationFailed = %q, want %q", ConditionReasonValidationFailed, "ValidationFailed")
	}
}

func TestKindGatewayConstant(t *testing.T) {
	if kindGateway != "Gateway" {
		t.Errorf("kindGateway = %q, want %q", kindGateway, "Gateway")
	}
}

func TestGetConfigServer_Nil(t *testing.T) {
	// Reset config server
	configServerMu.Lock()
	configServer = nil
	configServerMu.Unlock()

	server := GetConfigServer()
	if server != nil {
		t.Error("GetConfigServer() should return nil when not set")
	}
}

func TestSetConfigServer(t *testing.T) {
	// Reset
	configServerMu.Lock()
	configServer = nil
	configServerMu.Unlock()

	// Set nil should work
	SetConfigServer(nil)
	if GetConfigServer() != nil {
		t.Error("GetConfigServer() should return nil after setting nil")
	}
}

func TestTriggerConfigUpdate_NilServer(_ *testing.T) {
	// Reset config server
	configServerMu.Lock()
	configServer = nil
	configServerMu.Unlock()

	// Should not panic
	TriggerConfigUpdate()
}

func TestTriggerNodeConfigUpdate_NilServer(_ *testing.T) {
	// Reset config server
	configServerMu.Lock()
	configServer = nil
	configServerMu.Unlock()

	// Should not panic
	TriggerNodeConfigUpdate("test-node")
}

func TestSetGetConfigServer_Concurrent(_ *testing.T) {
	// Reset
	configServerMu.Lock()
	configServer = nil
	configServerMu.Unlock()

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = GetConfigServer()
			}
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				SetConfigServer(nil)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}
}
