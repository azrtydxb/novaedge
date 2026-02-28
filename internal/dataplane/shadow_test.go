package dataplane

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	configpb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewShadowComparator(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)
	sc := NewShadowComparator(translator, client, logger)

	if sc == nil {
		t.Fatal("NewShadowComparator returned nil")
	}
	if sc.translator != translator {
		t.Error("translator not set correctly")
	}
	if sc.client != client {
		t.Error("client not set correctly")
	}
}

func TestShadowComparator_StartStop(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)
	sc := NewShadowComparator(translator, client, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc.Start(ctx)

	// Allow goroutine to start.
	time.Sleep(50 * time.Millisecond)

	// Stop should complete without hanging.
	sc.Stop()
}

func TestShadowComparator_SyncConfig(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)
	sc := NewShadowComparator(translator, client, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc.Start(ctx)
	defer sc.Stop()

	snap := &configpb.ConfigSnapshot{
		Version: "v1",
		Gateways: []*configpb.Gateway{
			{
				Name: "gw",
				Listeners: []*configpb.Listener{
					{Name: "http", Port: 80, Protocol: configpb.Protocol_HTTP},
				},
			},
		},
	}

	// SyncConfig should not panic or block.
	sc.SyncConfig(ctx, snap)

	stats := sc.Stats()
	if stats.ConfigSyncs != 1 {
		t.Errorf("config syncs = %d, want %d", stats.ConfigSyncs, 1)
	}
	if stats.ConfigSyncErrors != 0 {
		t.Errorf("config sync errors = %d, want %d", stats.ConfigSyncErrors, 0)
	}
}

func TestShadowComparator_SyncConfigMultiple(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)
	sc := NewShadowComparator(translator, client, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc.Start(ctx)
	defer sc.Stop()

	for i := range 5 {
		snap := &configpb.ConfigSnapshot{
			Version: "v" + string(rune('0'+i)),
		}
		sc.SyncConfig(ctx, snap)
	}

	stats := sc.Stats()
	if stats.ConfigSyncs != 5 {
		t.Errorf("config syncs = %d, want %d", stats.ConfigSyncs, 5)
	}
}

func TestShadowComparator_Stats_Initial(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a shadow comparator without starting it.
	sc := &ShadowComparator{
		logger: logger.Named("shadow"),
	}

	stats := sc.Stats()
	if stats.ConfigSyncs != 0 {
		t.Errorf("initial config syncs = %d, want 0", stats.ConfigSyncs)
	}
	if stats.ConfigSyncErrors != 0 {
		t.Errorf("initial config sync errors = %d, want 0", stats.ConfigSyncErrors)
	}
	if stats.FlowEvents != 0 {
		t.Errorf("initial flow events = %d, want 0", stats.FlowEvents)
	}
	if stats.Discrepancies != 0 {
		t.Errorf("initial discrepancies = %d, want 0", stats.Discrepancies)
	}
}

func TestShadowComparator_StopWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)

	sc := &ShadowComparator{
		logger: logger.Named("shadow"),
	}

	// Stop should not panic when Start was never called.
	sc.Stop()
}

func TestShadowStats_Fields(t *testing.T) {
	stats := ShadowStats{
		ConfigSyncs:      10,
		ConfigSyncErrors: 2,
		FlowEvents:       1000,
		Discrepancies:    5,
	}

	if stats.ConfigSyncs != 10 {
		t.Errorf("ConfigSyncs = %d, want %d", stats.ConfigSyncs, 10)
	}
	if stats.ConfigSyncErrors != 2 {
		t.Errorf("ConfigSyncErrors = %d, want %d", stats.ConfigSyncErrors, 2)
	}
	if stats.FlowEvents != 1000 {
		t.Errorf("FlowEvents = %d, want %d", stats.FlowEvents, 1000)
	}
	if stats.Discrepancies != 5 {
		t.Errorf("Discrepancies = %d, want %d", stats.Discrepancies, 5)
	}
}
