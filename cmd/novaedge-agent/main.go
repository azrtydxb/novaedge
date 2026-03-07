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
// on each node to handle L4/L7 load balancing and policy enforcement.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/azrtydxb/novaedge/internal/agent/afxdp"
	"github.com/azrtydxb/novaedge/internal/agent/config"
	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	ebpfhealth "github.com/azrtydxb/novaedge/internal/agent/ebpf/health"
	ebpfratelimit "github.com/azrtydxb/novaedge/internal/agent/ebpf/ratelimit"
	"github.com/azrtydxb/novaedge/internal/agent/ebpf/sockmap"
	"github.com/azrtydxb/novaedge/internal/agent/ebpfmesh"
	"github.com/azrtydxb/novaedge/internal/agent/gossip"
	"github.com/azrtydxb/novaedge/internal/agent/introspection"
	"github.com/azrtydxb/novaedge/internal/agent/mesh"
	"github.com/azrtydxb/novaedge/internal/agent/sdwan"
	"github.com/azrtydxb/novaedge/internal/agent/server"
	dpctl "github.com/azrtydxb/novaedge/internal/dataplane"
	"github.com/azrtydxb/novaedge/internal/observability"
	"github.com/azrtydxb/novaedge/internal/pkg/grpclimits"
	"github.com/azrtydxb/novaedge/internal/pkg/tlsutil"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"

	// Blank import: registers WASM plugin Prometheus metrics via promauto init().
	_ "github.com/azrtydxb/novaedge/internal/agent/wasm"
)

// Build-time variables set via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var (
	nodeName        string
	controllerAddr  string
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

	// Service mesh configuration
	meshEnabled    bool
	meshTPROXYPort int
	meshTunnelPort int

	// SD-WAN configuration
	sdwanEnabled    bool
	meshTrustDomain string

	// XDP/AF_XDP interface for eBPF acceleration
	xdpInterface string

	// Force legacy paths (disable eBPF auto-detection)
	forceLegacyMesh bool

	// Rust dataplane connection
	dataplaneSocket string
)

// agentComponents holds all subsystem managers created during agent initialization.
type agentComponents struct {
	watcher       *config.Watcher
	gossiper      *gossip.ConfigGossiper
	afxdpWorker   *afxdp.Worker
	sockMapMgr    *sockmap.Manager
	meshManager   *mesh.Manager
	sdwanManager  *sdwan.Manager
	ebpfHealthMon *ebpfhealth.Monitor
	ebpfRL        *ebpfratelimit.RateLimiter
	dpClient      *dpctl.Client
	dpTranslator  *dpctl.Translator

	metricsServer *server.MetricsServer
	healthServer  *server.HealthServer
	adminServer   *server.AdminServer
}

func main() {
	parseFlags()

	// Validate required flags
	if nodeName == "" {
		fmt.Fprintf(os.Stderr, "Error: --node-name is required\n")
		os.Exit(1)
	}

	// Initialize logger
	logger, atomicLevel := initLogger(logLevel)
	defer func() { _ = logger.Sync() }()

	// Expose dynamic log level endpoint on the health probe port.
	http.Handle("/debug/loglevel", atomicLevel)

	logger.Info("Starting NovaEdge agent",
		zap.String("node", nodeName),
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("date", date),
		zap.String("controller", controllerAddr),
	)

	logger.Info("Rust dataplane configured",
		zap.String("dataplane_socket", dataplaneSocket),
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
		ServiceVersion: version,
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

	// Initialize all agent components
	comp := initAgentComponents(ctx, logger, atomicLevel)

	// Start all managers and servers
	startAgentManagers(ctx, logger, comp)

	// Run config watcher and wait for shutdown
	runAgentLoop(ctx, cancel, logger, comp)
}

// parseFlags registers and parses all CLI flags.
func parseFlags() {
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

	// Service mesh flags
	flag.BoolVar(&meshEnabled, "mesh-enabled", false, "Enable service mesh east-west traffic interception")
	flag.IntVar(&meshTPROXYPort, "mesh-tproxy-port", int(mesh.DefaultTPROXYPort), "Port for transparent proxy listener")
	flag.IntVar(&meshTunnelPort, "mesh-tunnel-port", int(mesh.DefaultTunnelPort), "Port for mTLS tunnel server")
	flag.StringVar(&meshTrustDomain, "mesh-trust-domain", "cluster.local", "SPIFFE trust domain for mesh identity")

	// SD-WAN flags
	flag.BoolVar(&sdwanEnabled, "sdwan-enabled", false, "Enable SD-WAN multi-link management")

	// XDP/AF_XDP interface — when set, eBPF acceleration is auto-attempted
	flag.StringVar(&xdpInterface, "xdp-interface", "", "Network interface for XDP/AF_XDP program attachment (enables eBPF acceleration when set)")

	// Force-legacy flags — explicitly disable eBPF auto-detection
	flag.BoolVar(&forceLegacyMesh, "force-legacy-mesh", false, "Force legacy nftables/iptables mesh interception instead of eBPF sk_lookup")

	// Rust dataplane socket
	flag.StringVar(&dataplaneSocket, "dataplane-socket", dpctl.DefaultDataplaneSocket,
		"Unix domain socket path for the Rust dataplane daemon")

	flag.Parse()
}

// initConfigWatcher creates the config watcher with optional mTLS and cluster identification.
func initConfigWatcher(ctx context.Context, logger *zap.Logger) *config.Watcher {
	var watcher *config.Watcher
	var err error
	isRemoteAgent := clusterName != ""

	switch {
	case isRemoteAgent:
		if grpcTLSCert == "" || grpcTLSKey == "" || grpcTLSCA == "" {
			logger.Fatal("Remote agents require mTLS configuration",
				zap.String("cluster", clusterName))
		}
		watcher, err = config.NewRemoteWatcher(ctx, nodeName, version, controllerAddr,
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
		watcher, err = config.NewWatcherWithTLS(ctx, nodeName, version, controllerAddr,
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
		watcher, err = config.NewWatcher(ctx, nodeName, version, controllerAddr, logger)
		if err != nil {
			logger.Fatal("Failed to create config watcher", zap.Error(err))
		}
		logger.Warn("WARNING: Config watcher running without TLS (insecure)")
	}
	return watcher
}

// initEBPFSubsystems creates AF_XDP subsystems.
func initEBPFSubsystems(ctx context.Context, logger *zap.Logger) (afxdpW *afxdp.Worker) {
	if xdpInterface != "" {
		afxdpLoader := novaebpf.NewProgramLoader(logger, "")
		afxdpW = afxdp.NewWorker(logger, afxdpLoader, afxdp.WorkerConfig{
			InterfaceName: xdpInterface,
			QueueID:       0,
		}, nil)
		go func() {
			if startErr := afxdpW.Start(ctx); startErr != nil {
				logger.Warn("AF_XDP not available, using kernel stack", zap.Error(startErr))
			}
		}()
		logger.Info("AF_XDP zero-copy fast path enabled", zap.String("interface", xdpInterface))
	}
	return
}

// initMeshSubsystem creates mesh manager and related eBPF accelerators.
func initMeshSubsystem(logger *zap.Logger) (*mesh.Manager, *sockmap.Manager) {
	if !meshEnabled {
		return nil, nil
	}

	sockMapMgr := ebpfmesh.TrySockMap(logger)

	var meshBackend mesh.RuleBackend
	if !forceLegacyMesh {
		meshBackend = ebpfmesh.TryBackend(logger)
	} else {
		logger.Info("eBPF mesh redirect disabled by --force-legacy-mesh, using nftables/iptables")
	}
	meshManager := mesh.NewManager(logger, mesh.ManagerConfig{
		TPROXYPort:          int32(meshTPROXYPort), //nolint:gosec // port range validated by flag
		TunnelPort:          int32(meshTunnelPort), //nolint:gosec // port range validated by flag
		TrustDomain:         meshTrustDomain,
		RuleBackendOverride: meshBackend,
		SockMapManager:      sockMapMgr,
	})
	return meshManager, sockMapMgr
}

// initEBPFMonitoring creates eBPF health monitor and rate limiter.
func initEBPFMonitoring(logger *zap.Logger) (*ebpfhealth.Monitor, *ebpfratelimit.RateLimiter) {
	ebpfHealthMon, err := ebpfhealth.NewMonitor(logger, 0)
	if err != nil {
		logger.Warn("eBPF health monitor not available, using active probes only", zap.Error(err))
		ebpfHealthMon = nil
	} else {
		logger.Info("eBPF passive health monitor enabled")
	}

	ebpfRL, err := ebpfratelimit.NewRateLimiter(logger, 0)
	if err != nil {
		logger.Warn("eBPF rate limiter not available, using Go-side token buckets", zap.Error(err))
		ebpfRL = nil
	} else {
		logger.Info("eBPF per-IP rate limiter enabled")
	}
	return ebpfHealthMon, ebpfRL
}

// initAgentComponents initializes all agent subsystem managers.
func initAgentComponents(ctx context.Context, logger *zap.Logger, atomicLevel zap.AtomicLevel) *agentComponents {
	comp := &agentComponents{}

	comp.watcher = initConfigWatcher(ctx, logger)

	comp.gossiper = gossip.NewConfigGossiper(nodeName, comp.watcher.ForceResync, logger)
	if err := comp.gossiper.Start(ctx); err != nil {
		logger.Warn("Failed to start config gossiper", zap.Error(err))
	}

	comp.afxdpWorker = initEBPFSubsystems(ctx, logger)
	comp.meshManager, comp.sockMapMgr = initMeshSubsystem(logger)

	if sdwanEnabled {
		comp.sdwanManager = sdwan.NewManager(logger)
	}

	comp.ebpfHealthMon, comp.ebpfRL = initEBPFMonitoring(logger)

	dpClient, dpErr := dpctl.NewClient(dataplaneSocket, logger.Named("dataplane"))
	if dpErr != nil {
		logger.Fatal("Failed to connect to Rust dataplane",
			zap.String("socket", dataplaneSocket),
			zap.Error(dpErr))
	}
	comp.dpClient = dpClient
	comp.dpTranslator = dpctl.NewTranslator(dpClient, logger.Named("dataplane"))
	logger.Info("Rust forwarding plane active: delegating all forwarding to dataplane daemon")

	comp.metricsServer = server.NewMetricsServer(logger, metricsPort)
	comp.healthServer = server.NewHealthServer(logger, healthProbePort)
	comp.adminServer = server.NewAdminServer("", logger)
	comp.adminServer.SetAtomicLevel(atomicLevel)

	return comp
}

// startAgentManagers starts mesh and SD-WAN managers.
func startAgentManagers(ctx context.Context, logger *zap.Logger, comp *agentComponents) {
	if comp.meshManager != nil {
		if err := comp.meshManager.Start(ctx); err != nil {
			logger.Fatal("Failed to start mesh manager", zap.Error(err))
		}
		meshConn, meshConnErr := createGRPCConnection(controllerAddr, grpcTLSCert, grpcTLSKey, grpcTLSCA)
		if meshConnErr != nil {
			logger.Fatal("Failed to create gRPC connection for mesh cert requester", zap.Error(meshConnErr))
		}
		comp.meshManager.StartCertRequester(ctx, nodeName, meshConn)
	}

	if comp.sdwanManager != nil {
		if err := comp.sdwanManager.Start(ctx); err != nil {
			logger.Fatal("Failed to start SD-WAN manager", zap.Error(err))
		}
	}
}

// runAgentLoop starts the config watcher and servers, then waits for shutdown.
func runAgentLoop(ctx context.Context, cancel context.CancelFunc, logger *zap.Logger, comp *agentComponents) {
	snapshotHolder := introspection.NewSnapshotHolder()
	introServer := introspection.NewServer(snapshotHolder, logger)
	go func() {
		if introErr := introServer.Start(ctx, ":9092"); introErr != nil {
			logger.Error("introspection server failed", zap.Error(introErr))
		}
	}()

	configChan := make(chan error, 1)
	go func() {
		configChan <- comp.watcher.Start(func(snapshot *config.Snapshot) error {
			return applyAgentConfig(ctx, logger, comp, snapshotHolder, snapshot)
		})
	}()

	metricsChan := make(chan error, 1)
	go func() { metricsChan <- comp.metricsServer.Start(ctx) }()

	healthChan := make(chan error, 1)
	go func() { healthChan <- comp.healthServer.Start(ctx) }()

	adminChan := make(chan error, 1)
	go func() { adminChan <- comp.adminServer.Start(ctx) }()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-configChan:
		logger.Error("Config watcher failed", zap.Error(err))
	case err := <-metricsChan:
		logger.Error("Metrics server failed", zap.Error(err))
	case err := <-healthChan:
		logger.Error("Health probe failed", zap.Error(err))
	case err := <-adminChan:
		logger.Error("Admin server failed", zap.Error(err))
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	}

	logger.Info("Shutting down...")
	cancel()

	shutdownAgent(logger, comp)
}

// applyAgentConfig applies a new configuration snapshot to all agent subsystems.
func applyAgentConfig(ctx context.Context, logger *zap.Logger, comp *agentComponents, snapshotHolder *introspection.SnapshotHolder, snapshot *config.Snapshot) error {
	logger.Info("Applying new configuration",
		zap.String("version", snapshot.Version),
		zap.Int("gateways", len(snapshot.Gateways)),
		zap.Int("routes", len(snapshot.Routes)),
	)

	snapshotHolder.Store(snapshot.ConfigSnapshot)

	if syncErr := comp.dpTranslator.Sync(ctx, snapshot.ConfigSnapshot); syncErr != nil {
		logger.Error("Failed to sync config to Rust dataplane", zap.Error(syncErr))
		comp.healthServer.SetReady(false)
		comp.adminServer.SetReady(false)
		return syncErr
	}

	if comp.meshManager != nil {
		if err := comp.meshManager.ApplyConfig(
			snapshot.GetInternalServices(),
			snapshot.GetMeshAuthzPolicies(),
		); err != nil {
			logger.Error("Failed to apply mesh config", zap.Error(err))
		}
	}

	if comp.sdwanManager != nil {
		links := convertWANLinks(snapshot.GetWanLinks())
		policies := convertWANPolicies(snapshot.GetWanPolicies())
		if err := comp.sdwanManager.ApplyConfig(links, policies); err != nil {
			logger.Error("Failed to apply SD-WAN config", zap.Error(err))
		}
	}

	if comp.ebpfRL != nil && comp.ebpfRL.IsActive() {
		configureEBPFRateLimiter(comp.ebpfRL, snapshot.GetPolicies(), logger)
	}

	startEBPFHealthPoller(comp.ebpfHealthMon, logger)

	comp.healthServer.SetReady(true)
	comp.adminServer.SetSnapshot(snapshot)
	comp.adminServer.SetReady(true)

	comp.gossiper.UpdateGenTime(snapshot.GenerationTime)
	return nil
}

// closeIfNotNil calls Close() on c if it is not nil, logging any error.
func closeIfNotNil(logger *zap.Logger, name string, c interface{ Close() error }) {
	if c != nil {
		if err := c.Close(); err != nil {
			logger.Error("Error closing "+name, zap.Error(err))
		}
	}
}

// shutdownAgent performs graceful shutdown of all agent subsystems.
func shutdownAgent(logger *zap.Logger, comp *agentComponents) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if comp.meshManager != nil {
		if err := comp.meshManager.Shutdown(shutdownCtx); err != nil {
			logger.Error("Error during mesh manager shutdown", zap.Error(err))
		}
	}
	if comp.dpClient != nil {
		if err := comp.dpClient.Close(); err != nil {
			logger.Error("Error closing dataplane client", zap.Error(err))
		}
	}
	if comp.sdwanManager != nil {
		comp.sdwanManager.Stop()
	}
	if comp.afxdpWorker != nil {
		if err := comp.afxdpWorker.Stop(); err != nil {
			logger.Error("Error during AF_XDP worker shutdown", zap.Error(err))
		}
	}

	// Cleanup eBPF subsystem resources (idempotent Close methods).
	closeIfNotNil(logger, "eBPF health monitor", comp.ebpfHealthMon)
	closeIfNotNil(logger, "eBPF rate limiter", comp.ebpfRL)

	if err := comp.metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during metrics server shutdown", zap.Error(err))
	}
	if err := comp.adminServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during admin server shutdown", zap.Error(err))
	}

	logger.Info("Agent stopped")
}

func initLogger(level string) (*zap.Logger, zap.AtomicLevel) {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	atomicLevel := zap.NewAtomicLevelAt(zapLevel)

	// Create logger config
	config := zap.Config{
		Level:            atomicLevel,
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

	return logger, atomicLevel
}

// createGRPCConnection creates a gRPC client connection to the controller.
// If TLS cert/key/CA are provided, it uses mTLS; otherwise insecure.
func createGRPCConnection(addr, certFile, keyFile, caFile string) (*grpc.ClientConn, error) {
	opts := grpclimits.ClientOptions()

	if certFile != "" && keyFile != "" && caFile != "" {
		creds, err := tlsutil.LoadClientTLSCredentials(certFile, keyFile, caFile, "")
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// Config-callback helper functions: translate proto types into Go manager types
// ---------------------------------------------------------------------------

// convertWANLinks translates proto WANLinks into sdwan.LinkConfig values.
func convertWANLinks(wanLinks []*pb.WANLink) []sdwan.LinkConfig {
	links := make([]sdwan.LinkConfig, 0, len(wanLinks))
	for _, wl := range wanLinks {
		lc := sdwan.LinkConfig{
			Name:      wl.GetName(),
			Site:      wl.GetSite(),
			Provider:  wl.GetProvider(),
			Role:      sdwan.WANLinkRole(wl.GetRole()),
			Bandwidth: wl.GetBandwidth(),
			Cost:      wl.GetCost(),
		}
		if sla := wl.GetSla(); sla != nil {
			lc.SLA = &sdwan.WANLinkSLA{
				MaxLatencyMs:  float64(sla.GetMaxLatencyMs()),
				MaxJitterMs:   float64(sla.GetMaxJitterMs()),
				MaxPacketLoss: sla.GetMaxPacketLoss(),
			}
		}
		if ep := wl.GetTunnelEndpoint(); ep != nil {
			lc.TunnelEndpoint = &sdwan.TunnelEndpoint{
				PublicIP: ep.GetPublicIp(),
				Port:     ep.GetPort(),
			}
		}
		links = append(links, lc)
	}
	return links
}

// convertWANPolicies translates proto WANPolicies into sdwan.PolicyConfig values.
func convertWANPolicies(wanPolicies []*pb.WANPolicy) []sdwan.PolicyConfig {
	policies := make([]sdwan.PolicyConfig, 0, len(wanPolicies))
	for _, wp := range wanPolicies {
		pc := sdwan.PolicyConfig{
			Name: wp.GetName(),
		}
		if ps := wp.GetPathSelection(); ps != nil {
			pc.Strategy = ps.GetStrategy()
			pc.Failover = ps.GetFailover()
			pc.DSCPClass = ps.GetDscpClass()
		}
		if m := wp.GetMatch(); m != nil {
			pc.MatchHosts = m.GetHosts()
			pc.MatchPaths = m.GetPaths()
			pc.MatchHeaders = m.GetHeaders()
		}
		policies = append(policies, pc)
	}
	return policies
}

// configureEBPFRateLimiter extracts rate-limit configuration from snapshot policies
// and applies it to the eBPF per-IP rate limiter.
func configureEBPFRateLimiter(rl *ebpfratelimit.RateLimiter, policies []*pb.Policy, logger *zap.Logger) {
	for _, pol := range policies {
		if pol.GetType() != pb.PolicyType_RATE_LIMIT {
			continue
		}
		rlCfg := pol.GetRateLimit()
		if rlCfg == nil {
			continue
		}
		rps := rlCfg.GetRequestsPerSecond()
		b := rlCfg.GetBurst()
		if rps < 0 {
			rps = 0
		}
		if b < 0 {
			b = 0
		}
		rate := uint64(rps) //nolint:gosec // guarded above
		burst := uint64(b)  //nolint:gosec // guarded above
		if rate == 0 {
			continue
		}
		if burst == 0 {
			burst = rate // default burst = rate
		}
		if err := rl.Configure(rate, burst); err != nil {
			logger.Error("Failed to configure eBPF rate limiter",
				zap.Uint64("rate", rate),
				zap.Uint64("burst", burst),
				zap.Error(err))
		} else {
			logger.Debug("eBPF rate limiter configured",
				zap.String("policy", pol.GetName()),
				zap.Uint64("rate", rate),
				zap.Uint64("burst", burst))
		}
		return // use first rate-limit policy
	}
}

// ebpfHealthPollerOnce ensures the eBPF health monitor poller is started at most once.
var ebpfHealthPollerOnce sync.Once

// startEBPFHealthPoller starts the eBPF passive health monitor's polling goroutine
// exactly once (on first config snapshot). Subsequent calls are no-ops.
func startEBPFHealthPoller(mon *ebpfhealth.Monitor, logger *zap.Logger) {
	if mon == nil || !mon.IsActive() {
		return
	}
	ebpfHealthPollerOnce.Do(func() {
		// Poll eBPF health map every 10 seconds; log aggregated results.
		mon.StartPoller(context.Background(), 10*time.Second, func(results map[ebpfhealth.BackendKey]ebpfhealth.AggregatedHealth) {
			if len(results) > 0 {
				logger.Debug("eBPF passive health update",
					zap.Int("backends", len(results)))
			}
		})
		logger.Info("eBPF passive health monitor poller started")
	})
}
