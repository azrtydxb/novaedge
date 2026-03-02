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

// Package main provides the standalone NovaEdge load balancer entry point.
// This runs NovaEdge without Kubernetes, reading configuration from a YAML file.
// Traffic handling is delegated to the Rust dataplane sidecar.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	agentconfig "github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/server"
	"github.com/piwi3910/novaedge/internal/agent/vip"
	dpctl "github.com/piwi3910/novaedge/internal/dataplane"
	"github.com/piwi3910/novaedge/internal/standalone"
)

// Build-time variables set via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var (
	configFile      string
	nodeName        string
	metricsPort     int
	healthProbePort int
	logLevel        string
	dataplaneSocket string
)

func main() {
	flag.StringVar(&configFile, "config", "/etc/novaedge/config.yaml", "Path to configuration file")
	flag.StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")
	flag.IntVar(&metricsPort, "metrics-port", 9090, "Port for Prometheus metrics")
	flag.IntVar(&healthProbePort, "health-probe-port", 8080, "Port for health probes")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&dataplaneSocket, "dataplane-socket", dpctl.DefaultDataplaneSocket,
		"Unix domain socket path for the Rust dataplane daemon")
	flag.Parse()

	// Setup logger
	logger := setupLogger(logLevel)
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting NovaEdge Standalone Load Balancer",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("date", date),
		zap.String("config", configFile))

	// Get node name
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			logger.Fatal("Failed to get hostname", zap.Error(err))
		}
		nodeName = hostname
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Create VIP manager (optional - fails gracefully on non-Linux systems)
	vipManager, err := vip.NewManager(logger)
	if err != nil {
		logger.Warn("VIP manager not available (VIP features disabled)", zap.Error(err))
		vipManager = nil
	}

	// Create dataplane client for Rust forwarding plane
	dpClient, dpErr := dpctl.NewClient(dataplaneSocket, logger.Named("dataplane"))
	if dpErr != nil {
		logger.Fatal("Failed to connect to Rust dataplane",
			zap.String("socket", dataplaneSocket),
			zap.Error(dpErr))
	}
	dpTranslator := dpctl.NewTranslator(dpClient, logger.Named("dataplane"))
	logger.Info("Rust forwarding plane active: delegating all forwarding to dataplane daemon")

	// Create metrics server
	metricsServer := server.NewMetricsServer(logger, metricsPort)

	// Create health probe server
	healthServer := server.NewHealthServer(logger, healthProbePort)

	// Start VIP manager if available
	if vipManager != nil {
		if err := vipManager.Start(ctx); err != nil {
			logger.Error("Failed to start VIP manager", zap.Error(err))
			vipManager = nil
		}
	}

	// Create config watcher
	configWatcher, err := standalone.NewConfigWatcher(configFile, nodeName, logger)
	if err != nil {
		logger.Fatal("Failed to create config watcher", zap.Error(err))
	}

	// Start config watcher
	configChan := make(chan error, 1)
	go func() {
		configChan <- configWatcher.Start(ctx, func(snapshot *agentconfig.Snapshot) error {
			// Apply new configuration to VIP manager and Rust dataplane
			logger.Info("Applying new configuration",
				zap.String("version", snapshot.Version),
				zap.Int("gateways", len(snapshot.Gateways)),
				zap.Int("routes", len(snapshot.Routes)),
				zap.Int("vips", len(snapshot.VipAssignments)),
			)

			// Apply VIP assignments if VIP manager is available
			if vipManager != nil {
				if err := vipManager.ApplyVIPs(ctx, snapshot.VipAssignments); err != nil {
					logger.Error("Failed to apply VIP assignments", zap.Error(err))
				}
			}

			// Sync all config to Rust dataplane (L7 forwarding, routing, policies).
			if syncErr := dpTranslator.Sync(ctx, snapshot.ConfigSnapshot); syncErr != nil {
				logger.Error("Failed to sync config to Rust dataplane", zap.Error(syncErr))
				healthServer.SetReady(false)
				return syncErr
			}

			// Mark agent as ready after successful config application
			healthServer.SetReady(true)
			return nil
		})
	}()

	// Start metrics server
	metricsChan := make(chan error, 1)
	go func() {
		metricsChan <- metricsServer.Start(ctx)
	}()

	// Start health probe server
	healthChan := make(chan error, 1)
	go func() {
		healthChan <- healthServer.Start(ctx)
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-configChan:
		if err != nil && ctx.Err() == nil {
			logger.Error("Config watcher failed", zap.Error(err))
		}
	case err := <-metricsChan:
		logger.Error("Metrics server failed", zap.Error(err))
	case err := <-healthChan:
		logger.Error("Health probe failed", zap.Error(err))
	case <-ctx.Done():
		// Graceful shutdown
	}

	// Graceful shutdown
	logger.Info("Shutting down...")

	// Give servers time to shutdown gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Release VIPs first to allow failover
	if vipManager != nil {
		logger.Info("Releasing VIPs...")
		if err := vipManager.Release(); err != nil {
			logger.Error("Error releasing VIPs", zap.Error(err))
		}
	}

	// Shutdown dataplane client
	if dpClient != nil {
		if err := dpClient.Close(); err != nil {
			logger.Error("Error closing dataplane client", zap.Error(err))
		}
	}

	// Shutdown metrics server
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during metrics server shutdown", zap.Error(err))
	}

	logger.Info("NovaEdge standalone agent stopped")
}

func setupLogger(level string) *zap.Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}

	return logger
}
