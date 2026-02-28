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
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/piwi3910/novaedge/internal/agent/afxdp"
	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/cpvip"
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"github.com/piwi3910/novaedge/internal/agent/ebpf/conntrack"
	ebpfhealth "github.com/piwi3910/novaedge/internal/agent/ebpf/health"
	"github.com/piwi3910/novaedge/internal/agent/ebpf/maglev"
	ebpfratelimit "github.com/piwi3910/novaedge/internal/agent/ebpf/ratelimit"
	ebpfservice "github.com/piwi3910/novaedge/internal/agent/ebpf/service"
	"github.com/piwi3910/novaedge/internal/agent/ebpf/sockmap"
	"github.com/piwi3910/novaedge/internal/agent/ebpfmesh"
	"github.com/piwi3910/novaedge/internal/agent/introspection"
	"github.com/piwi3910/novaedge/internal/agent/mesh"
	"github.com/piwi3910/novaedge/internal/agent/sdwan"
	"github.com/piwi3910/novaedge/internal/agent/server"
	"github.com/piwi3910/novaedge/internal/agent/vip"
	"github.com/piwi3910/novaedge/internal/agent/xdplb"
	dpctl "github.com/piwi3910/novaedge/internal/dataplane"
	"github.com/piwi3910/novaedge/internal/observability"
	"github.com/piwi3910/novaedge/internal/pkg/grpclimits"
	"github.com/piwi3910/novaedge/internal/pkg/tlsutil"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

var (
	errInvalidBGPPeerFormat = errors.New("invalid BGP peer format")
	errInvalidBGPPeerIP     = errors.New("invalid BGP peer IP")
)

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(val string) error {
	*s = append(*s, val)
	return nil
}

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

	// Control-plane VIP mode
	controlPlaneVIP  bool
	cpVIPAddress     string
	cpVIPInterface   string
	cpAPIPort        int
	cpHealthInterval time.Duration
	cpHealthTimeout  time.Duration

	// Shutdown drain period
	shutdownDrainPeriod time.Duration

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
	forceLegacyLB   bool
	forceLegacyMesh bool

	// BGP backend selection
	bgpBackend      string
	novarouteSocket string
	novarouteOwner  string
	novarouteToken  string

	// Control-plane VIP BGP/BFD configuration
	cpVIPMode       string
	cpBGPLocalAS    uint
	cpBGPRouterID   string
	cpBGPPeers      stringSlice
	cpBFDEnabled    bool
	cpBFDDetectMult int
	cpBFDTxInterval string
	cpBFDRxInterval string

	// Rust dataplane connection
	dataplaneSocket string
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

	// Control-plane VIP mode flags
	flag.BoolVar(&controlPlaneVIP, "control-plane-vip", false, "Enable control-plane VIP mode for kube-apiserver HA")
	flag.StringVar(&cpVIPAddress, "cp-vip-address", "", "Control-plane VIP address in CIDR notation (e.g., 10.0.0.100/32)")
	flag.StringVar(&cpVIPInterface, "cp-vip-interface", "", "Network interface for control-plane VIP (auto-detect if empty)")
	flag.IntVar(&cpAPIPort, "cp-api-port", 6443, "Kube-apiserver port for health checks")
	flag.DurationVar(&cpHealthInterval, "cp-health-interval", time.Second, "Health check interval for control-plane VIP")
	flag.DurationVar(&cpHealthTimeout, "cp-health-timeout", 3*time.Second, "Health check timeout for control-plane VIP")

	// Shutdown drain period
	flag.DurationVar(&shutdownDrainPeriod, "shutdown-drain-period", 3*time.Second,
		"Delay between VIP release and server shutdown, allowing upstream routers to converge after BGP/OSPF withdrawal")

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
	flag.BoolVar(&forceLegacyLB, "force-legacy-lb", false, "Force legacy userspace L4 proxy instead of XDP/AF_XDP acceleration")
	flag.BoolVar(&forceLegacyMesh, "force-legacy-mesh", false, "Force legacy nftables/iptables mesh interception instead of eBPF sk_lookup")

	// BGP backend flags
	flag.StringVar(&bgpBackend, "bgp-backend", "gobgp", "BGP backend for VIP announcements: gobgp (built-in) or novaroute (delegated to NovaRoute agent)")
	flag.StringVar(&novarouteSocket, "novaroute-socket", "/run/novaroute/novaroute.sock", "Unix domain socket path for NovaRoute gRPC API")
	flag.StringVar(&novarouteOwner, "novaroute-owner", "novaedge", "Owner name for NovaRoute registration")
	flag.StringVar(&novarouteToken, "novaroute-token", "", "Authentication token for NovaRoute registration")

	// Control-plane VIP BGP/BFD flags
	flag.StringVar(&cpVIPMode, "cp-vip-mode", "l2", "Control-plane VIP mode: l2 or bgp")
	flag.UintVar(&cpBGPLocalAS, "cp-bgp-local-as", 0, "Local BGP AS number for CP VIP (required for bgp mode)")
	flag.StringVar(&cpBGPRouterID, "cp-bgp-router-id", "", "BGP router ID for CP VIP (default: auto from node IP)")
	flag.Var(&cpBGPPeers, "cp-bgp-peer", "BGP peer in format IP:AS[:PORT] (repeatable)")
	flag.BoolVar(&cpBFDEnabled, "cp-bfd-enabled", false, "Enable BFD for CP VIP")
	flag.IntVar(&cpBFDDetectMult, "cp-bfd-detect-mult", 3, "BFD detect multiplier for CP VIP")
	flag.StringVar(&cpBFDTxInterval, "cp-bfd-tx-interval", "300ms", "BFD minimum TX interval for CP VIP")
	flag.StringVar(&cpBFDRxInterval, "cp-bfd-rx-interval", "300ms", "BFD minimum RX interval for CP VIP")

	// Rust dataplane socket
	flag.StringVar(&dataplaneSocket, "dataplane-socket", dpctl.DefaultDataplaneSocket,
		"Unix domain socket path for the Rust dataplane daemon")

	flag.Parse()

	// Validate required flags
	if nodeName == "" {
		fmt.Fprintf(os.Stderr, "Error: --node-name is required\n")
		os.Exit(1)
	}

	// Initialize logger
	logger, atomicLevel := initLogger(logLevel)
	defer func() { _ = logger.Sync() }()

	// Expose dynamic log level endpoint on the health probe port.
	// PUT /debug/loglevel with body like "debug" or "info" to change at runtime.
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

	// Control-plane VIP mode: run a simplified agent that only manages
	// a VIP for kube-apiserver HA, without connecting to the controller.
	if controlPlaneVIP {
		runControlPlaneVIPMode(logger)
		return
	}

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
		// Local agent with mTLS enabled
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
		// Local agent without TLS (insecure, development only)
		watcher, err = config.NewWatcher(ctx, nodeName, version, controllerAddr, logger)
		if err != nil {
			logger.Fatal("Failed to create config watcher", zap.Error(err))
		}
		logger.Warn("WARNING: Config watcher running without TLS (insecure)")
	}

	// Create VIP manager with selected BGP backend
	var vipOpts []vip.ManagerOption
	switch bgpBackend {
	case "novaroute":
		logger.Info("Using NovaRoute BGP backend",
			zap.String("socket", novarouteSocket),
			zap.String("owner", novarouteOwner),
		)
		nrHandler := vip.NewNovaRouteBGPHandler(logger, novarouteSocket, novarouteOwner, novarouteToken)
		vipOpts = append(vipOpts, vip.WithBGPBackend(nrHandler))
	case "gobgp", "":
		logger.Info("Using built-in GoBGP backend")
	default:
		logger.Fatal("Unknown BGP backend", zap.String("backend", bgpBackend))
	}
	vipManager, err := vip.NewManager(logger, vipOpts...)
	if err != nil {
		logger.Fatal("Failed to create VIP manager", zap.Error(err))
	}

	// Create XDP LB manager — auto-attempted when xdpInterface is set
	var xdpManager *xdplb.Manager
	if !forceLegacyLB && xdpInterface != "" {
		ebpfLoader := novaebpf.NewProgramLoader(logger, "")
		xdpManager = xdplb.NewManager(logger, ebpfLoader, xdpInterface)
		if err := xdpManager.Start(); err != nil {
			logger.Warn("XDP L4 LB not available, using userspace proxy",
				zap.Error(err))
			xdpManager = nil
		} else {
			logger.Info("XDP L4 load balancing active",
				zap.String("interface", xdpInterface))
		}
	} else if forceLegacyLB {
		logger.Info("XDP L4 LB disabled by --force-legacy-lb, using userspace proxy")
	}

	// Create eBPF Maglev manager for consistent hashing (attaches to XDP LB)
	var maglevMgr *maglev.Manager
	var conntrackMgr *conntrack.Conntrack
	if xdpManager != nil && xdpManager.IsRunning() {
		maglevMgr = maglev.NewManager(logger, 0) // 0 = DefaultTableSize
		if initErr := maglevMgr.Init(); initErr != nil {
			logger.Warn("eBPF Maglev not available, using hash-mod backend selection", zap.Error(initErr))
			maglevMgr = nil
		} else {
			xdpManager.SetMaglev(maglevMgr)
			logger.Info("eBPF Maglev consistent hashing enabled for XDP LB")
		}

		var ctErr error
		conntrackMgr, ctErr = conntrack.NewConntrack(logger, 0, 0) // 0 = defaults
		if ctErr != nil {
			logger.Warn("eBPF conntrack not available", zap.Error(ctErr))
			conntrackMgr = nil
		} else {
			xdpManager.SetConntrack(conntrackMgr)
			conntrackMgr.StartGC()
			logger.Info("eBPF conntrack enabled for XDP LB")
		}
	}

	// Create AF_XDP worker — auto-attempted when xdpInterface is set
	var afxdpWorker *afxdp.Worker
	if !forceLegacyLB && xdpInterface != "" {
		afxdpLoader := novaebpf.NewProgramLoader(logger, "")
		afxdpWorker = afxdp.NewWorker(logger, afxdpLoader, afxdp.WorkerConfig{
			InterfaceName: xdpInterface,
			QueueID:       0,
		}, nil)
		go func() {
			if startErr := afxdpWorker.Start(ctx); startErr != nil {
				logger.Warn("AF_XDP not available, using kernel stack",
					zap.Error(startErr))
			}
		}()
		logger.Info("AF_XDP zero-copy fast path enabled",
			zap.String("interface", xdpInterface))
	}

	// Create eBPF SOCKMAP + ServiceMap for mesh acceleration (if mesh enabled).
	// TrySockMap/TryServiceMap auto-detect kernel capabilities and return nil
	// when eBPF prerequisites are missing — no fatal errors.
	var sockMapMgr *sockmap.Manager
	var ebpfSvcMap *ebpfservice.ServiceMap
	if meshEnabled {
		sockMapMgr = ebpfmesh.TrySockMap(logger)
		ebpfSvcMap = ebpfmesh.TryServiceMap(logger, 0, 0) // 0 = defaults
	}

	// Create mesh manager (if enabled)
	var meshManager *mesh.Manager
	if meshEnabled {
		// eBPF sk_lookup is auto-attempted; falls back to nftables/iptables
		// if the kernel doesn't support it or --force-legacy-mesh is set.
		var meshBackend mesh.RuleBackend
		if !forceLegacyMesh {
			meshBackend = ebpfmesh.TryBackend(logger)
		} else {
			logger.Info("eBPF mesh redirect disabled by --force-legacy-mesh, using nftables/iptables")
		}
		meshManager = mesh.NewManager(logger, mesh.ManagerConfig{
			TPROXYPort:          int32(meshTPROXYPort), //nolint:gosec // port range validated by flag
			TunnelPort:          int32(meshTunnelPort), //nolint:gosec // port range validated by flag
			TrustDomain:         meshTrustDomain,
			RuleBackendOverride: meshBackend,
			SockMapManager:      sockMapMgr,
			ServiceMap:          ebpfSvcMap,
		})
	}

	// Create SD-WAN manager (if enabled)
	var sdwanManager *sdwan.Manager
	if sdwanEnabled {
		sdwanManager = sdwan.NewManager(logger)
	}

	// Create eBPF health monitor for passive health signals from kernel
	var ebpfHealthMon *ebpfhealth.HealthMonitor
	ebpfHealthMon, err = ebpfhealth.NewHealthMonitor(logger, 0) // 0 = default (4096)
	if err != nil {
		logger.Warn("eBPF health monitor not available, using active probes only", zap.Error(err))
		ebpfHealthMon = nil
	} else {
		logger.Info("eBPF passive health monitor enabled")
	}

	// Create eBPF rate limiter for per-IP fast-path rate limiting
	var ebpfRL *ebpfratelimit.RateLimiter
	ebpfRL, err = ebpfratelimit.NewRateLimiter(logger, 0) // 0 = default (100k entries)
	if err != nil {
		logger.Warn("eBPF rate limiter not available, using Go-side token buckets", zap.Error(err))
		ebpfRL = nil
	} else {
		logger.Info("eBPF per-IP rate limiter enabled")
	}

	// Create dataplane client and translator for Rust forwarding plane
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

	// Create admin/debug server (pprof, stats, config introspection)
	adminServer := server.NewAdminServer("", logger)
	adminServer.SetAtomicLevel(atomicLevel)

	// Start VIP manager
	if err := vipManager.Start(ctx); err != nil {
		logger.Fatal("Failed to start VIP manager", zap.Error(err))
	}

	// Start mesh manager (if enabled)
	if meshManager != nil {
		if err := meshManager.Start(ctx); err != nil {
			logger.Fatal("Failed to start mesh manager", zap.Error(err))
		}

		// Start mesh certificate requester in background.
		// Creates a separate gRPC connection to the controller for cert requests.
		meshConn, meshConnErr := createGRPCConnection(controllerAddr, grpcTLSCert, grpcTLSKey, grpcTLSCA)
		if meshConnErr != nil {
			logger.Fatal("Failed to create gRPC connection for mesh cert requester", zap.Error(meshConnErr))
		}
		meshManager.StartCertRequester(ctx, nodeName, meshConn)
	}

	// Start SD-WAN manager (if enabled)
	if sdwanManager != nil {
		if err := sdwanManager.Start(ctx); err != nil {
			logger.Fatal("Failed to start SD-WAN manager", zap.Error(err))
		}
	}

	// Start config watcher
	// Create snapshot holder and introspection server
	snapshotHolder := introspection.NewSnapshotHolder()
	introServer := introspection.NewServer(snapshotHolder, logger)
	go func() {
		if introErr := introServer.Start(ctx, ":9092"); introErr != nil {
			logger.Error("introspection server failed", zap.Error(introErr))
		}
	}()

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

			// Store snapshot for introspection
			snapshotHolder.Store(snapshot.ConfigSnapshot)

			// Sync all config to Rust dataplane (L7 forwarding, routing, policies).
			if syncErr := dpTranslator.Sync(ctx, snapshot.ConfigSnapshot); syncErr != nil {
				logger.Error("Failed to sync config to Rust dataplane", zap.Error(syncErr))
				healthServer.SetReady(false)
				adminServer.SetReady(false)
				return syncErr
			}

			// --- Wire Go-side managers for host-level operations ---

			// VIP manager: bind/unbind VIPs to interfaces, send GARPs, manage BGP/OSPF.
			if err := vipManager.ApplyVIPs(ctx, snapshot.GetVipAssignments()); err != nil {
				logger.Error("Failed to apply VIPs", zap.Error(err))
			}

			// Mesh manager: reconcile TPROXY/nftables rules, update service table.
			if meshManager != nil {
				if err := meshManager.ApplyConfig(
					snapshot.GetInternalServices(),
					snapshot.GetMeshAuthzPolicies(),
				); err != nil {
					logger.Error("Failed to apply mesh config", zap.Error(err))
				}
			}

			// SD-WAN manager: reconcile WAN links and path-selection policies.
			if sdwanManager != nil {
				links := convertWANLinks(snapshot.GetWanLinks())
				policies := convertWANPolicies(snapshot.GetWanPolicies())
				if err := sdwanManager.ApplyConfig(links, policies); err != nil {
					logger.Error("Failed to apply SD-WAN config", zap.Error(err))
				}
			}

			// XDP LB: populate eBPF VIP/backend maps for kernel-level L4 load balancing.
			if xdpManager != nil && xdpManager.IsRunning() {
				routes := buildL4Routes(snapshot.GetVipAssignments(), snapshot.GetL4Listeners(), snapshot.GetEndpoints())
				if err := xdpManager.SyncBackends(routes); err != nil {
					logger.Error("Failed to sync XDP LB backends", zap.Error(err))
				}
			}

			// AF_XDP: update VIP filter so the zero-copy fast path knows which packets to intercept.
			if afxdpWorker != nil && afxdpWorker.IsRunning() {
				vipKeys := buildAFXDPVIPKeys(snapshot.GetVipAssignments())
				if err := afxdpWorker.SyncVIPs(vipKeys); err != nil {
					logger.Error("Failed to sync AF_XDP VIPs", zap.Error(err))
				}
			}

			// eBPF rate limiter: configure from the first rate-limit policy found.
			if ebpfRL != nil && ebpfRL.IsActive() {
				configureEBPFRateLimiter(ebpfRL, snapshot.GetPolicies(), logger)
			}

			// eBPF health monitor: start the poller once on first config (idempotent).
			startEBPFHealthPoller(ebpfHealthMon, logger)

			healthServer.SetReady(true)
			adminServer.SetSnapshot(snapshot)
			adminServer.SetReady(true)
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

	// Start admin/debug server (pprof, stats, config introspection on 127.0.0.1:9901)
	adminChan := make(chan error, 1)
	go func() {
		adminChan <- adminServer.Start(ctx)
	}()

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

	// Wait for upstream routers to converge after BGP/OSPF route withdrawal
	// before shutting down servers. This prevents dropping in-flight requests.
	if shutdownDrainPeriod > 0 {
		logger.Info("Draining connections after VIP release...",
			zap.Duration("drain_period", shutdownDrainPeriod))
		select {
		case <-time.After(shutdownDrainPeriod):
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timeout reached during drain period")
		}
	}

	// Shutdown mesh manager (removes iptables rules)
	if meshManager != nil {
		if err := meshManager.Shutdown(shutdownCtx); err != nil {
			logger.Error("Error during mesh manager shutdown", zap.Error(err))
		}
	}

	// Shutdown dataplane client
	if dpClient != nil {
		if err := dpClient.Close(); err != nil {
			logger.Error("Error closing dataplane client", zap.Error(err))
		}
	}

	// Shutdown SD-WAN manager
	if sdwanManager != nil {
		sdwanManager.Stop()
	}

	// Shutdown XDP LB manager
	if xdpManager != nil {
		if err := xdpManager.Stop(); err != nil {
			logger.Error("Error during XDP LB manager shutdown", zap.Error(err))
		}
	}

	// Shutdown AF_XDP worker
	if afxdpWorker != nil {
		if err := afxdpWorker.Stop(); err != nil {
			logger.Error("Error during AF_XDP worker shutdown", zap.Error(err))
		}
	}

	// Cleanup eBPF subsystem resources.
	// Note: Maglev and conntrack are cleaned up by xdpManager.Stop() above,
	// but we also close them here in case xdpManager was nil or failed to
	// stop them. The Close methods are idempotent.
	if maglevMgr != nil {
		if err := maglevMgr.Close(); err != nil {
			logger.Error("Error closing eBPF Maglev manager", zap.Error(err))
		}
	}
	if conntrackMgr != nil {
		if err := conntrackMgr.Close(); err != nil {
			logger.Error("Error closing eBPF conntrack", zap.Error(err))
		}
	}
	if ebpfHealthMon != nil {
		if err := ebpfHealthMon.Close(); err != nil {
			logger.Error("Error closing eBPF health monitor", zap.Error(err))
		}
	}
	if ebpfRL != nil {
		if err := ebpfRL.Close(); err != nil {
			logger.Error("Error closing eBPF rate limiter", zap.Error(err))
		}
	}
	// Note: sockMapMgr and ebpfSvcMap are closed by meshManager.Shutdown() above.

	// Shutdown metrics server
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during metrics server shutdown", zap.Error(err))
	}

	// Shutdown admin server
	if err := adminServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during admin server shutdown", zap.Error(err))
	}

	logger.Info("Agent stopped")
}

// parseBGPPeer parses a peer string in the format "IP:AS[:PORT]" into a BGPPeerConfig.
func parseBGPPeer(peerStr string) (cpvip.BGPPeerConfig, error) {
	parts := strings.Split(peerStr, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return cpvip.BGPPeerConfig{}, fmt.Errorf("%w: %q: expected IP:AS[:PORT]", errInvalidBGPPeerFormat, peerStr)
	}

	if net.ParseIP(parts[0]) == nil {
		return cpvip.BGPPeerConfig{}, fmt.Errorf("%w: %q", errInvalidBGPPeerIP, parts[0])
	}

	peerAS, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return cpvip.BGPPeerConfig{}, fmt.Errorf("invalid BGP peer AS %q: %w", parts[1], err)
	}

	var port uint16 = 179
	if len(parts) == 3 {
		p, err := strconv.ParseUint(parts[2], 10, 16)
		if err != nil {
			return cpvip.BGPPeerConfig{}, fmt.Errorf("invalid BGP peer port %q: %w", parts[2], err)
		}
		port = uint16(p)
	}

	return cpvip.BGPPeerConfig{
		Address: parts[0],
		AS:      uint32(peerAS),
		Port:    port,
	}, nil
}

// runControlPlaneVIPMode runs the agent in control-plane VIP mode.
// This mode manages a single VIP for kube-apiserver HA without requiring
// the NovaEdge controller, making it suitable for pre-bootstrap scenarios.
func runControlPlaneVIPMode(logger *zap.Logger) {
	logger.Info("Running in control-plane VIP mode",
		zap.String("vip", cpVIPAddress),
		zap.String("mode", cpVIPMode),
		zap.String("interface", cpVIPInterface),
		zap.Int("api_port", cpAPIPort),
	)

	// Validate required flags
	if cpVIPAddress == "" {
		logger.Fatal("--cp-vip-address is required when --control-plane-vip is enabled")
	}

	// Parse BGP peers
	bgpPeers := make([]cpvip.BGPPeerConfig, 0, len(cpBGPPeers))
	for _, peerStr := range cpBGPPeers {
		peer, err := parseBGPPeer(peerStr)
		if err != nil {
			logger.Fatal("Failed to parse BGP peer", zap.String("peer", peerStr), zap.Error(err))
		}
		bgpPeers = append(bgpPeers, peer)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Validate integer bounds before narrowing conversions
	if cpBGPLocalAS > math.MaxUint32 {
		logger.Fatal("--cp-bgp-local-as exceeds maximum uint32 value", zap.Uint("value", cpBGPLocalAS))
	}
	if cpBFDDetectMult < 0 || cpBFDDetectMult > math.MaxInt32 {
		logger.Fatal("--cp-bfd-detect-mult out of int32 range", zap.Int("value", cpBFDDetectMult))
	}

	// Create CP VIP manager
	cpManager, err := cpvip.NewManager(cpvip.Config{
		VIPAddress:     cpVIPAddress,
		Interface:      cpVIPInterface,
		APIPort:        cpAPIPort,
		HealthInterval: cpHealthInterval,
		HealthTimeout:  cpHealthTimeout,
		Mode:           cpVIPMode,
		BGPLocalAS:     uint32(cpBGPLocalAS), //nolint:gosec // bounds checked above
		BGPRouterID:    cpBGPRouterID,
		BGPPeers:       bgpPeers,
		BFDEnabled:     cpBFDEnabled,
		BFDDetectMult:  int32(cpBFDDetectMult), //nolint:gosec // bounds checked above
		BFDTxInterval:  cpBFDTxInterval,
		BFDRxInterval:  cpBFDRxInterval,
	}, logger)
	if err != nil {
		logger.Fatal("Failed to create control-plane VIP manager", zap.Error(err))
	}

	// Create metrics server
	metricsServer := server.NewMetricsServer(logger, metricsPort)

	// Create health probe server
	healthServer := server.NewHealthServer(logger, healthProbePort)

	// Mark health probe as ready (CP VIP mode is always ready once started)
	healthServer.SetReady(true)

	// Start CP VIP manager
	cpvipChan := make(chan error, 1)
	go func() {
		cpvipChan <- cpManager.Start(ctx)
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
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-cpvipChan:
		logger.Error("CP VIP manager failed", zap.Error(err))
	case err := <-metricsChan:
		logger.Error("Metrics server failed", zap.Error(err))
	case err := <-healthChan:
		logger.Error("Health probe failed", zap.Error(err))
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	}

	// Graceful shutdown
	logger.Info("Shutting down control-plane VIP mode...")
	cancel()

	// Release VIP
	if err := cpManager.Stop(); err != nil {
		logger.Error("Error stopping CP VIP manager", zap.Error(err))
	}

	// Shutdown metrics server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error during metrics server shutdown", zap.Error(err))
	}

	logger.Info("Agent stopped (control-plane VIP mode)")
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

// buildL4Routes constructs xdplb.L4Route entries from VIP assignments, L4 listeners,
// and cluster endpoints. This enables the XDP eBPF program to perform kernel-level
// L4 load balancing by populating the vip_backends and backend_list eBPF maps.
func buildL4Routes(
	vips []*pb.VIPAssignment,
	l4Listeners []*pb.L4Listener,
	endpoints map[string]*pb.EndpointList,
) []xdplb.L4Route {
	// Build a VIP address set for quick lookup.
	vipAddrs := make(map[string]bool, len(vips))
	for _, v := range vips {
		// Strip CIDR suffix if present (e.g., "10.0.0.1/32" -> "10.0.0.1").
		addr := v.GetAddress()
		if idx := strings.IndexByte(addr, '/'); idx >= 0 {
			addr = addr[:idx]
		}
		vipAddrs[addr] = true
	}

	var routes []xdplb.L4Route
	for _, l4 := range l4Listeners {
		backendName := l4.GetBackendName()
		if backendName == "" {
			continue
		}
		epList, ok := endpoints[backendName]
		if !ok || epList == nil {
			continue
		}

		var protocol uint8 = 6 // TCP default
		switch l4.GetProtocol() {
		case pb.Protocol_UDP:
			protocol = 17
		case pb.Protocol_TCP, pb.Protocol_TLS:
			protocol = 6
		}

		var backends []xdplb.Backend
		for _, ep := range epList.GetEndpoints() {
			if !ep.GetReady() {
				continue
			}
			backends = append(backends, xdplb.Backend{
				Addr: ep.GetAddress(),
				Port: uint16(ep.GetPort()), //nolint:gosec // port range
			})
			// Note: Backend.MAC is left as zero value. The XDP program will
			// need ARP resolution or a neighbor-cache lookup to fill this.
			// For now, the backends are populated without MAC addresses;
			// XDP_TX will fall back to the kernel stack for MAC resolution.
		}

		if len(backends) == 0 {
			continue
		}

		// Create a route for each VIP address that matches.
		// L4 listeners don't specify a VIP directly, so we create a route
		// for every VIP on the matching port.
		for vipAddr := range vipAddrs {
			routes = append(routes, xdplb.L4Route{
				VIP:      vipAddr,
				Port:     uint16(l4.GetPort()), //nolint:gosec // port range
				Protocol: protocol,
				Backends: backends,
			})
		}
	}

	return routes
}

// buildAFXDPVIPKeys converts VIP assignments into afxdp.VIPKey entries
// so the AF_XDP zero-copy fast path knows which packets to intercept.
func buildAFXDPVIPKeys(vips []*pb.VIPAssignment) []afxdp.VIPKey {
	var keys []afxdp.VIPKey
	for _, v := range vips {
		addr := v.GetAddress()
		if idx := strings.IndexByte(addr, '/'); idx >= 0 {
			addr = addr[:idx]
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue // AF_XDP VIPKey only supports IPv4
		}

		for _, port := range v.GetPorts() {
			keys = append(keys, afxdp.VIPKey{
				Addr:  [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]},
				Port:  uint16(port), //nolint:gosec // port range
				Proto: 6,            // TCP
			})
		}
	}
	return keys
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
		rate := uint64(rlCfg.GetRequestsPerSecond())
		burst := uint64(rlCfg.GetBurst())
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
func startEBPFHealthPoller(mon *ebpfhealth.HealthMonitor, logger *zap.Logger) {
	if mon == nil {
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
