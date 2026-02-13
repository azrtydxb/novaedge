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

// Package main implements the novaedge-agent binary, which runs as a DaemonSet
// on each node to handle L4/L7 load balancing, VIP management, and policy enforcement.
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
	"github.com/piwi3910/novaedge/internal/agent/l4"
	"github.com/piwi3910/novaedge/internal/agent/server"
	"github.com/piwi3910/novaedge/internal/agent/vip"
	"github.com/piwi3910/novaedge/internal/observability"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

var (
	nodeName        string
	controllerAddr  string
	agentVersion    = "0.1.0"
	logLevel        string
	healthProbePort int
	metricsPort     int

	// TLS configuration for controller-agent mTLS
	grpcTLSCert string
	grpcTLSKey  string
	grpcTLSCA   string

	// Remote cluster identification (for hub-spoke deployments)
	clusterName   string
	clusterRegion string
	clusterZone   string

	// Tracing configuration
	tracingEnabled    bool
	tracingEndpoint   string
	tracingSampleRate float64
)

func main() {
	flag.StringVar(&nodeName, "node-name", "", "Name of this node (required)")
	flag.StringVar(&controllerAddr, "controller-address", "localhost:9090", "Address of the controller gRPC server")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&healthProbePort, "health-probe-port", 9091, "Port for health probe endpoint")
	flag.IntVar(&metricsPort, "metrics-port", 9090, "Port for Prometheus metrics endpoint")

	// TLS flags for mTLS with controller
	flag.StringVar(&grpcTLSCert, "grpc-tls-cert", "", "Path to gRPC client TLS certificate file (enables mTLS if provided)")
	flag.StringVar(&grpcTLSKey, "grpc-tls-key", "", "Path to gRPC client TLS key file")
	flag.StringVar(&grpcTLSCA, "grpc-tls-ca", "", "Path to gRPC CA certificate file for server verification")

	// Remote cluster identification flags (for hub-spoke deployments)
	flag.StringVar(&clusterName, "cluster-name", "", "Name of the remote cluster (empty for local agents)")
	flag.StringVar(&clusterRegion, "cluster-region", "", "Geographic region of the remote cluster")
	flag.StringVar(&clusterZone, "cluster-zone", "", "Availability zone within the region")

	// Tracing flags
	flag.BoolVar(&tracingEnabled, "tracing-enabled", false, "Enable OpenTelemetry distributed tracing")
	flag.StringVar(&tracingEndpoint, "tracing-endpoint", "localhost:4317", "OTLP gRPC endpoint for trace export")
	flag.Float64Var(&tracingSampleRate, "tracing-sample-rate", 0.1, "Trace sampling rate (0.0 to 1.0)")

	flag.Parse()

	// Validate required flags
	if nodeName == "" {
		fmt.Fprintf(os.Stderr, "Error: --node-name is required\n")
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger(logLevel)
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting NovaEdge agent",
		zap.String("node", nodeName),
		zap.String("version", agentVersion),
		zap.String("controller", controllerAddr),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry tracing
	tracerProvider, err := observability.NewTracerProvider(ctx, observability.TracingConfig{
		Enabled:        tracingEnabled,
		Endpoint:       tracingEndpoint,
		SampleRate:     tracingSampleRate,
		ServiceName:    "novaedge-agent",
		ServiceVersion: agentVersion,
	}, logger)
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			logger.Error("Failed to shutdown tracer provider", zap.Error(err))
		}
	}()

	// Create config watcher with optional mTLS and cluster identification
	var watcher *config.Watcher
	isRemoteAgent := clusterName != ""

	switch {
	case isRemoteAgent:
		// Remote agents require mTLS
		if grpcTLSCert == "" || grpcTLSKey == "" || grpcTLSCA == "" {
			logger.Fatal("Remote agents require mTLS configuration",
				zap.String("cluster", clusterName))
		}
		// Create remote watcher with mTLS and cluster identification
		watcher, err = config.NewRemoteWatcher(ctx, nodeName, agentVersion, controllerAddr,
			&config.TLSConfig{
				CertFile: grpcTLSCert,
				KeyFile:  grpcTLSKey,
				CAFile:   grpcTLSCA,
			},
			&config.ClusterConfig{
				Name:   clusterName,
				Region: clusterRegion,
				Zone:   clusterZone,
			}, logger)
		if err != nil {
			logger.Fatal("Failed to create remote config watcher", zap.Error(err))
		}
		logger.Info("Remote agent configured with mTLS",
			zap.String("cluster", clusterName),
			zap.String("cluster_region", clusterRegion),
			zap.String("cluster_zone", clusterZone),
			zap.String("cert", grpcTLSCert),
			zap.String("ca", grpcTLSCA))
	case grpcTLSCert != "" && grpcTLSKey != "" && grpcTLSCA != "":
		// Local agent with mTLS enabled
		watcher, err = config.NewWatcherWithTLS(ctx, nodeName, agentVersion, controllerAddr,
			&config.TLSConfig{
				CertFile: grpcTLSCert,
				KeyFile:  grpcTLSKey,
				CAFile:   grpcTLSCA,
			}, logger)
		if err != nil {
			logger.Fatal("Failed to create config watcher with TLS", zap.Error(err))
		}
		logger.Info("Config watcher configured with mTLS",
			zap.String("cert", grpcTLSCert),
			zap.String("ca", grpcTLSCA))
	default:
		// Local agent without TLS (insecure, development only)
		watcher, err = config.NewWatcher(ctx, nodeName, agentVersion, controllerAddr, logger)
		if err != nil {
			logger.Fatal("Failed to create config watcher", zap.Error(err))
		}
		logger.Warn("WARNING: Config watcher running without TLS (insecure)")
	}

	// Create VIP manager
	vipManager, err := vip.NewManager(logger)
	if err != nil {
		logger.Fatal("Failed to create VIP manager", zap.Error(err))
	}

	// Create HTTP server
	httpServer := server.NewHTTPServer(logger)

	// Create L4 manager
	l4Manager := l4.NewManager(logger)

	// Create metrics server
	metricsServer := server.NewMetricsServer(logger, metricsPort)

	// Create health probe server
	healthServer := server.NewHealthServer(logger, healthProbePort)

	// Start VIP manager
	if err := vipManager.Start(ctx); err != nil {
		logger.Fatal("Failed to start VIP manager", zap.Error(err))
	}

	// Start config watcher
	configChan := make(chan error, 1)
	go func() {
		configChan <- watcher.Start(func(snapshot *config.Snapshot) error {
			// Apply new configuration to HTTP server and VIP manager
			logger.Info("Applying new configuration",
				zap.String("version", snapshot.Version),
				zap.Int("gateways", len(snapshot.Gateways)),
				zap.Int("routes", len(snapshot.Routes)),
				zap.Int("vips", len(snapshot.VipAssignments)),
			)

			// Apply L4 config (TCP/UDP/TLS passthrough)
			if applyErr := applyL4Config(ctx, l4Manager, snapshot, logger); applyErr != nil {
				logger.Error("Failed to apply L4 config", zap.Error(applyErr))
				// Don't fail the whole config update for L4 errors
			}

			// Apply VIP assignments
			if err := vipManager.ApplyVIPs(ctx, snapshot.VipAssignments); err != nil {
				logger.Error("Failed to apply VIP assignments", zap.Error(err))
				// Don't fail the whole config update
			}

			// Apply HTTP server config
			if err := httpServer.ApplyConfig(ctx, snapshot); err != nil {
				healthServer.SetReady(false)
				return err
			}

			// Mark agent as ready after successful config application
			healthServer.SetReady(true)
			return nil
		})
	}()

	// Start HTTP server
	serverChan := make(chan error, 1)
	go func() {
		serverChan <- httpServer.Start(ctx)
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

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-configChan:
		logger.Error("Config watcher failed", zap.Error(err))
	case err := <-serverChan:
		logger.Error("HTTP server failed", zap.Error(err))
	case err := <-metricsChan:
		logger.Error("Metrics server failed", zap.Error(err))
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

	// Release VIPs first to allow failover
	logger.Info("Releasing VIPs...")
	if err := vipManager.Release(); err != nil {
		logger.Error("Error releasing VIPs", zap.Error(err))
	}

	// Shutdown L4 listeners
	if err := l4Manager.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during L4 manager shutdown", zap.Error(err))
	}

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during HTTP server shutdown", zap.Error(err))
	}

	// Shutdown metrics server
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during metrics server shutdown", zap.Error(err))
	}

	logger.Info("Agent stopped")
}

// applyL4Config converts snapshot L4 listeners to L4 manager config and applies it
func applyL4Config(ctx context.Context, manager *l4.Manager, snapshot *config.Snapshot, logger *zap.Logger) error {
	if len(snapshot.L4Listeners) == 0 {
		// No L4 listeners, clear any existing ones
		return manager.ApplyConfig(ctx, nil)
	}

	configs := make([]l4.ListenerConfig, 0, len(snapshot.L4Listeners))
	for _, l4Listener := range snapshot.L4Listeners {
		cfg := l4.ListenerConfig{
			Name:        l4Listener.Name,
			Port:        l4Listener.Port,
			BackendName: l4Listener.BackendName,
			Backends:    l4Listener.Backends,
		}

		switch l4Listener.Protocol {
		case pb.Protocol_TCP:
			cfg.Type = l4.ListenerTypeTCP
			if l4Listener.TcpConfig != nil {
				cfg.TCPConfig = &l4.TCPProxyConfig{
					ConnectTimeout: time.Duration(l4Listener.TcpConfig.ConnectTimeoutMs) * time.Millisecond,
					IdleTimeout:    time.Duration(l4Listener.TcpConfig.IdleTimeoutMs) * time.Millisecond,
					BufferSize:     int(l4Listener.TcpConfig.BufferSize),
					DrainTimeout:   time.Duration(l4Listener.TcpConfig.DrainTimeoutMs) * time.Millisecond,
				}
			}
		case pb.Protocol_TLS:
			cfg.Type = l4.ListenerTypeTLSPassthrough
			routes := make(map[string]*l4.TLSRoute)
			for _, tlsRoute := range l4Listener.TlsRoutes {
				routes[tlsRoute.Hostname] = &l4.TLSRoute{
					Hostname:    tlsRoute.Hostname,
					Backends:    tlsRoute.Backends,
					BackendName: tlsRoute.BackendName,
				}
			}
			var defaultBackend *l4.TLSRoute
			if l4Listener.DefaultTlsBackend != nil {
				defaultBackend = &l4.TLSRoute{
					Hostname:    l4Listener.DefaultTlsBackend.Hostname,
					Backends:    l4Listener.DefaultTlsBackend.Backends,
					BackendName: l4Listener.DefaultTlsBackend.BackendName,
				}
			}
			cfg.TLSPassthroughConfig = &l4.TLSPassthroughConfig{
				Routes:         routes,
				DefaultBackend: defaultBackend,
			}
		case pb.Protocol_UDP:
			cfg.Type = l4.ListenerTypeUDP
			if l4Listener.UdpConfig != nil {
				cfg.UDPConfig = &l4.UDPProxyConfig{
					SessionTimeout: time.Duration(l4Listener.UdpConfig.SessionTimeoutMs) * time.Millisecond,
					BufferSize:     int(l4Listener.UdpConfig.BufferSize),
				}
			}
		default:
			logger.Warn("Unknown L4 listener protocol",
				zap.Int32("protocol", int32(l4Listener.Protocol)))
			continue
		}

		configs = append(configs, cfg)
	}

	logger.Info("Applying L4 configuration",
		zap.Int("listeners", len(configs)))
	return manager.ApplyConfig(ctx, configs)
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
