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

// Package main implements the novaedge-controller binary, which watches CRDs
// and Gateway API resources, builds routing configuration, and pushes
// ConfigSnapshots to node agents via gRPC.
package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // G108: pprof is served on localhost:6060 only, not publicly exposed
	"os"

	uberzap "go.uber.org/zap"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"k8s.io/client-go/dynamic"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller"
	"github.com/azrtydxb/novaedge/internal/controller/certmanager"
	"github.com/azrtydxb/novaedge/internal/controller/federation"
	"github.com/azrtydxb/novaedge/internal/controller/meshca"
	"github.com/azrtydxb/novaedge/internal/controller/snapshot"
	vaultpkg "github.com/azrtydxb/novaedge/internal/controller/vault"
	"github.com/azrtydxb/novaedge/internal/pkg/grpclimits"
	"github.com/azrtydxb/novaedge/internal/pkg/tlsutil"
	novanetv1alpha1 "github.com/azrtydxb/novanet/api/v1alpha1"
)

// Build-time variables set via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func setupScheme() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(novaedgev1alpha1.AddToScheme(scheme))
	utilruntime.Must(novanetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
}

// controllerFlags holds all parsed command-line flags for the controller.
type controllerFlags struct {
	metricsAddr           string
	probeAddr             string
	grpcAddr              string
	grpcTLSCert           string
	grpcTLSKey            string
	grpcTLSCA             string
	controllerClass       string
	defaultVIPRef         string
	enableCertManager     string
	enableVault           string
	vaultAddr             string
	vaultAuthMethod       string
	vaultRole             string
	meshTrustDomain       string
	federationID          string
	federationLocalMember string
	enableLeaderElection  bool
}

func parseControllerFlags() controllerFlags {
	var f controllerFlags
	flag.StringVar(&f.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&f.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&f.grpcAddr, "grpc-bind-address", ":9090", "The address the gRPC config server binds to.")
	flag.StringVar(&f.grpcTLSCert, "grpc-tls-cert", "", "Path to gRPC server TLS certificate file (enables mTLS if provided)")
	flag.StringVar(&f.grpcTLSKey, "grpc-tls-key", "", "Path to gRPC server TLS key file")
	flag.StringVar(&f.grpcTLSCA, "grpc-tls-ca", "", "Path to gRPC CA certificate file for client verification")
	flag.StringVar(&f.controllerClass, "controller-class", "novaedge.io/proxy",
		"The loadBalancerClass this controller handles. Only gateways matching this class will be reconciled.")
	flag.StringVar(&f.defaultVIPRef, "default-vip-ref", "default-vip",
		"Default VIP reference name for Ingress resources that don't specify the novaedge.io/vip-ref annotation.")
	flag.StringVar(&f.enableCertManager, "enable-cert-manager", "auto", "Enable cert-manager integration (auto|true|false)")
	flag.StringVar(&f.enableVault, "enable-vault", "false", "Enable HashiCorp Vault integration (auto|true|false)")
	flag.StringVar(&f.vaultAddr, "vault-addr", "", "HashiCorp Vault server address")
	flag.StringVar(&f.vaultAuthMethod, "vault-auth-method", "kubernetes", "Vault auth method (kubernetes|approle|token)")
	flag.StringVar(&f.vaultRole, "vault-role", "novaedge", "Vault auth role name")
	flag.StringVar(&f.meshTrustDomain, "mesh-trust-domain", "cluster.local", "SPIFFE trust domain for mesh mTLS identity")
	flag.StringVar(&f.federationID, "federation-id", "", "Federation identifier. When set, enables federation state on config snapshots.")
	flag.StringVar(&f.federationLocalMember, "federation-local-member", "", "Name of this controller in the federation.")
	flag.BoolVar(&f.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	return f
}

// registerReconciler is a helper that registers a single controller with the manager.
func registerReconciler(mgr ctrl.Manager, name string, setupFn func(ctrl.Manager) error) {
	if err := setupFn(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", name)
		os.Exit(1)
	}
}

// registerReconcilers registers all CRD and Gateway API reconcilers with the manager.
func registerReconcilers(mgr ctrl.Manager, f controllerFlags) *controller.ProxyGatewayReconciler {
	proxyGatewayReconciler := &controller.ProxyGatewayReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ControllerClass: f.controllerClass,
	}
	registerReconciler(mgr, "ProxyGateway", proxyGatewayReconciler.SetupWithManager)

	registerReconciler(mgr, "ProxyRoute", func(m ctrl.Manager) error {
		return (&controller.ProxyRouteReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "ProxyBackend", func(m ctrl.Manager) error {
		return (&controller.ProxyBackendReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "ProxyPolicy", func(m ctrl.Manager) error {
		return (&controller.ProxyPolicyReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "Ingress", func(m ctrl.Manager) error {
		return (&controller.IngressReconciler{
			Client: m.GetClient(), Scheme: m.GetScheme(), DefaultVIPRef: f.defaultVIPRef,
		}).SetupWithManager(m)
	})
	registerReconciler(mgr, "Gateway", func(m ctrl.Manager) error {
		return (&controller.GatewayReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "GRPCRoute", func(m ctrl.Manager) error {
		return (&controller.GRPCRouteReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "HTTPRoute", func(m ctrl.Manager) error {
		return (&controller.HTTPRouteReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "GatewayClass", func(m ctrl.Manager) error {
		return (&controller.GatewayClassReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "EndpointSlice", func(m ctrl.Manager) error {
		return (&controller.EndpointSliceReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "ProxyWANLink", func(m ctrl.Manager) error {
		return (&controller.ProxyWANLinkReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})
	registerReconciler(mgr, "ProxyWANPolicy", func(m ctrl.Manager) error {
		return (&controller.ProxyWANPolicyReconciler{Client: m.GetClient(), Scheme: m.GetScheme()}).SetupWithManager(m)
	})

	return proxyGatewayReconciler
}

// initCertManagerIntegration initializes cert-manager if enabled.
func initCertManagerIntegration(mgr ctrl.Manager, enableCertManager string, reconciler *controller.ProxyGatewayReconciler) {
	certMgrMode := certmanager.EnableMode(enableCertManager)
	detector, detectErr := certmanager.NewDetector(mgr.GetConfig())
	if detectErr != nil {
		setupLog.Error(detectErr, "unable to create cert-manager detector")
		return
	}
	certMgrEnabled, shouldEnableErr := detector.ShouldEnable(context.Background(), certMgrMode)
	switch {
	case shouldEnableErr != nil:
		setupLog.Error(shouldEnableErr, "cert-manager detection failed")
	case certMgrEnabled:
		dynClient, dynErr := dynamic.NewForConfig(mgr.GetConfig())
		if dynErr != nil {
			setupLog.Error(dynErr, "failed to create dynamic client for cert-manager")
		} else {
			reconciler.CertManager = certmanager.NewCertificateManager(dynClient)
			setupLog.Info("cert-manager integration enabled and wired into ProxyGateway reconciler")
		}
	default:
		setupLog.Info("cert-manager integration disabled")
	}
}

// initVaultIntegration initializes HashiCorp Vault if enabled.
func initVaultIntegration(f controllerFlags, reconciler *controller.ProxyGatewayReconciler) {
	vaultMode := vaultpkg.EnableMode(f.enableVault)
	if vaultMode == vaultpkg.EnableModeFalse {
		return
	}
	vaultConfig := &vaultpkg.Config{
		Address:    f.vaultAddr,
		AuthMethod: vaultpkg.AuthMethod(f.vaultAuthMethod),
	}
	if f.vaultAuthMethod == "kubernetes" {
		vaultConfig.KubernetesAuth = &vaultpkg.KubernetesAuthConfig{
			Role: f.vaultRole,
		}
	}
	zapLogger, _ := uberzap.NewProduction()
	vaultEnabled, vaultErr := vaultpkg.ShouldEnable(context.Background(), vaultConfig, vaultMode, zapLogger)
	switch {
	case vaultErr != nil:
		setupLog.Error(vaultErr, "Vault initialization failed")
		if vaultMode == vaultpkg.EnableModeTrue {
			os.Exit(1)
		}
	case vaultEnabled:
		vaultClient, clientErr := vaultpkg.NewClient(vaultConfig, zapLogger)
		if clientErr != nil {
			setupLog.Error(clientErr, "failed to create Vault client for PKI")
		} else {
			reconciler.VaultPKI = vaultpkg.NewPKIManager(vaultClient, zapLogger)
			setupLog.Info("Vault integration enabled and wired into ProxyGateway reconciler", "address", f.vaultAddr)
		}
	default:
		setupLog.Info("Vault integration disabled")
	}
}

// initConfigServer creates the config server, wires mesh CA and federation, and starts the gRPC server.
func initConfigServer(mgr ctrl.Manager, f controllerFlags) *grpc.Server {
	// Initialize mesh CA for issuing workload certificates.
	directClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "failed to create direct client for mesh CA")
		os.Exit(1)
	}
	meshCALogger, _ := uberzap.NewProduction()
	meshCA := meshca.NewMeshCA(meshCALogger, f.meshTrustDomain)
	if err := meshCA.Initialize(context.Background(), directClient); err != nil {
		setupLog.Error(err, "failed to initialize mesh CA")
		os.Exit(1)
	}
	setupLog.Info("Mesh CA initialized", "trustDomain", f.meshTrustDomain)

	configServer := snapshot.NewServer(mgr.GetClient())
	configServer.SetMeshCA(meshCA)

	// Wire federation state provider when configured.
	if f.federationID != "" && f.federationLocalMember != "" {
		fedConfig := &federation.Config{
			FederationID: f.federationID,
			LocalMember:  &federation.PeerInfo{Name: f.federationLocalMember},
		}
		fedLogger, _ := uberzap.NewProduction()
		fedManager := federation.NewManager(fedConfig, fedLogger)
		configServer.SetFederationProvider(fedManager)
		setupLog.Info("Federation state provider wired into snapshot builder",
			"federationID", f.federationID, "localMember", f.federationLocalMember)
	}

	// Create gRPC server with message size limits and interceptors
	grpcLogger, _ := uberzap.NewProduction()
	serverOpts := grpclimits.ServerOptions(grpcLogger)

	var grpcServer *grpc.Server
	if f.grpcTLSCert != "" && f.grpcTLSKey != "" && f.grpcTLSCA != "" {
		creds, tlsErr := tlsutil.LoadServerTLSCredentials(f.grpcTLSCert, f.grpcTLSKey, f.grpcTLSCA)
		if tlsErr != nil {
			setupLog.Error(tlsErr, "failed to load gRPC TLS credentials")
			os.Exit(1)
		}
		grpcServer = grpc.NewServer(append(serverOpts, grpc.Creds(creds))...)
		setupLog.Info("gRPC server configured with mTLS",
			"cert", f.grpcTLSCert, "ca", f.grpcTLSCA,
			"max_recv_msg_size", grpclimits.DefaultMaxRecvMsgSize,
			"max_send_msg_size", grpclimits.DefaultMaxSendMsgSize)
	} else {
		grpcServer = grpc.NewServer(serverOpts...)
		setupLog.Info("WARNING: gRPC server running without TLS (insecure)")
	}

	configServer.RegisterServer(grpcServer)

	// Start gRPC server in a goroutine
	go func() {
		var lc net.ListenConfig
		lis, lisErr := lc.Listen(context.Background(), "tcp", f.grpcAddr)
		if lisErr != nil {
			setupLog.Error(lisErr, "failed to listen for gRPC")
			os.Exit(1)
		}
		setupLog.Info("starting gRPC config server", "address", f.grpcAddr)
		if serveErr := grpcServer.Serve(lis); serveErr != nil {
			setupLog.Error(serveErr, "failed to serve gRPC")
			os.Exit(1)
		}
	}()

	// Pass config server to reconcilers so they can trigger updates
	controller.SetConfigServer(configServer)

	return grpcServer
}

func main() {
	setupScheme()
	f := parseControllerFlags()

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Expose dynamic log level endpoint on default mux.
	controllerAtomicLevel := uberzap.NewAtomicLevelAt(uberzap.InfoLevel)
	http.Handle("/debug/loglevel", controllerAtomicLevel)

	// Start debug server for pprof and log-level endpoints (localhost only).
	go func() {
		debugServer := &http.Server{Addr: "127.0.0.1:6060", Handler: nil} //nolint:gosec // intentionally serves DefaultServeMux on localhost
		if err := debugServer.ListenAndServe(); err != nil {
			setupLog.Error(err, "debug server failed")
		}
	}()

	setupLog.Info("Starting NovaEdge controller",
		"version", version, "commit", commit, "date", date)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: f.metricsAddr,
		},
		HealthProbeBindAddress: f.probeAddr,
		LeaderElection:         f.enableLeaderElection,
		LeaderElectionID:       "novaedge-controller-leader-election",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	proxyGatewayReconciler := registerReconcilers(mgr, f)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	initCertManagerIntegration(mgr, f.enableCertManager, proxyGatewayReconciler)
	initVaultIntegration(f, proxyGatewayReconciler)
	grpcServer := initConfigServer(mgr, f)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	// Graceful shutdown of gRPC server
	grpcServer.GracefulStop()
}
