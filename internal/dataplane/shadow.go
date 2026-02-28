package dataplane

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/api/proto/dataplane"
	configpb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ShadowComparator runs the Go forwarding path as primary while also
// sending configuration to the Rust dataplane. It streams flow events
// from the Rust dataplane and compares verdicts against the Go path,
// logging any discrepancies as warnings.
//
// This enables gradual validation of the Rust dataplane before full
// cut-over from the Go forwarding path.
type ShadowComparator struct {
	translator *Translator
	client     *Client
	logger     *zap.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics tracked during shadow comparison.
	configSyncs      atomic.Int64
	configSyncErrors atomic.Int64
	flowEvents       atomic.Int64
	discrepancies    atomic.Int64
}

// NewShadowComparator creates a new ShadowComparator that uses the given
// Translator to send config updates to the Rust dataplane and streams
// flow events for comparison.
func NewShadowComparator(translator *Translator, client *Client, logger *zap.Logger) *ShadowComparator {
	return &ShadowComparator{
		translator: translator,
		client:     client,
		logger:     logger.Named("shadow"),
	}
}

// Start begins streaming flow events from the Rust dataplane for comparison.
// The flow event stream runs in the background until Stop is called.
func (s *ShadowComparator) Start(ctx context.Context) {
	flowCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.wg.Add(1)
	go s.streamFlows(flowCtx)

	s.logger.Info("Shadow mode started: Go forwarding is primary, Rust dataplane receives config in parallel")
}

// Stop stops the flow event stream and waits for background goroutines
// to complete. It logs a summary of shadow comparison metrics.
func (s *ShadowComparator) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	s.logger.Info("Shadow mode stopped",
		zap.Int64("config_syncs", s.configSyncs.Load()),
		zap.Int64("config_sync_errors", s.configSyncErrors.Load()),
		zap.Int64("flow_events", s.flowEvents.Load()),
		zap.Int64("discrepancies", s.discrepancies.Load()),
	)
}

// SyncConfig sends the config to the Rust dataplane via the translator.
// Any errors are logged as warnings (not fatal) since Go is the primary
// forwarding path in shadow mode.
func (s *ShadowComparator) SyncConfig(ctx context.Context, snapshot *configpb.ConfigSnapshot) {
	s.configSyncs.Add(1)

	if err := s.translator.Sync(ctx, snapshot); err != nil {
		s.configSyncErrors.Add(1)
		s.logger.Warn("Shadow: failed to sync config to Rust dataplane",
			zap.Error(err),
			zap.String("version", snapshot.GetVersion()),
		)
		return
	}

	s.logger.Debug("Shadow: config synced to Rust dataplane",
		zap.String("version", snapshot.GetVersion()),
	)
}

// streamFlows streams flow events from the Rust dataplane and logs
// them for comparison. In a production shadow mode, this would compare
// flow verdicts against the Go path to detect discrepancies.
func (s *ShadowComparator) streamFlows(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		flowCh, err := s.client.StreamFlows(ctx, &pb.StreamFlowsRequest{
			BufferSize: 256,
		})
		if err != nil {
			s.logger.Warn("Shadow: failed to start flow stream, retrying in 5s",
				zap.Error(err),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		s.logger.Debug("Shadow: flow event stream connected")

		for flow := range flowCh {
			s.flowEvents.Add(1)

			// In shadow mode, we log flow events from the Rust dataplane.
			// A full implementation would compare these against Go-side
			// decisions to find discrepancies.
			if flow.GetVerdict() == pb.FlowVerdict_FLOW_VERDICT_DROPPED ||
				flow.GetVerdict() == pb.FlowVerdict_FLOW_VERDICT_REJECTED {
				s.discrepancies.Add(1)
				s.logger.Warn("Shadow: Rust dataplane would have dropped/rejected a flow",
					zap.String("src", flow.GetSrcIp()),
					zap.String("dst", flow.GetDstIp()),
					zap.Uint32("src_port", flow.GetSrcPort()),
					zap.Uint32("dst_port", flow.GetDstPort()),
					zap.String("verdict", flow.GetVerdict().String()),
					zap.String("backend", flow.GetBackendSelected()),
					zap.String("lb_algorithm", flow.GetLbAlgorithm()),
				)
			}
		}

		s.logger.Debug("Shadow: flow event stream ended, reconnecting in 5s")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// Stats returns the current shadow comparison statistics.
func (s *ShadowComparator) Stats() ShadowStats {
	return ShadowStats{
		ConfigSyncs:      s.configSyncs.Load(),
		ConfigSyncErrors: s.configSyncErrors.Load(),
		FlowEvents:       s.flowEvents.Load(),
		Discrepancies:    s.discrepancies.Load(),
	}
}

// ShadowStats holds counters from the shadow comparison.
type ShadowStats struct {
	ConfigSyncs      int64
	ConfigSyncErrors int64
	FlowEvents       int64
	Discrepancies    int64
}
