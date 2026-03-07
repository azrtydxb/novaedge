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

package federation

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// TestIntegrationTwoServersExchangeResources verifies that two federation
// Server instances with in-memory state can record and apply resource changes
// between each other. This simulates the bidirectional sync flow without
// requiring a real gRPC transport.
func TestIntegrationTwoServersExchangeResources(t *testing.T) {
	logger := zap.NewNop()

	configA := DefaultConfig()
	configA.FederationID = "integration-test"
	configA.LocalMember = &PeerInfo{Name: "server-a", Endpoint: "localhost:50051"}
	configA.Peers = []*PeerInfo{{Name: "server-b", Endpoint: "localhost:50052"}}
	configA.ConflictResolutionStrategy = StrategyLastWriterWins

	configB := DefaultConfig()
	configB.FederationID = "integration-test"
	configB.LocalMember = &PeerInfo{Name: "server-b", Endpoint: "localhost:50052"}
	configB.Peers = []*PeerInfo{{Name: "server-a", Endpoint: "localhost:50051"}}
	configB.ConflictResolutionStrategy = StrategyLastWriterWins

	serverA := NewServer(configA, logger)
	serverB := NewServer(configB, logger)

	// Server A records a local change
	key := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw-1"}
	serverA.RecordLocalChange(key, ChangeTypeCreated, []byte(`{"name":"gw-1"}`), map[string]string{"env": "prod"})

	// Verify server A has the resource
	val, ok := serverA.resources.Load(key.String())
	if !ok {
		t.Fatal("serverA should have the resource after RecordLocalChange")
	}
	resA, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type in resources map")
	}
	if resA.OriginMember != "server-a" {
		t.Errorf("OriginMember = %q, want server-a", resA.OriginMember)
	}

	// Simulate propagation: convert serverA's change to a protobuf ResourceChange
	// and apply it on serverB via handleResourceChange
	change := &pb.ResourceChange{
		ChangeId:        "test-change-1",
		VectorClock:     resA.VectorClock,
		ChangeType:      pb.ChangeType_CREATED,
		ResourceType:    key.Kind,
		Namespace:       key.Namespace,
		Name:            key.Name,
		ResourceVersion: resA.ResourceVersion,
		ResourceData:    resA.Data,
		ResourceHash:    resA.Hash,
		Timestamp:       resA.LastModified.UnixNano(),
		OriginMember:    resA.OriginMember,
		Labels:          resA.Labels,
	}

	ctx := context.Background()
	if err := serverB.handleResourceChange(ctx, "server-a", change); err != nil {
		t.Fatalf("serverB.handleResourceChange failed: %v", err)
	}

	// Verify server B now has the resource
	val, ok = serverB.resources.Load(key.String())
	if !ok {
		t.Fatal("serverB should have the resource after handleResourceChange")
	}
	resB, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type in resources map on serverB")
	}
	if resB.OriginMember != "server-a" {
		t.Errorf("serverB resource OriginMember = %q, want server-a", resB.OriginMember)
	}
	if string(resB.Data) != `{"name":"gw-1"}` {
		t.Errorf("serverB resource data = %q, want %q", string(resB.Data), `{"name":"gw-1"}`)
	}

	// Verify stats
	statsB := serverB.GetStats()
	if statsB.TotalChangesReceived != 1 {
		t.Errorf("serverB TotalChangesReceived = %d, want 1", statsB.TotalChangesReceived)
	}
}

// TestIntegrationVectorClockUpdatesOnSync verifies that vector clocks are
// correctly merged when resources flow between two servers.
func TestIntegrationVectorClockUpdatesOnSync(t *testing.T) {
	logger := zap.NewNop()

	configA := DefaultConfig()
	configA.FederationID = "vc-test"
	configA.LocalMember = &PeerInfo{Name: "server-a", Endpoint: "localhost:50051"}

	configB := DefaultConfig()
	configB.FederationID = "vc-test"
	configB.LocalMember = &PeerInfo{Name: "server-b", Endpoint: "localhost:50052"}

	serverA := NewServer(configA, logger)
	serverB := NewServer(configB, logger)

	// Server A records two changes to increment its clock
	key1 := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "route-1"}
	key2 := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "route-2"}
	serverA.RecordLocalChange(key1, ChangeTypeCreated, []byte(`{"route":1}`), nil)
	serverA.RecordLocalChange(key2, ChangeTypeCreated, []byte(`{"route":2}`), nil)

	// Server A's vector clock should have server-a >= 3 (1 from init + 2 from changes)
	vcA := serverA.vectorClock.Get("server-a")
	if vcA < 3 {
		t.Errorf("server-a vector clock = %d, want >= 3", vcA)
	}

	// Get the resource to propagate
	val, ok := serverA.resources.Load(key1.String())
	if !ok {
		t.Fatal("serverA should have route-1")
	}
	resA, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type for route-1")
	}

	// Propagate to server B
	change := &pb.ResourceChange{
		ChangeId:     "vc-change-1",
		VectorClock:  resA.VectorClock,
		ChangeType:   pb.ChangeType_CREATED,
		ResourceType: key1.Kind,
		Namespace:    key1.Namespace,
		Name:         key1.Name,
		ResourceData: resA.Data,
		Timestamp:    resA.LastModified.UnixNano(),
		OriginMember: resA.OriginMember,
	}

	ctx := context.Background()
	if err := serverB.handleResourceChange(ctx, "server-a", change); err != nil {
		t.Fatalf("serverB.handleResourceChange failed: %v", err)
	}

	// Server B's vector clock should now include server-a's value
	vcBForA := serverB.vectorClock.Get("server-a")
	if vcBForA < 2 {
		t.Errorf("serverB vectorClock[server-a] = %d, want >= 2", vcBForA)
	}

	// Server B's own clock should still show its initial value
	vcBForB := serverB.vectorClock.Get("server-b")
	if vcBForB < 1 {
		t.Errorf("serverB vectorClock[server-b] = %d, want >= 1", vcBForB)
	}
}

// TestIntegrationAntiEntropyDetectsDrift verifies that the anti-entropy
// mechanism can detect when one server has a resource that the other does not,
// using direct in-memory merkle tree comparison.
func TestIntegrationAntiEntropyDetectsDrift(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "ae-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}

	server := NewServer(config, logger)

	// Add a resource to the server
	key := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw-drift"}
	server.RecordLocalChange(key, ChangeTypeCreated, []byte(`{"drifted":true}`), nil)

	// Create an anti-entropy manager and rebuild its local tree
	aeConfig := DefaultAntiEntropyConfig()
	aeMgr := NewAntiEntropyManager(aeConfig, server, nil, logger)
	aeMgr.rebuildLocalTree()

	// Build a "peer" tree that is empty (simulating a peer with no resources)
	peerTree := NewMerkleTree(MerkleTreeDepth)

	// Compare: should detect that the peer is missing the resource
	aeMgr.mu.RLock()
	diffs := aeMgr.localTree.Compare(peerTree)
	aeMgr.mu.RUnlock()

	if len(diffs) == 0 {
		t.Fatal("anti-entropy should detect drift when peer is missing resources")
	}

	found := false
	for _, diff := range diffs {
		if diff == key.String() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("diffs should contain %q, got %v", key.String(), diffs)
	}

	// Now add the same resource to the peer tree and verify no drift
	val, ok := server.resources.Load(key.String())
	if !ok {
		t.Fatal("server should have the resource")
	}
	res, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type for resource")
	}
	peerTree.Update(key.String(), res.Hash)

	aeMgr.mu.RLock()
	diffs = aeMgr.localTree.Compare(peerTree)
	aeMgr.mu.RUnlock()

	if len(diffs) != 0 {
		t.Errorf("no drift expected after adding resource to peer, got %v", diffs)
	}
}

// TestIntegrationConflictResolutionLastWriterWins verifies that when two
// servers have concurrent changes to the same resource, the LastWriterWins
// strategy picks the resource with the later timestamp.
func TestIntegrationConflictResolutionLastWriterWins(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "conflict-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}
	config.ConflictResolutionStrategy = StrategyLastWriterWins

	server := NewServer(config, logger)

	// Store a local resource with an older timestamp
	key := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "route-conflict"}
	localTimestamp := time.Now().Add(-10 * time.Minute)

	localResource := &TrackedResource{
		Key:          key,
		Data:         []byte(`{"version":"local"}`),
		VectorClock:  map[string]int64{"local": 2, "remote": 1},
		OriginMember: "local",
		LastModified: localTimestamp,
		Hash:         "local-hash",
	}
	server.resources.Store(key.String(), localResource)

	// Incoming remote change with a newer timestamp but concurrent vector clock
	remoteTimestamp := time.Now()
	remoteChange := &pb.ResourceChange{
		ChangeId:     "conflict-change-1",
		VectorClock:  map[string]int64{"local": 1, "remote": 3},
		ChangeType:   pb.ChangeType_UPDATED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		ResourceData: []byte(`{"version":"remote"}`),
		ResourceHash: "remote-hash",
		Timestamp:    remoteTimestamp.UnixNano(),
		OriginMember: "remote-peer",
	}

	ctx := context.Background()
	if err := server.handleResourceChange(ctx, "remote-peer", remoteChange); err != nil {
		t.Fatalf("handleResourceChange failed: %v", err)
	}

	// Verify remote won because it has a newer timestamp
	val, ok := server.resources.Load(key.String())
	if !ok {
		t.Fatal("resource should exist after conflict resolution")
	}
	resolved, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type for resolved resource")
	}
	if string(resolved.Data) != `{"version":"remote"}` {
		t.Errorf("expected remote version to win, got data = %q", string(resolved.Data))
	}
	if resolved.OriginMember != "remote-peer" {
		t.Errorf("OriginMember = %q, want remote-peer", resolved.OriginMember)
	}

	// Verify conflict was detected and resolved
	stats := server.GetStats()
	if stats.ConflictsDetected < 1 {
		t.Errorf("ConflictsDetected = %d, want >= 1", stats.ConflictsDetected)
	}
	if stats.ConflictsResolved < 1 {
		t.Errorf("ConflictsResolved = %d, want >= 1", stats.ConflictsResolved)
	}
}

// TestIntegrationConflictResolutionLocalWins verifies that LastWriterWins
// keeps the local version when it has a newer timestamp.
func TestIntegrationConflictResolutionLocalWins(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "conflict-local-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}
	config.ConflictResolutionStrategy = StrategyLastWriterWins

	server := NewServer(config, logger)

	// Store a local resource with a newer timestamp
	key := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "route-local-wins"}
	localTimestamp := time.Now()

	localResource := &TrackedResource{
		Key:          key,
		Data:         []byte(`{"version":"local"}`),
		VectorClock:  map[string]int64{"local": 3, "remote": 1},
		OriginMember: "local",
		LastModified: localTimestamp,
		Hash:         "local-hash",
	}
	server.resources.Store(key.String(), localResource)

	// Incoming remote change with an older timestamp but concurrent vector clock
	remoteTimestamp := time.Now().Add(-10 * time.Minute)
	remoteChange := &pb.ResourceChange{
		ChangeId:     "conflict-local-change",
		VectorClock:  map[string]int64{"local": 1, "remote": 5},
		ChangeType:   pb.ChangeType_UPDATED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		ResourceData: []byte(`{"version":"remote"}`),
		ResourceHash: "remote-hash",
		Timestamp:    remoteTimestamp.UnixNano(),
		OriginMember: "remote-peer",
	}

	ctx := context.Background()
	if err := server.handleResourceChange(ctx, "remote-peer", remoteChange); err != nil {
		t.Fatalf("handleResourceChange failed: %v", err)
	}

	// Verify local won because it has a newer timestamp
	val, ok := server.resources.Load(key.String())
	if !ok {
		t.Fatal("resource should exist after conflict resolution")
	}
	resolved, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type for resolved resource")
	}
	if string(resolved.Data) != `{"version":"local"}` {
		t.Errorf("expected local version to win, got data = %q", string(resolved.Data))
	}
}

// TestIntegrationResourceDeletion verifies that deleting a resource on one
// server and propagating the deletion to another creates a proper tombstone.
func TestIntegrationResourceDeletion(t *testing.T) {
	logger := zap.NewNop()

	configA := DefaultConfig()
	configA.FederationID = "delete-test"
	configA.LocalMember = &PeerInfo{Name: "server-a", Endpoint: "localhost:50051"}

	configB := DefaultConfig()
	configB.FederationID = "delete-test"
	configB.LocalMember = &PeerInfo{Name: "server-b", Endpoint: "localhost:50052"}

	serverA := NewServer(configA, logger)
	serverB := NewServer(configB, logger)

	// First create a resource on both servers
	key := ResourceKey{Kind: "ProxyBackend", Namespace: "default", Name: "backend-to-delete"}
	serverA.RecordLocalChange(key, ChangeTypeCreated, []byte(`{"backend":1}`), nil)

	// Get the resource to propagate initial create to server B
	val, _ := serverA.resources.Load(key.String())
	resA, _ := val.(*TrackedResource)

	ctx := context.Background()
	createChange := &pb.ResourceChange{
		ChangeId:     "create-then-delete",
		VectorClock:  resA.VectorClock,
		ChangeType:   pb.ChangeType_CREATED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		ResourceData: resA.Data,
		Timestamp:    resA.LastModified.UnixNano(),
		OriginMember: "server-a",
	}
	if err := serverB.handleResourceChange(ctx, "server-a", createChange); err != nil {
		t.Fatalf("serverB create failed: %v", err)
	}

	// Now delete on server A
	serverA.RecordLocalChange(key, ChangeTypeDeleted, nil, nil)

	// Verify tombstone exists on server A
	_, hasTombstone := serverA.tombstones.Load(key.String())
	if !hasTombstone {
		t.Error("serverA should have a tombstone after deletion")
	}

	// Verify resource is gone from server A
	_, hasResource := serverA.resources.Load(key.String())
	if hasResource {
		t.Error("serverA should not have the resource after deletion")
	}

	// Propagate deletion to server B
	deleteChange := &pb.ResourceChange{
		ChangeId:     "delete-propagated",
		VectorClock:  serverA.vectorClock.ToMap(),
		ChangeType:   pb.ChangeType_DELETED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		Timestamp:    time.Now().UnixNano(),
		OriginMember: "server-a",
	}
	if err := serverB.handleResourceChange(ctx, "server-a", deleteChange); err != nil {
		t.Fatalf("serverB delete failed: %v", err)
	}

	// Verify resource is gone and tombstone exists on server B
	_, hasResource = serverB.resources.Load(key.String())
	if hasResource {
		t.Error("serverB should not have the resource after deletion propagation")
	}
	_, hasTombstone = serverB.tombstones.Load(key.String())
	if !hasTombstone {
		t.Error("serverB should have a tombstone after deletion propagation")
	}
}

// TestIntegrationResourceChangeCallbacks verifies that registered change
// callbacks are invoked when resources are applied from a remote peer.
func TestIntegrationResourceChangeCallbacks(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "callback-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}

	server := NewServer(config, logger)

	callbackCh := make(chan ResourceKey, 1)
	server.OnChange(func(key ResourceKey, _ ChangeType, _ []byte) {
		callbackCh <- key
	})

	key := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "callback-gw"}
	change := &pb.ResourceChange{
		ChangeId:     "callback-change",
		VectorClock:  map[string]int64{"remote": 5},
		ChangeType:   pb.ChangeType_CREATED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		ResourceData: []byte(`{"callback":"test"}`),
		Timestamp:    time.Now().UnixNano(),
		OriginMember: "remote-peer",
	}

	ctx := context.Background()
	if err := server.handleResourceChange(ctx, "remote-peer", change); err != nil {
		t.Fatalf("handleResourceChange failed: %v", err)
	}

	select {
	case received := <-callbackCh:
		if received != key {
			t.Errorf("callback received key %v, want %v", received, key)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for change callback")
	}
}

// TestIntegrationOlderChangeIgnored verifies that a change with an older
// vector clock is ignored when we already have a newer version.
func TestIntegrationOlderChangeIgnored(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "stale-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}

	server := NewServer(config, logger)

	key := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "route-stale"}

	// Store a resource with a higher vector clock
	newerResource := &TrackedResource{
		Key:          key,
		Data:         []byte(`{"version":"newer"}`),
		VectorClock:  map[string]int64{"local": 10, "remote": 5},
		OriginMember: "local",
		LastModified: time.Now(),
		Hash:         "newer-hash",
	}
	server.resources.Store(key.String(), newerResource)

	// Try to apply an older change
	olderChange := &pb.ResourceChange{
		ChangeId:     "stale-change",
		VectorClock:  map[string]int64{"local": 5, "remote": 3},
		ChangeType:   pb.ChangeType_UPDATED,
		ResourceType: key.Kind,
		Namespace:    key.Namespace,
		Name:         key.Name,
		ResourceData: []byte(`{"version":"older"}`),
		Timestamp:    time.Now().Add(-time.Hour).UnixNano(),
		OriginMember: "remote-peer",
	}

	ctx := context.Background()
	if err := server.handleResourceChange(ctx, "remote-peer", olderChange); err != nil {
		t.Fatalf("handleResourceChange should not return error for older change: %v", err)
	}

	// Verify the newer version is still stored
	val, ok := server.resources.Load(key.String())
	if !ok {
		t.Fatal("resource should still exist")
	}
	res, ok := val.(*TrackedResource)
	if !ok {
		t.Fatal("unexpected type for resource")
	}
	if string(res.Data) != `{"version":"newer"}` {
		t.Errorf("expected newer version to be kept, got data = %q", string(res.Data))
	}
}

// TestExtractResourceChanges_CreatedUpdatedUnchanged verifies that
// ExtractResourceChanges correctly distinguishes between CREATED, UPDATED,
// and unchanged resources using content hashing.
func TestExtractResourceChanges_CreatedUpdatedUnchanged(t *testing.T) {
	// Baseline has one gateway and one route
	baseline := &pb.ConfigSnapshot{
		Gateways: []*pb.Gateway{
			{Namespace: "default", Name: "gw-1"},
		},
		Routes: []*pb.Route{
			{Namespace: "default", Name: "route-1", Hostnames: []string{"api.example.com"}},
		},
		FederationMetadata: &pb.FederationMetadata{
			VectorClock: map[string]int64{"ctrl-a": 1},
		},
	}

	// Current has:
	// - gw-1 unchanged (same content)
	// - route-1 updated (different PathPrefix)
	// - gw-2 created (new)
	current := &pb.ConfigSnapshot{
		Gateways: []*pb.Gateway{
			{Namespace: "default", Name: "gw-1"}, // unchanged
			{Namespace: "default", Name: "gw-2"}, // new
		},
		Routes: []*pb.Route{
			{Namespace: "default", Name: "route-1", Hostnames: []string{"api-v2.example.com"}}, // updated
		},
		FederationMetadata: &pb.FederationMetadata{
			VectorClock: map[string]int64{"ctrl-a": 2},
		},
	}

	changes := ExtractResourceChanges(current, baseline)

	// Categorize changes
	changeMap := make(map[string]pb.ChangeType)
	for _, c := range changes {
		key := c.ResourceType + "/" + c.Namespace + "/" + c.Name
		changeMap[key] = c.ChangeType
	}

	// gw-1 should NOT appear (unchanged)
	if _, ok := changeMap["ProxyGateway/default/gw-1"]; ok {
		t.Error("gw-1 should be skipped (unchanged), but was included in changes")
	}

	// gw-2 should be CREATED
	if ct, ok := changeMap["ProxyGateway/default/gw-2"]; !ok {
		t.Error("gw-2 should be CREATED but not found in changes")
	} else if ct != pb.ChangeType_CREATED {
		t.Errorf("gw-2 change type = %v, want CREATED", ct)
	}

	// route-1 should be UPDATED
	if ct, ok := changeMap["ProxyRoute/default/route-1"]; !ok {
		t.Error("route-1 should be UPDATED but not found in changes")
	} else if ct != pb.ChangeType_UPDATED {
		t.Errorf("route-1 change type = %v, want UPDATED", ct)
	}
}

// TestExtractResourceChanges_NilBaseline verifies that when no baseline is
// provided, all current resources are marked as CREATED.
func TestExtractResourceChanges_NilBaseline(t *testing.T) {
	current := &pb.ConfigSnapshot{
		Gateways: []*pb.Gateway{
			{Namespace: "default", Name: "gw-1"},
		},
		Policies: []*pb.Policy{
			{Namespace: "default", Name: "pol-1"},
		},
		FederationMetadata: &pb.FederationMetadata{
			VectorClock: map[string]int64{"ctrl-a": 1},
		},
	}

	changes := ExtractResourceChanges(current, nil)

	for _, c := range changes {
		if c.ChangeType != pb.ChangeType_CREATED {
			t.Errorf("with nil baseline, %s/%s should be CREATED, got %v",
				c.ResourceType, c.Name, c.ChangeType)
		}
	}
	if len(changes) != 2 {
		t.Errorf("expected 2 changes (1 gateway + 1 policy), got %d", len(changes))
	}
}

// TestExtractResourceChanges_Deletions verifies that resources in baseline
// but not in current are marked as DELETED.
func TestExtractResourceChanges_Deletions(t *testing.T) {
	baseline := &pb.ConfigSnapshot{
		Gateways: []*pb.Gateway{
			{Namespace: "default", Name: "gw-1"},
			{Namespace: "default", Name: "gw-deleted"},
		},
		FederationMetadata: &pb.FederationMetadata{
			VectorClock: map[string]int64{"ctrl-a": 1},
		},
	}

	current := &pb.ConfigSnapshot{
		Gateways: []*pb.Gateway{
			{Namespace: "default", Name: "gw-1"},
		},
		FederationMetadata: &pb.FederationMetadata{
			VectorClock: map[string]int64{"ctrl-a": 2},
		},
	}

	changes := ExtractResourceChanges(current, baseline)

	var foundDeleted bool
	for _, c := range changes {
		if c.Name == "gw-deleted" && c.ChangeType == pb.ChangeType_DELETED {
			foundDeleted = true
		}
		// gw-1 should be skipped (unchanged)
		if c.Name == "gw-1" {
			t.Error("gw-1 should be skipped (unchanged) but was included")
		}
	}
	if !foundDeleted {
		t.Error("expected DELETED change for gw-deleted")
	}
}

// TestIntegrationServiceEndpointsCache verifies that ServiceEndpoints changes
// are stored in the remote endpoint cache.
func TestIntegrationServiceEndpointsCache(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultConfig()
	config.FederationID = "ep-cache-test"
	config.LocalMember = &PeerInfo{Name: "local", Endpoint: "localhost:50051"}

	server := NewServer(config, logger)

	// Build a ServiceEndpoints proto message
	svcEP := &pb.ServiceEndpoints{
		ClusterName: "remote-cluster",
		Namespace:   "default",
		ServiceName: "my-service",
		Region:      "us-west-2",
		Zone:        "us-west-2a",
		Endpoints: []*pb.Endpoint{
			{Address: "10.1.0.1", Port: 8080, Ready: true},
			{Address: "10.1.0.2", Port: 8080, Ready: true},
		},
	}

	data, err := proto.Marshal(svcEP)
	if err != nil {
		t.Fatalf("failed to marshal ServiceEndpoints: %v", err)
	}

	change := &pb.ResourceChange{
		ChangeId:     "ep-change",
		VectorClock:  map[string]int64{"remote": 1},
		ChangeType:   pb.ChangeType_CREATED,
		ResourceType: "ServiceEndpoints",
		Namespace:    "default",
		Name:         "my-service",
		ResourceData: data,
		Timestamp:    time.Now().UnixNano(),
		OriginMember: "remote-cluster",
	}

	ctx := context.Background()
	if err := server.handleResourceChange(ctx, "remote-cluster", change); err != nil {
		t.Fatalf("handleResourceChange for ServiceEndpoints failed: %v", err)
	}

	// Verify the endpoint cache has the data
	cache := server.GetEndpointCache()
	eps := cache.GetForService("default", "my-service")
	if len(eps) != 1 {
		t.Fatalf("expected 1 ServiceEndpoints entry, got %d", len(eps))
	}
	if eps[0].ClusterName != "remote-cluster" {
		t.Errorf("ClusterName = %q, want remote-cluster", eps[0].ClusterName)
	}
	if len(eps[0].Endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(eps[0].Endpoints))
	}
}
