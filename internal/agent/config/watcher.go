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
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/piwi3910/novaedge/internal/pkg/tlsutil"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Snapshot is a wrapper around the protobuf ConfigSnapshot
type Snapshot struct {
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

	currentVersion string
}

// TLSConfig holds TLS configuration for the watcher
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
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
	}, nil
}

// NewWatcherWithTLS creates a new config watcher with mTLS enabled
func NewWatcherWithTLS(ctx context.Context, nodeName, agentVersion, controllerAddr string, tlsConfig *TLSConfig, logger *zap.Logger) (*Watcher, error) {
	if tlsConfig == nil || tlsConfig.CertFile == "" || tlsConfig.KeyFile == "" || tlsConfig.CAFile == "" {
		return nil, fmt.Errorf("TLS configuration is incomplete")
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
	}, nil
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
	defer conn.Close()

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
				time.Sleep(5 * time.Second)
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

		var opts []grpc.DialOption
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
			time.Sleep(backoff)
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

// streamConfig streams config snapshots from the controller
func (w *Watcher) streamConfig(client pb.ConfigServiceClient, applyFunc ApplyFunc) error {
	// Create stream request
	req := &pb.StreamConfigRequest{
		NodeName:           w.nodeName,
		AgentVersion:       w.agentVersion,
		LastAppliedVersion: w.currentVersion,
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

	// Receive snapshots
	for {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()

		case <-statusTicker.C:
			// Report status to controller
			go w.reportStatus(client)

		default:
			snapshot, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("error receiving config snapshot: %w", err)
			}

			w.logger.Info("Received config snapshot",
				zap.String("version", snapshot.Version),
				zap.Int64("generation_time", snapshot.GenerationTime),
			)

			// Apply the new configuration
			wrapped := &Snapshot{ConfigSnapshot: snapshot}
			if err := applyFunc(wrapped); err != nil {
				w.logger.Error("Failed to apply config snapshot",
					zap.Error(err),
					zap.String("version", snapshot.Version),
				)
				// Report error to controller
				go w.reportStatus(client)
				continue
			}

			// Update current version
			w.currentVersion = snapshot.Version
			w.logger.Info("Applied config snapshot successfully",
				zap.String("version", snapshot.Version),
			)

			// Report successful application
			go w.reportStatus(client)
		}
	}
}

// reportStatus reports agent status to the controller
func (w *Watcher) reportStatus(client pb.ConfigServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status := &pb.AgentStatus{
		NodeName:             w.nodeName,
		AppliedConfigVersion: w.currentVersion,
		Timestamp:            time.Now().Unix(),
		Healthy:              true,
		Metrics:              make(map[string]int64),
	}

	_, err := client.ReportStatus(ctx, status)
	if err != nil {
		w.logger.Warn("Failed to report status", zap.Error(err))
	}
}
