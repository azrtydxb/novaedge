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

package introspection

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewSnapshotHolder(t *testing.T) {
	holder := NewSnapshotHolder()
	require.NotNil(t, holder)
	assert.Nil(t, holder.GetCurrentSnapshot())
}

func TestSnapshotHolder_StoreAndGet(t *testing.T) {
	holder := NewSnapshotHolder()

	// Initially nil
	assert.Nil(t, holder.GetCurrentSnapshot())

	// Store a snapshot
	snap := &pb.ConfigSnapshot{
		Version: "v1",
	}
	holder.Store(snap)

	// Get should return the stored snapshot
	got := holder.GetCurrentSnapshot()
	require.NotNil(t, got)
	assert.Equal(t, "v1", got.Version)
}

func TestSnapshotHolder_Overwrite(t *testing.T) {
	holder := NewSnapshotHolder()

	// Store first snapshot
	snap1 := &pb.ConfigSnapshot{
		Version: "v1",
	}
	holder.Store(snap1)
	assert.Equal(t, "v1", holder.GetCurrentSnapshot().Version)

	// Overwrite with second snapshot
	snap2 := &pb.ConfigSnapshot{
		Version: "v2",
	}
	holder.Store(snap2)
	assert.Equal(t, "v2", holder.GetCurrentSnapshot().Version)
}

func TestSnapshotHolder_ConcurrentAccess(t *testing.T) {
	holder := NewSnapshotHolder()
	var wg sync.WaitGroup
	numOps := 100

	// Concurrent writes
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			snap := &pb.ConfigSnapshot{
				Version: "v" + string(rune('0'+idx%10)),
			}
			holder.Store(snap)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = holder.GetCurrentSnapshot()
		}()
	}

	wg.Wait()

	// Should have some snapshot stored
	assert.NotNil(t, holder.GetCurrentSnapshot())
}

func TestSnapshotHolder_StoreNil(t *testing.T) {
	holder := NewSnapshotHolder()

	// Store a snapshot first
	snap := &pb.ConfigSnapshot{
		Version: "v1",
	}
	holder.Store(snap)
	assert.NotNil(t, holder.GetCurrentSnapshot())

	// Store nil
	holder.Store(nil)
	assert.Nil(t, holder.GetCurrentSnapshot())
}
