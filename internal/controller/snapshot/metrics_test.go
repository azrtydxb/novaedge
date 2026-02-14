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

package snapshot

import (
	"testing"
)

func TestRecordSnapshotBuild(t *testing.T) {
	// This test verifies the function doesn't panic
	resourceCounts := map[string]int{
		"routes":    10,
		"backends":  5,
		"policies":  3,
		"gateways":  2,
	}

	RecordSnapshotBuild("test-node", 0.001, 1024, resourceCounts)
	RecordSnapshotBuild("node-2", 0.5, 65536, map[string]int{"routes": 100})
}

func TestRecordSnapshotUpdate(t *testing.T) {
	// This test verifies the function doesn't panic
	RecordSnapshotUpdate("test-node", "config-change")
	RecordSnapshotUpdate("test-node", "watch-event")
	RecordSnapshotUpdate("node-2", "startup")
}

func TestRecordSnapshotError(t *testing.T) {
	// This test verifies the function doesn't panic
	RecordSnapshotError("test-node", "validation")
	RecordSnapshotError("test-node", "build-failed")
	RecordSnapshotError("node-2", "timeout")
}

func TestUpdateAgentStatus(t *testing.T) {
	// This test verifies the function doesn't panic
	UpdateAgentStatus("test-node", "v1.0.0", true)
	UpdateAgentStatus("test-node", "v1.0.0", false)
	UpdateAgentStatus("node-2", "v1.1.0", true)
}

func TestUpdateActiveStreams(t *testing.T) {
	// This test verifies the function doesn't panic
	UpdateActiveStreams(0)
	UpdateActiveStreams(10)
	UpdateActiveStreams(100)
}

func TestUpdateCachedSnapshots(t *testing.T) {
	// This test verifies the function doesn't panic
	UpdateCachedSnapshots(0)
	UpdateCachedSnapshots(50)
	UpdateCachedSnapshots(200)
}
