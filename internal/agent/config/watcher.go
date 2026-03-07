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
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/azrtydxb/novaedge/internal/pkg/grpclimits"
	"github.com/azrtydxb/novaedge/internal/pkg/tlsutil"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errTLSConfigurationIsIncomplete                  = errors.New("TLS configuration is incomplete")
	errTLSConfigurationIsRequiredForRemoteAgents     = errors.New("TLS configuration is required for remote agents")
	errClusterConfigurationIsRequiredForRemoteAgents = errors.New("cluster configuration is required for remote agents")
	errForceResyncByGossipQuorum                     = errors.New("force resync requested by gossip quorum")
)

// Snapshot is a wrapper around the protobuf ConfigSnapshot
type Snapshot struct {
	// Extensions carries mTLS, PROXY protocol, and OCSP configuration
	Extensions *SnapshotExtensions
	*pb.ConfigSnapshot
}

// ApplyFunc is called when a new config snapshot is received
type ApplyFunc func(*Snapshot) error

// Watcher watches for config updates from the controller
type Watcher struct {
	nodeName       string
	agentVersion   string
	controllerAddr string
	logger         *zap.Logger
	ctx            context.Context

	// TLS configuration for mTLS
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string
	tlsEnabled  bool

	// Remote cluster identification (for hub-spoke deployments)
	clusterName   string
	clusterRegion string
	clusterZone   string
	clusterLabels map[string]string

	// currentSnapshot stores the latest applied config snapshot using
	// atomic.Pointer for lock-free reads on the hot path. Writers build the
	// complete new Snapshot then do a single atomic Store; readers use Load
	// without any lock.
	currentSnapshot atomic.Pointer[Snapshot]

	// forceResyncCh is used by the gossip layer to trigger an immediate
	// reconnect to the controller, bypassing version comparison.
	forceResyncCh chan struct{}
}

// TLSConfig holds TLS configuration for the watcher
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// ClusterConfig holds cluster identification for remote agents
type ClusterConfig struct {
	Name   string
	Region string
	Zone   string
	Labels map[string]string
}

// NewWatcher creates a new config watcher
func NewWatcher(ctx context.Context, nodeName, agentVersion, controllerAddr string, logger *zap.Logger) (*Watcher, error) {
	return &Watcher{
		nodeName:       nodeName,
		agentVersion:   agentVersion,
		controllerAddr: controllerAddr,
		logger:         logger,
		ctx:            ctx,
		tlsEnabled:     false,
		forceResyncCh:  make(chan struct{}, 1),
	}, nil
}

// NewWatcherWithTLS creates a new config watcher with mTLS enabled
func NewWatcherWithTLS(ctx context.Context, nodeName, agentVersion, controllerAddr string, tlsConfig *TLSConfig, logger *zap.Logger) (*Watcher, error) {
	if tlsConfig == nil || tlsConfig.CertFile == "" || tlsConfig.KeyFile == "" || tlsConfig.CAFile == "" {
		return nil, errTLSConfigurationIsIncomplete
	}

	return &Watcher{
		nodeName:       nodeName,
		agentVersion:   agentVersion,
		controllerAddr: controllerAddr,
		logger:         logger,
		ctx:            ctx,
		tlsCertFile:    tlsConfig.CertFile,
		tlsKeyFile:     tlsConfig.KeyFile,
		tlsCAFile:      tlsConfig.CAFile,
		tlsEnabled:     true,
		forceResyncCh:  make(chan struct{}, 1),
	}, nil
}

// NewRemoteWatcher creates a new config watcher for remote cluster agents with mTLS and cluster identification
func NewRemoteWatcher(ctx context.Context, nodeName, agentVersion, controllerAddr string, tlsConfig *TLSConfig, clusterConfig *ClusterConfig, logger *zap.Logger) (*Watcher, error) {
	if tlsConfig == nil || tlsConfig.CertFile == "" || tlsConfig.KeyFile == "" || tlsConfig.CAFile == "" {
		return nil, errTLSConfigurationIsRequiredForRemoteAgents
	}
	if clusterConfig == nil || clusterConfig.Name == "" {
		return nil, errClusterConfigurationIsRequiredForRemoteAgents
	}

	return &Watcher{
		nodeName:       nodeName,
		agentVersion:   agentVersion,
		controllerAddr: controllerAddr,
		logger:         logger,
		ctx:            ctx,
		tlsCertFile:    tlsConfig.CertFile,
		tlsKeyFile:     tlsConfig.KeyFile,
		tlsCAFile:      tlsConfig.CAFile,
		tlsEnabled:     true,
		clusterName:    clusterConfig.Name,
		clusterRegion:  clusterConfig.Region,
		clusterZone:    clusterConfig.Zone,
		clusterLabels:  clusterConfig.Labels,
		forceResyncCh:  make(chan struct{}, 1),
	}, nil
}

// ForceResync forces the watcher to close the current stream and reconnect
// to the controller, fetching a fresh config snapshot. Called by the gossip
// layer when a quorum of peers have a newer config version.
func (w *Watcher) ForceResync() {
	select {
	case w.forceResyncCh <- struct{}{}:
		w.logger.Info("Force resync requested by gossip quorum")
	default:
		// Already pending
	}
}

// Start begins watching for config updates and calls applyFunc when updates arrive
func (w *Watcher) Start(applyFunc ApplyFunc) error {
	w.logger.Info("Starting config watcher",
		zap.String("controller", w.controllerAddr),
	)

	// Connect to controller with retry
	conn, err := w.connectWithRetry()
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Create config service client
	client := pb.NewConfigServiceClient(conn)

	// Start streaming config
	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Config watcher stopped")
			return w.ctx.Err()
		default:
			if err := w.streamConfig(client, applyFunc); err != nil {
				w.logger.Error("Config stream error, retrying...",
					zap.Error(err),
					zap.Duration("retry_delay", 5*time.Second),
				)
				select {
				case <-w.ctx.Done():
					return w.ctx.Err()
				case <-time.After(5 * time.Second):
				}
				continue
			}
		}
	}
}

// connectWithRetry attempts to connect to the controller with exponential backoff
func (w *Watcher) connectWithRetry() (*grpc.ClientConn, error) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-w.ctx.Done():
			return nil, w.ctx.Err()
		default:
		}

		w.logger.Info("Connecting to controller",
			zap.String("address", w.controllerAddr),
			zap.Bool("tls_enabled", w.tlsEnabled))

		// Start with message size limits and keepalive options
		opts := grpclimits.ClientOptions()
		var creds credentials.TransportCredentials

		if w.tlsEnabled {
			// Load TLS credentials for mTLS
			var err error
			creds, err = tlsutil.LoadClientTLSCredentials(
				w.tlsCertFile,
				w.tlsKeyFile,
				w.tlsCAFile,
				"novaedge-controller", // Server name for SNI
			)
			if err != nil {
				w.logger.Error("Failed to load TLS credentials", zap.Error(err))
				return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
			}
			opts = append(opts, grpc.WithTransportCredentials(creds))
		} else {
			// Use insecure connection (development only)
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}

		conn, err := grpc.NewClient(w.controllerAddr, opts...)
		if err != nil {
			w.logger.Warn("Failed to connect to controller",
				zap.Error(err),
				zap.Duration("retry_in", backoff),
			)
			select {
			case <-w.ctx.Done():
				return nil, w.ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		if w.tlsEnabled {
			w.logger.Info("Connected to controller with mTLS")
		} else {
			w.logger.Info("Connected to controller (insecure)")
		}
		return conn, nil
	}
}

// recvResult carries the result of a blocking stream.Recv() call so it can
// be selected alongside other channels.
type recvResult struct {
	snapshot *pb.ConfigSnapshot
	err      error
}

// streamConfig streams config snapshots from the controller
func (w *Watcher) streamConfig(client pb.ConfigServiceClient, applyFunc ApplyFunc) error {
	// Read the last applied version from the atomic snapshot pointer
	lastVersion := ""
	if snap := w.currentSnapshot.Load(); snap != nil {
		lastVersion = snap.Version
	}

	// Create stream request
	req := &pb.StreamConfigRequest{
		NodeName:           w.nodeName,
		AgentVersion:       w.agentVersion,
		LastAppliedVersion: lastVersion,
		ClusterName:        w.clusterName,
		ClusterRegion:      w.clusterRegion,
		ClusterZone:        w.clusterZone,
		ClusterLabels:      w.clusterLabels,
	}

	// Start streaming
	stream, err := client.StreamConfig(w.ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start config stream: %w", err)
	}

	w.logger.Info("Config stream established")

	// Status reporting ticker
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	// Run stream.Recv() in a goroutine so we can select on forceResyncCh.
	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			snapshot, recvErr := stream.Recv()
			recvCh <- recvResult{snapshot: snapshot, err: recvErr}
			if recvErr != nil {
				return
			}
		}
	}()

	// Receive snapshots
	for {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()

		case <-w.forceResyncCh:
			return errForceResyncByGossipQuorum

		case <-statusTicker.C:
			// Report status to controller
			go w.reportStatus(w.ctx, client)

		case result := <-recvCh:
			if result.err != nil {
				return fmt.Errorf("error receiving config snapshot: %w", result.err)
			}

			snapshot := result.snapshot
			w.logger.Info("Received config snapshot",
				zap.String("version", snapshot.Version),
				zap.Int64("generation_time", snapshot.GenerationTime),
			)

			// Build the complete new snapshot, then atomically swap the
			// pointer so readers never see a partially-built config.
			wrapped := &Snapshot{ConfigSnapshot: snapshot}
			if err := applyFunc(wrapped); err != nil {
				w.logger.Error("Failed to apply config snapshot",
					zap.Error(err),
					zap.String("version", snapshot.Version),
				)
				// Report error to controller
				go w.reportStatus(w.ctx, client)
				continue
			}

			// Atomic store: readers use Load() without any lock
			w.currentSnapshot.Store(wrapped)
			w.logger.Info("Applied config snapshot successfully",
				zap.String("version", snapshot.Version),
			)

			// Report successful application
			go w.reportStatus(w.ctx, client)
		}
	}
}

// reportStatus reports agent status to the controller
func (w *Watcher) reportStatus(ctx context.Context, client pb.ConfigServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Read current version from atomic pointer (lock-free)
	currentVersion := ""
	if snap := w.currentSnapshot.Load(); snap != nil {
		currentVersion = snap.Version
	}

	status := &pb.AgentStatus{
		NodeName:             w.nodeName,
		AppliedConfigVersion: currentVersion,
		Timestamp:            time.Now().Unix(),
		Healthy:              true,
		Metrics:              make(map[string]int64),
		ClusterName:          w.clusterName,
	}

	_, err := client.ReportStatus(ctx, status)
	if err != nil {
		w.logger.Warn("Failed to report status", zap.Error(err))
	}
}

// IsRemote returns true if this watcher is for a remote cluster agent
func (w *Watcher) IsRemote() bool {
	return w.clusterName != ""
}

// GetClusterName returns the cluster name (empty for local agents)
func (w *Watcher) GetClusterName() string {
	return w.clusterName
}
