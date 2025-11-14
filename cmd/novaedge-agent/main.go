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

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/server"
)

var (
	nodeName        string
	controllerAddr  string
	agentVersion    = "0.1.0"
	logLevel        string
	healthProbeAddr string
)

func main() {
	flag.StringVar(&nodeName, "node-name", "", "Name of this node (required)")
	flag.StringVar(&controllerAddr, "controller-address", "localhost:9090", "Address of the controller gRPC server")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&healthProbeAddr, "health-probe-address", ":8082", "Address for health probe endpoint")
	flag.Parse()

	// Validate required flags
	if nodeName == "" {
		fmt.Fprintf(os.Stderr, "Error: --node-name is required\n")
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger(logLevel)
	defer logger.Sync()

	logger.Info("Starting NovaEdge agent",
		zap.String("node", nodeName),
		zap.String("version", agentVersion),
		zap.String("controller", controllerAddr),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create config watcher
	watcher, err := config.NewWatcher(ctx, nodeName, agentVersion, controllerAddr, logger)
	if err != nil {
		logger.Fatal("Failed to create config watcher", zap.Error(err))
	}

	// Create HTTP server
	httpServer := server.NewHTTPServer(logger)

	// Start config watcher
	configChan := make(chan error, 1)
	go func() {
		configChan <- watcher.Start(func(snapshot *config.Snapshot) error {
			// Apply new configuration to HTTP server
			logger.Info("Applying new configuration",
				zap.String("version", snapshot.Version),
				zap.Int("gateways", len(snapshot.Gateways)),
				zap.Int("routes", len(snapshot.Routes)),
			)
			return httpServer.ApplyConfig(snapshot)
		})
	}()

	// Start HTTP server
	serverChan := make(chan error, 1)
	go func() {
		serverChan <- httpServer.Start(ctx)
	}()

	// Start health probe server
	healthChan := make(chan error, 1)
	go func() {
		healthChan <- startHealthProbe(healthProbeAddr, logger)
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-configChan:
		logger.Error("Config watcher failed", zap.Error(err))
	case err := <-serverChan:
		logger.Error("HTTP server failed", zap.Error(err))
	case err := <-healthChan:
		logger.Error("Health probe failed", zap.Error(err))
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	}

	// Graceful shutdown
	logger.Info("Shutting down...")
	cancel()

	// Give servers time to shutdown gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during HTTP server shutdown", zap.Error(err))
	}

	logger.Info("Agent stopped")
}

func initLogger(level string) *zap.Logger {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	// Create logger config
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := config.Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}

	return logger
}

func startHealthProbe(addr string, logger *zap.Logger) error {
	// TODO: Implement proper health probe server
	// For now, just sleep
	logger.Info("Health probe would start here", zap.String("address", addr))
	select {}
}
