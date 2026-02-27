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

package config

import (
	"errors"
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/piwi3910/novaedge/internal/pkg/grpclimits"
	"github.com/piwi3910/novaedge/internal/pkg/tlsutil"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)
var (
	errUnknownState = errors.New("unknown state")
	errConfigPersistenceNotInitialized = errors.New("config persistence not initialized")
	errNoCachedConfigAvailable = errors.New("no cached config available")
)


// FailoverState represents the current state of the failover state machine
type FailoverState string

const (
	// StateConnectedPrimary - connected to primary controller
	StateConnectedPrimary FailoverState = "ConnectedPrimary"

	// StateFailingOver - attempting to failover to secondary
	StateFailingOver FailoverState = "FailingOver"

	// StateConnectedSecondary - connected to a secondary controller
	StateConnectedSecondary FailoverState = "ConnectedSecondary"

	// StateRecoveryCheck - checking if primary is available again
	StateRecoveryCheck FailoverState = "RecoveryCheck"

	// StateAutonomous - operating without controller connection
	StateAutonomous FailoverState = "Autonomous"
)

// ControllerEndpoint represents a controller endpoint for failover
type ControllerEndpoint struct {
	// Name is the controller's name
	Name string

	// Endpoint is the gRPC address (host:port)
	Endpoint string

	// Priority (lower = higher priority, 0 = primary)
	Priority int32

	// Region is the geographic region
	Region string

	// Zone is the availability zone
	Zone string

	// LastHealthy is when we last successfully communicated
	LastHealthy time.Time

	// FailureCount is the number of consecutive failures
	FailureCount int32

	// Latency is the last measured latency
	Latency time.Duration
}

// FailoverConfig configures the failover behavior
type FailoverConfig struct {
	// Timeout before initiating failover
	Timeout time.Duration

	// HealthCheckInterval is how often to check controller health
	HealthCheckInterval time.Duration

	// FailureThreshold is failures before marking controller unhealthy
	FailureThreshold int32

	// RecoveryDelay is how long to wait before checking for primary recovery
	RecoveryDelay time.Duration

	// LatencyAware enables latency-based controller selection
	LatencyAware bool

	// AutonomousModeEnabled enables autonomous operation when no controllers available
	AutonomousModeEnabled bool

	// ConfigPersistPath is where to persist config for autonomous mode
	ConfigPersistPath string
}

// DefaultFailoverConfig returns sensible defaults
func DefaultFailoverConfig() *FailoverConfig {
	return &FailoverConfig{
		Timeout:               30 * time.Second,
		HealthCheckInterval:   10 * time.Second,
		FailureThreshold:      3,
		RecoveryDelay:         60 * time.Second,
		LatencyAware:          true,
		AutonomousModeEnabled: true,
		ConfigPersistPath:     "/var/lib/novaedge/config-cache",
	}
}

// FailoverWatcher manages connections to multiple controllers with failover
type FailoverWatcher struct {
	// Configuration
	nodeName     string
	agentVersion string
	tlsConfig    *TLSConfig
	logger       *zap.Logger
	config       *FailoverConfig

	// Controllers
	controllers   []*ControllerEndpoint
	controllersMu sync.RWMutex

	// Current state
	state      FailoverState
	stateMu    sync.RWMutex
	activeConn *grpc.ClientConn
	activeCtrl *ControllerEndpoint

	// Current config
	currentSnapshot *Snapshot
	currentVersion  string
	lastVectorClock map[string]int64
	snapshotMu      sync.RWMutex

	// Context management
	ctx            context.Context
	cancel         context.CancelFunc
	healthCheckCtx context.Context
	healthCancel   context.CancelFunc

	// Remote cluster config
	clusterConfig *ClusterConfig

	// Config persistence for autonomous mode
	persistence *Persistence

	// Callbacks
	onStateChange func(FailoverState, *ControllerEndpoint)
	onSnapshot    ApplyFunc

	// Metrics
	failoverCount       int64
	connectionErrors    int64
	autonomousStartTime time.Time
	autonomousDuration  time.Duration

	// Controller reachability tracking (for agent-assisted quorum)
	controllerReachability map[string]*controllerReachabilityInfo
	reachabilityMu         sync.RWMutex
}

// controllerReachabilityInfo tracks reachability to a specific controller
type controllerReachabilityInfo struct {
	Name        string
	Endpoint    string
	Reachable   bool
	LastContact time.Time
	Latency     time.Duration
}

// NewFailoverWatcher creates a new failover-aware config watcher
func NewFailoverWatcher(
	ctx context.Context,
	nodeName, agentVersion string,
	controllers []*ControllerEndpoint,
	tlsConfig *TLSConfig,
	clusterConfig *ClusterConfig,
	config *FailoverConfig,
	logger *zap.Logger,
) *FailoverWatcher {
	if config == nil {
		config = DefaultFailoverConfig()
	}

	// Sort controllers by priority
	sortedControllers := make([]*ControllerEndpoint, len(controllers))
	copy(sortedControllers, controllers)
	sort.Slice(sortedControllers, func(i, j int) bool {
		return sortedControllers[i].Priority < sortedControllers[j].Priority
	})

	ctx, cancel := context.WithCancel(ctx)

	// Initialize controller reachability tracking
	reachability := make(map[string]*controllerReachabilityInfo)
	for _, ctrl := range sortedControllers {
		reachability[ctrl.Name] = &controllerReachabilityInfo{
			Name:      ctrl.Name,
			Endpoint:  ctrl.Endpoint,
			Reachable: false,
		}
	}

	fw := &FailoverWatcher{
		nodeName:               nodeName,
		agentVersion:           agentVersion,
		controllers:            sortedControllers,
		tlsConfig:              tlsConfig,
		clusterConfig:          clusterConfig,
		config:                 config,
		logger:                 logger.Named("failover"),
		state:                  StateConnectedPrimary,
		ctx:                    ctx,
		cancel:                 cancel,
		controllerReachability: reachability,
	}

	// Initialize config persistence if path is configured
	if config.ConfigPersistPath != "" {
		persistence, err := NewPersistence(config.ConfigPersistPath, logger)
		if err != nil {
			logger.Warn("Failed to initialize config persistence, autonomous mode may not work",
				zap.Error(err),
			)
		} else {
			fw.persistence = persistence
		}
	}

	return fw
}

// Start begins the failover watcher
func (w *FailoverWatcher) Start(applyFunc ApplyFunc) error {
	w.onSnapshot = applyFunc

	w.logger.Info("Starting failover watcher",
		zap.Int("controllers", len(w.controllers)),
		zap.Duration("timeout", w.config.Timeout),
	)

	// Start health checker
	w.healthCheckCtx, w.healthCancel = context.WithCancel(w.ctx)
	go w.runHealthChecker()

	// Main connection loop
	for {
		select {
		case <-w.ctx.Done():
			w.cleanup()
			return w.ctx.Err()
		default:
		}

		if err := w.runStateMachine(); err != nil {
			w.logger.Error("State machine error", zap.Error(err))
			select {
			case <-w.ctx.Done():
				w.cleanup()
				return w.ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

// Stop stops the failover watcher
func (w *FailoverWatcher) Stop() {
	w.cancel()
}

// GetState returns the current failover state
func (w *FailoverWatcher) GetState() FailoverState {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.state
}

// GetActiveController returns the currently active controller
func (w *FailoverWatcher) GetActiveController() *ControllerEndpoint {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.activeCtrl
}

// OnStateChange sets a callback for state changes
func (w *FailoverWatcher) OnStateChange(fn func(FailoverState, *ControllerEndpoint)) {
	w.onStateChange = fn
}

// UpdateControllers updates the list of available controllers
func (w *FailoverWatcher) UpdateControllers(controllers []*ControllerEndpoint) {
	w.controllersMu.Lock()
	defer w.controllersMu.Unlock()

	// Sort by priority
	sortedControllers := make([]*ControllerEndpoint, len(controllers))
	copy(sortedControllers, controllers)
	sort.Slice(sortedControllers, func(i, j int) bool {
		return sortedControllers[i].Priority < sortedControllers[j].Priority
	})

	w.controllers = sortedControllers
	w.logger.Info("Updated controller list", zap.Int("count", len(controllers)))
}

// runStateMachine runs the failover state machine
func (w *FailoverWatcher) runStateMachine() error {
	state := w.GetState()

	switch state {
	case StateConnectedPrimary:
		return w.handleConnectedPrimary()

	case StateFailingOver:
		return w.handleFailingOver()

	case StateConnectedSecondary:
		return w.handleConnectedSecondary()

	case StateRecoveryCheck:
		return w.handleRecoveryCheck()

	case StateAutonomous:
		return w.handleAutonomous()

	default:
		return fmt.Errorf("%w: %s", errUnknownState, state)
	}
}

// handleConnectedPrimary handles the connected-to-primary state
func (w *FailoverWatcher) handleConnectedPrimary() error {
	primary := w.getPrimaryController()
	if primary == nil {
		w.transitionTo(StateFailingOver, nil)
		return nil
	}

	// Try to connect and stream
	if err := w.connectAndStream(primary); err != nil {
		w.logger.Warn("Primary connection failed",
			zap.String("controller", primary.Name),
			zap.Error(err),
		)

		primary.FailureCount++
		if primary.FailureCount >= w.config.FailureThreshold {
			w.transitionTo(StateFailingOver, nil)
		}
		return nil
	}

	return nil
}

// handleFailingOver handles the failover state
func (w *FailoverWatcher) handleFailingOver() error {
	w.failoverCount++

	w.controllersMu.RLock()
	controllers := w.controllers
	w.controllersMu.RUnlock()

	// Try each controller in priority order
	for _, ctrl := range controllers {
		if ctrl.FailureCount >= w.config.FailureThreshold {
			continue // Skip unhealthy controllers
		}

		w.logger.Info("Attempting failover to controller",
			zap.String("controller", ctrl.Name),
			zap.Int32("priority", ctrl.Priority),
		)

		if err := w.connectAndStream(ctrl); err != nil {
			w.logger.Warn("Failover connection failed",
				zap.String("controller", ctrl.Name),
				zap.Error(err),
			)
			ctrl.FailureCount++
			continue
		}

		// Successfully connected
		if ctrl.Priority == 0 {
			w.transitionTo(StateConnectedPrimary, ctrl)
		} else {
			w.transitionTo(StateConnectedSecondary, ctrl)
		}
		return nil
	}

	// No controllers available - enter autonomous mode if enabled
	if w.config.AutonomousModeEnabled {
		w.transitionTo(StateAutonomous, nil)
	} else {
		// Wait and retry
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	return nil
}

// handleConnectedSecondary handles the connected-to-secondary state
func (w *FailoverWatcher) handleConnectedSecondary() error {
	// Start recovery timer
	recoveryTimer := time.NewTimer(w.config.RecoveryDelay)
	defer recoveryTimer.Stop()

	select {
	case <-w.ctx.Done():
		return w.ctx.Err()

	case <-recoveryTimer.C:
		// Time to check if primary is back
		w.transitionTo(StateRecoveryCheck, w.activeCtrl)
	}

	return nil
}

// handleRecoveryCheck checks if primary is available again
func (w *FailoverWatcher) handleRecoveryCheck() error {
	primary := w.getPrimaryController()
	if primary == nil {
		w.transitionTo(StateConnectedSecondary, w.activeCtrl)
		return nil
	}

	// Try to ping primary
	if err := w.pingController(primary); err != nil {
		w.logger.Debug("Primary still unavailable",
			zap.String("controller", primary.Name),
			zap.Error(err),
		)
		w.transitionTo(StateConnectedSecondary, w.activeCtrl)
		return nil
	}

	// Primary is back - reconnect
	w.logger.Info("Primary recovered, reconnecting",
		zap.String("controller", primary.Name),
	)

	// Close secondary connection
	if w.activeConn != nil {
		_ = w.activeConn.Close()
		w.activeConn = nil
	}

	primary.FailureCount = 0
	w.transitionTo(StateConnectedPrimary, nil)

	return nil
}

// handleAutonomous handles autonomous mode
func (w *FailoverWatcher) handleAutonomous() error {
	w.logger.Info("Operating in autonomous mode")

	// Track autonomous mode duration
	w.autonomousStartTime = time.Now()
	defer func() {
		w.autonomousDuration += time.Since(w.autonomousStartTime)
	}()

	// Load cached config if available
	if w.currentSnapshot == nil {
		if snapshot, err := w.loadCachedConfig(); err == nil {
			w.snapshotMu.Lock()
			w.currentSnapshot = snapshot
			w.currentVersion = snapshot.Version
			w.snapshotMu.Unlock()

			if w.onSnapshot != nil {
				if err := w.onSnapshot(snapshot); err != nil {
					w.logger.Error("Failed to apply cached config", zap.Error(err))
				} else {
					w.logger.Info("Applied cached config in autonomous mode",
						zap.String("version", snapshot.Version),
					)
				}
			}
		} else {
			w.logger.Warn("No cached config available for autonomous mode", zap.Error(err))
		}
	}

	// Periodically try to reconnect
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()

		case <-ticker.C:
			// Try to connect to any controller
			w.controllersMu.RLock()
			controllers := w.controllers
			w.controllersMu.RUnlock()

			for _, ctrl := range controllers {
				if err := w.pingController(ctrl); err == nil {
					w.logger.Info("Controller available, exiting autonomous mode",
						zap.String("controller", ctrl.Name),
						zap.Duration("autonomous_duration", time.Since(w.autonomousStartTime)),
					)
					ctrl.FailureCount = 0
					w.transitionTo(StateFailingOver, nil)
					return nil
				}
			}

			w.logger.Debug("Still in autonomous mode, no controllers available",
				zap.Duration("duration", time.Since(w.autonomousStartTime)),
			)
		}
	}
}

// transitionTo transitions to a new state
func (w *FailoverWatcher) transitionTo(newState FailoverState, ctrl *ControllerEndpoint) {
	w.stateMu.Lock()
	oldState := w.state
	w.state = newState
	w.activeCtrl = ctrl
	w.stateMu.Unlock()

	w.logger.Info("State transition",
		zap.String("from", string(oldState)),
		zap.String("to", string(newState)),
		zap.String("controller", controllerName(ctrl)),
	)

	if w.onStateChange != nil {
		w.onStateChange(newState, ctrl)
	}
}

// connectAndStream connects to a controller and streams config
func (w *FailoverWatcher) connectAndStream(ctrl *ControllerEndpoint) error {
	conn, err := w.connect(ctrl)
	if err != nil {
		return err
	}

	// Store connection
	w.stateMu.Lock()
	if w.activeConn != nil {
		_ = w.activeConn.Close()
	}
	w.activeConn = conn
	w.activeCtrl = ctrl
	w.stateMu.Unlock()

	client := pb.NewConfigServiceClient(conn)

	// Create stream request
	req := &pb.StreamConfigRequest{
		NodeName:           w.nodeName,
		AgentVersion:       w.agentVersion,
		LastAppliedVersion: w.currentVersion,
	}

	if w.clusterConfig != nil {
		req.ClusterName = w.clusterConfig.Name
		req.ClusterRegion = w.clusterConfig.Region
		req.ClusterZone = w.clusterConfig.Zone
		req.ClusterLabels = w.clusterConfig.Labels
	}

	// Start streaming
	ctx, cancel := context.WithCancel(w.ctx)
	defer cancel()

	stream, err := client.StreamConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	ctrl.LastHealthy = time.Now()
	ctrl.FailureCount = 0

	// Receive snapshots
	for {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		default:
		}

		snapshot, err := stream.Recv()
		if err != nil {
			w.connectionErrors++
			return fmt.Errorf("stream error: %w", err)
		}

		ctrl.LastHealthy = time.Now()

		// Update controllers list from snapshot
		if len(snapshot.AvailableControllers) > 0 {
			w.updateControllersFromSnapshot(snapshot.AvailableControllers)
		}

		// Store vector clock
		if snapshot.FederationMetadata != nil {
			w.snapshotMu.Lock()
			w.lastVectorClock = snapshot.FederationMetadata.VectorClock
			w.snapshotMu.Unlock()
		}

		// Apply snapshot
		wrapped := &Snapshot{ConfigSnapshot: snapshot}
		if w.onSnapshot != nil {
			if err := w.onSnapshot(wrapped); err != nil {
				w.logger.Error("Failed to apply snapshot", zap.Error(err))
				continue
			}
		}

		w.snapshotMu.Lock()
		w.currentSnapshot = wrapped
		w.currentVersion = snapshot.Version
		w.snapshotMu.Unlock()

		// Persist config for autonomous mode
		if w.config.ConfigPersistPath != "" {
			go w.persistConfig(wrapped)
		}
	}
}

// connect establishes a gRPC connection to a controller
func (w *FailoverWatcher) connect(ctrl *ControllerEndpoint) (*grpc.ClientConn, error) {
	// Start with message size limits and keepalive options
	opts := grpclimits.ClientOptions()

	if w.tlsConfig != nil && w.tlsConfig.CertFile != "" {
		creds, err := tlsutil.LoadClientTLSCredentials(
			w.tlsConfig.CertFile,
			w.tlsConfig.KeyFile,
			w.tlsConfig.CAFile,
			ctrl.Name,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	start := time.Now()
	conn, err := grpc.NewClient(ctrl.Endpoint, opts...)
	if err != nil {
		return nil, err
	}

	ctrl.Latency = time.Since(start)
	return conn, nil
}

// pingController checks if a controller is reachable
func (w *FailoverWatcher) pingController(ctrl *ControllerEndpoint) error {
	conn, err := w.connect(ctrl)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewConfigServiceClient(conn)

	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	_, err = client.ReportStatus(ctx, &pb.AgentStatus{
		NodeName:             w.nodeName,
		AppliedConfigVersion: w.currentVersion,
		Timestamp:            time.Now().Unix(),
		Healthy:              true,
	})

	if err != nil {
		if s, ok := status.FromError(err); ok {
			if s.Code() == codes.Unavailable {
				return err
			}
		}
		// Non-unavailable errors might still mean controller is reachable
		return nil
	}

	return nil
}

// runHealthChecker periodically checks controller health
func (w *FailoverWatcher) runHealthChecker() {
	ticker := time.NewTicker(w.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.healthCheckCtx.Done():
			return

		case <-ticker.C:
			w.controllersMu.Lock()
			for _, ctrl := range w.controllers {
				// Skip if recently healthy
				if time.Since(ctrl.LastHealthy) < w.config.HealthCheckInterval {
					continue
				}

				go func(c *ControllerEndpoint) {
					if err := w.pingController(c); err == nil {
						c.FailureCount = 0
						c.LastHealthy = time.Now()
					}
				}(ctrl)
			}
			w.controllersMu.Unlock()
		}
	}
}

// updateControllersFromSnapshot updates the controller list from snapshot metadata
func (w *FailoverWatcher) updateControllersFromSnapshot(pbControllers []*pb.ControllerInfo) {
	w.controllersMu.Lock()
	defer w.controllersMu.Unlock()

	// Build map of existing controllers
	existing := make(map[string]*ControllerEndpoint)
	for _, ctrl := range w.controllers {
		existing[ctrl.Name] = ctrl
	}

	// Update or add controllers
	var updated []*ControllerEndpoint
	for _, pbCtrl := range pbControllers {
		if ctrl, ok := existing[pbCtrl.Name]; ok {
			// Update existing
			ctrl.Endpoint = pbCtrl.Endpoint
			ctrl.Priority = pbCtrl.Priority
			ctrl.Region = pbCtrl.Region
			ctrl.Zone = pbCtrl.Zone
			updated = append(updated, ctrl)
		} else {
			// Add new
			updated = append(updated, &ControllerEndpoint{
				Name:     pbCtrl.Name,
				Endpoint: pbCtrl.Endpoint,
				Priority: pbCtrl.Priority,
				Region:   pbCtrl.Region,
				Zone:     pbCtrl.Zone,
			})
		}
	}

	// Sort by priority
	sort.Slice(updated, func(i, j int) bool {
		return updated[i].Priority < updated[j].Priority
	})

	w.controllers = updated
}

// getPrimaryController returns the primary controller (priority 0)
func (w *FailoverWatcher) getPrimaryController() *ControllerEndpoint {
	w.controllersMu.RLock()
	defer w.controllersMu.RUnlock()

	for _, ctrl := range w.controllers {
		if ctrl.Priority == 0 {
			return ctrl
		}
	}
	return nil
}

// persistConfig saves the current config for autonomous mode
func (w *FailoverWatcher) persistConfig(snapshot *Snapshot) {
	if w.persistence == nil {
		return
	}

	if snapshot == nil || snapshot.ConfigSnapshot == nil {
		return
	}

	if err := w.persistence.SaveSnapshot(snapshot.ConfigSnapshot); err != nil {
		w.logger.Error("Failed to persist config for autonomous mode",
			zap.String("version", snapshot.Version),
			zap.Error(err),
		)
		return
	}

	w.logger.Debug("Persisted config for autonomous mode",
		zap.String("version", snapshot.Version),
	)
}

// loadCachedConfig loads the cached config for autonomous mode
func (w *FailoverWatcher) loadCachedConfig() (*Snapshot, error) {
	if w.persistence == nil {
		return nil, errConfigPersistenceNotInitialized
	}

	// Check if we have a cached config
	if !w.persistence.HasCachedConfig() {
		return nil, errNoCachedConfigAvailable
	}

	// Check config age - warn if stale
	age := w.persistence.ConfigAge()
	if age > 24*time.Hour {
		w.logger.Warn("Cached config is stale",
			zap.Duration("age", age),
		)
	}

	pbSnapshot, err := w.persistence.LoadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to load cached config: %w", err)
	}

	w.logger.Info("Loaded cached config for autonomous mode",
		zap.String("version", pbSnapshot.Version),
		zap.Duration("age", age),
	)

	return &Snapshot{ConfigSnapshot: pbSnapshot}, nil
}

// cleanup cleans up resources
func (w *FailoverWatcher) cleanup() {
	if w.healthCancel != nil {
		w.healthCancel()
	}
	if w.activeConn != nil {
		_ = w.activeConn.Close()
	}
}

// controllerName safely gets a controller's name
func controllerName(ctrl *ControllerEndpoint) string {
	if ctrl == nil {
		return "<none>"
	}
	return ctrl.Name
}

// GetMetrics returns failover metrics
func (w *FailoverWatcher) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"state":               string(w.GetState()),
		"failover_count":      w.failoverCount,
		"connection_errors":   w.connectionErrors,
		"autonomous_duration": w.autonomousDuration.String(),
		"current_controller":  controllerName(w.GetActiveController()),
	}
}

// GetControllerConnectionInfo returns information for status reporting
func (w *FailoverWatcher) GetControllerConnectionInfo() *pb.ControllerConnectionInfo {
	w.stateMu.RLock()
	state := w.state
	ctrl := w.activeCtrl
	w.stateMu.RUnlock()

	w.snapshotMu.RLock()
	vectorClock := w.lastVectorClock
	w.snapshotMu.RUnlock()

	info := &pb.ControllerConnectionInfo{
		FailoverState: string(state),
		FailoverCount: safeInt64ToInt32(w.failoverCount),
	}

	// Set controller info if connected
	if ctrl != nil {
		info.ConnectedController = ctrl.Name
		info.IsPrimary = ctrl.Priority == 0
		info.ConnectedSince = ctrl.LastHealthy.Unix()
		info.ControllerLatencyMs = ctrl.Latency.Milliseconds()
	}

	// Set vector clock
	if vectorClock != nil {
		info.LastVectorClock = vectorClock
	}

	// Check autonomous mode
	if state == StateAutonomous {
		info.AutonomousMode = true
		info.AutonomousDurationSeconds = int64(w.autonomousDuration.Seconds())
		if !w.autonomousStartTime.IsZero() {
			info.AutonomousDurationSeconds += int64(time.Since(w.autonomousStartTime).Seconds())
		}
	}

	// Add controller reachability for agent-assisted quorum
	info.Reachability = w.getControllerReachability()

	return info
}

// getControllerReachability returns the current controller reachability info
func (w *FailoverWatcher) getControllerReachability() *pb.ControllerReachability {
	w.reachabilityMu.RLock()
	defer w.reachabilityMu.RUnlock()

	reachability := &pb.ControllerReachability{
		TotalControllers:    safeIntToInt32(len(w.controllerReachability)),
		ControllerLatencies: make(map[string]int64),
		LastContactTimes:    make(map[string]int64),
	}

	for name, info := range w.controllerReachability {
		if info.Reachable {
			reachability.ReachableControllers = append(reachability.ReachableControllers, name)
			reachability.ControllerLatencies[name] = info.Latency.Milliseconds()
			reachability.LastContactTimes[name] = info.LastContact.Unix()
		}
	}

	return reachability
}

// GetVectorClock returns the last known vector clock
func (w *FailoverWatcher) GetVectorClock() map[string]int64 {
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()

	if w.lastVectorClock == nil {
		return nil
	}

	result := make(map[string]int64, len(w.lastVectorClock))
	for k, v := range w.lastVectorClock {
		result[k] = v
	}
	return result
}

// GetPersistence returns the config persistence handler
func (w *FailoverWatcher) GetPersistence() *Persistence {
	return w.persistence
}

// safeInt64ToInt32 converts int64 to int32 with overflow protection
func safeInt64ToInt32(v int64) int32 {
	const maxInt32 = int64(1<<31 - 1)
	if v > maxInt32 {
		return int32(maxInt32)
	}
	if v < -maxInt32-1 {
		return -int32(maxInt32) - 1
	}
	return int32(v)
}

// safeIntToInt32 converts int to int32 with overflow protection
func safeIntToInt32(v int) int32 {
	return safeInt64ToInt32(int64(v))
}
