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

	agentconfig "github.com/azrtydxb/novaedge/internal/agent/config"
	"github.com/azrtydxb/novaedge/internal/agent/server"
	dpctl "github.com/azrtydxb/novaedge/internal/dataplane"
	"github.com/azrtydxb/novaedge/internal/standalone"
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

// standaloneComponents holds the initialized components for the standalone agent.
type standaloneComponents struct {
	logger        *zap.Logger
	dpClient      *dpctl.Client
	dpTranslator  *dpctl.Translator
	metricsServer *server.MetricsServer
	healthServer  *server.HealthServer
	configWatcher *standalone.ConfigWatcher
}

// parseFlags parses command-line flags and resolves the node name.
func parseFlags() {
	flag.StringVar(&configFile, "config", "/etc/novaedge/config.yaml", "Path to configuration file")
	flag.StringVar(&nodeName, "node-name", "", "Node name (defaults to hostname)")
	flag.IntVar(&metricsPort, "metrics-port", 9090, "Port for Prometheus metrics")
	flag.IntVar(&healthProbePort, "health-probe-port", 8080, "Port for health probes")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&dataplaneSocket, "dataplane-socket", dpctl.DefaultDataplaneSocket,
		"Unix domain socket path for the Rust dataplane daemon")
	flag.Parse()
}

// initComponents creates all standalone agent components.
func initComponents(ctx context.Context, logger *zap.Logger) *standaloneComponents {
	c := &standaloneComponents{logger: logger}

	// Create dataplane client for Rust forwarding plane
	dpClient, dpErr := dpctl.NewClient(dataplaneSocket, logger.Named("dataplane"))
	if dpErr != nil {
		logger.Fatal("Failed to connect to Rust dataplane",
			zap.String("socket", dataplaneSocket),
			zap.Error(dpErr))
	}
	c.dpClient = dpClient
	c.dpTranslator = dpctl.NewTranslator(dpClient, logger.Named("dataplane"))
	logger.Info("Rust forwarding plane active: delegating all forwarding to dataplane daemon")

	c.metricsServer = server.NewMetricsServer(ctx, logger, metricsPort)
	c.healthServer = server.NewHealthServer(ctx, logger, healthProbePort)

	cw, cwErr := standalone.NewConfigWatcher(configFile, nodeName, logger)
	if cwErr != nil {
		logger.Fatal("Failed to create config watcher", zap.Error(cwErr))
	}
	c.configWatcher = cw

	return c
}

// shutdownComponents performs graceful shutdown of all components.
func shutdownComponents(c *standaloneComponents) {
	c.logger.Info("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if c.dpClient != nil {
		if err := c.dpClient.Close(); err != nil {
			c.logger.Error("Error closing dataplane client", zap.Error(err))
		}
	}

	if err := c.metricsServer.Shutdown(shutdownCtx); err != nil {
		c.logger.Error("Error during metrics server shutdown", zap.Error(err))
	}

	c.logger.Info("NovaEdge standalone agent stopped")
}

func main() {
	parseFlags()

	logger := setupLogger(logLevel)
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting NovaEdge Standalone Load Balancer",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("date", date),
		zap.String("config", configFile))

	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			logger.Fatal("Failed to get hostname", zap.Error(err))
		}
		nodeName = hostname
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	c := initComponents(ctx, logger)

	// Start config watcher
	configChan := make(chan error, 1)
	go func() {
		configChan <- c.configWatcher.Start(ctx, func(snapshot *agentconfig.Snapshot) error {
			logger.Info("Applying new configuration",
				zap.String("version", snapshot.Version),
				zap.Int("gateways", len(snapshot.Gateways)),
				zap.Int("routes", len(snapshot.Routes)),
			)

			if syncErr := c.dpTranslator.Sync(ctx, snapshot.ConfigSnapshot); syncErr != nil {
				logger.Error("Failed to sync config to Rust dataplane", zap.Error(syncErr))
				c.healthServer.SetReady(false)
				return syncErr
			}

			c.healthServer.SetReady(true)
			return nil
		})
	}()

	metricsChan := make(chan error, 1)
	go func() {
		metricsChan <- c.metricsServer.Start(ctx)
	}()

	healthChan := make(chan error, 1)
	go func() {
		healthChan <- c.healthServer.Start(ctx)
	}()

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
	}

	shutdownComponents(c)
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
