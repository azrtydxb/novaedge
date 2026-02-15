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
	"os"

	uberzap "go.uber.org/zap"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"k8s.io/client-go/dynamic"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/controller"
	"github.com/piwi3910/novaedge/internal/controller/certmanager"
	"github.com/piwi3910/novaedge/internal/controller/ipam"
	"github.com/piwi3910/novaedge/internal/controller/snapshot"
	vaultpkg "github.com/piwi3910/novaedge/internal/controller/vault"
	"github.com/piwi3910/novaedge/internal/pkg/grpclimits"
	"github.com/piwi3910/novaedge/internal/pkg/tlsutil"
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

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(novaedgev1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var grpcAddr string
	var grpcTLSCert string
	var grpcTLSKey string
	var grpcTLSCA string
	var controllerClass string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&grpcAddr, "grpc-bind-address", ":9090", "The address the gRPC config server binds to.")
	flag.StringVar(&grpcTLSCert, "grpc-tls-cert", "", "Path to gRPC server TLS certificate file (enables mTLS if provided)")
	flag.StringVar(&grpcTLSKey, "grpc-tls-key", "", "Path to gRPC server TLS key file")
	flag.StringVar(&grpcTLSCA, "grpc-tls-ca", "", "Path to gRPC CA certificate file for client verification")
	flag.StringVar(&controllerClass, "controller-class", "novaedge.io/proxy",
		"The loadBalancerClass this controller handles. Only gateways matching this class will be reconciled.")

	var defaultVIPRef string
	var enableServiceLB bool

	var enableCertManager string
	var enableVault string
	var vaultAddr string
	var vaultAuthMethod string
	var vaultRole string
	flag.StringVar(&defaultVIPRef, "default-vip-ref", "default-vip",
		"Default VIP reference name for Ingress resources that don't specify the novaedge.io/vip-ref annotation.")
	flag.BoolVar(&enableServiceLB, "enable-service-lb", false,
		"Enable ServiceLB controller that watches type:LoadBalancer Services and creates ProxyVIP resources with IPAM allocation.")
	flag.StringVar(&enableCertManager, "enable-cert-manager", "auto", "Enable cert-manager integration (auto|true|false)")
	flag.StringVar(&enableVault, "enable-vault", "false", "Enable HashiCorp Vault integration (auto|true|false)")
	flag.StringVar(&vaultAddr, "vault-addr", "", "HashiCorp Vault server address")
	flag.StringVar(&vaultAuthMethod, "vault-auth-method", "kubernetes", "Vault auth method (kubernetes|approle|token)")
	flag.StringVar(&vaultRole, "vault-role", "novaedge", "Vault auth role name")

	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Expose dynamic log level endpoint on default mux.
	// PUT /debug/loglevel with body like "debug" or "info" to change at runtime.
	controllerAtomicLevel := uberzap.NewAtomicLevelAt(uberzap.InfoLevel)
	http.Handle("/debug/loglevel", controllerAtomicLevel)

	setupLog.Info("Starting NovaEdge controller",
		"version", version, "commit", commit, "date", date)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "novaedge-controller-leader-election",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Create shared IPAM allocator for IP pool management
	ipamLogger, _ := uberzap.NewProduction()
	allocator := ipam.NewAllocator(ipamLogger)

	if err = (&controller.ProxyVIPReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Allocator: allocator,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyVIP")
		os.Exit(1)
	}

	// Build ProxyGateway reconciler with optional cert-manager and Vault integrations.
	// These will be populated below if the respective integrations are enabled.
	proxyGatewayReconciler := &controller.ProxyGatewayReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ControllerClass: controllerClass,
	}
	if err = proxyGatewayReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyGateway")
		os.Exit(1)
	}

	if err = (&controller.ProxyRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyRoute")
		os.Exit(1)
	}

	if err = (&controller.ProxyBackendReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyBackend")
		os.Exit(1)
	}

	if err = (&controller.ProxyPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyPolicy")
		os.Exit(1)
	}

	if err = (&controller.IngressReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		DefaultVIPRef: defaultVIPRef,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Ingress")
		os.Exit(1)
	}

	if err = (&controller.GatewayReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Gateway")
		os.Exit(1)
	}

	if err = (&controller.GRPCRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GRPCRoute")
		os.Exit(1)
	}

	if err = (&controller.HTTPRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HTTPRoute")
		os.Exit(1)
	}

	if err = (&controller.GatewayClassReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatewayClass")
		os.Exit(1)
	}

	// Register ProxyIPPool reconciler with the shared IPAM allocator
	if err = (&controller.ProxyIPPoolReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Allocator: allocator,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyIPPool")
		os.Exit(1)
	}

	// Conditionally register ServiceLB controller
	if enableServiceLB {
		if err = (&controller.ServiceReconciler{
			Client:          mgr.GetClient(),
			Scheme:          mgr.GetScheme(),
			Allocator:       allocator,
			Recorder:        mgr.GetEventRecorderFor("service-lb-controller"),
			EnableServiceLB: true,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ServiceLB")
			os.Exit(1)
		}
		setupLog.Info("ServiceLB controller enabled")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Initialize cert-manager integration
	certMgrMode := certmanager.EnableMode(enableCertManager)
	detector, detectErr := certmanager.NewDetector(mgr.GetConfig())
	if detectErr != nil {
		setupLog.Error(detectErr, "unable to create cert-manager detector")
	} else {
		certMgrEnabled, shouldEnableErr := detector.ShouldEnable(context.Background(), certMgrMode)
		switch {
		case shouldEnableErr != nil:
			setupLog.Error(shouldEnableErr, "cert-manager detection failed")
		case certMgrEnabled:
			dynClient, dynErr := dynamic.NewForConfig(mgr.GetConfig())
			if dynErr != nil {
				setupLog.Error(dynErr, "failed to create dynamic client for cert-manager")
			} else {
				proxyGatewayReconciler.CertManager = certmanager.NewCertificateManager(dynClient)
				setupLog.Info("cert-manager integration enabled and wired into ProxyGateway reconciler")
			}
		default:
			setupLog.Info("cert-manager integration disabled")
		}
	}

	// Initialize Vault integration
	vaultMode := vaultpkg.EnableMode(enableVault)
	if vaultMode != vaultpkg.EnableModeFalse {
		vaultConfig := &vaultpkg.Config{
			Address:    vaultAddr,
			AuthMethod: vaultpkg.AuthMethod(vaultAuthMethod),
		}
		if vaultAuthMethod == "kubernetes" {
			vaultConfig.KubernetesAuth = &vaultpkg.KubernetesAuthConfig{
				Role: vaultRole,
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
				proxyGatewayReconciler.VaultPKI = vaultpkg.NewPKIManager(vaultClient, zapLogger)
				setupLog.Info("Vault integration enabled and wired into ProxyGateway reconciler", "address", vaultAddr)
			}
		default:
			setupLog.Info("Vault integration disabled")
		}
	}

	// Create and start gRPC server for config distribution
	configServer := snapshot.NewServer(mgr.GetClient())

	// Create gRPC server with message size limits and interceptors
	grpcLogger, _ := uberzap.NewProduction()
	serverOpts := grpclimits.ServerOptions(grpcLogger)

	var grpcServer *grpc.Server
	if grpcTLSCert != "" && grpcTLSKey != "" && grpcTLSCA != "" {
		// Load TLS credentials for mTLS
		creds, err := tlsutil.LoadServerTLSCredentials(grpcTLSCert, grpcTLSKey, grpcTLSCA)
		if err != nil {
			setupLog.Error(err, "failed to load gRPC TLS credentials")
			os.Exit(1)
		}
		grpcServer = grpc.NewServer(append(serverOpts, grpc.Creds(creds))...)
		setupLog.Info("gRPC server configured with mTLS",
			"cert", grpcTLSCert,
			"ca", grpcTLSCA,
			"max_recv_msg_size", grpclimits.DefaultMaxRecvMsgSize,
			"max_send_msg_size", grpclimits.DefaultMaxSendMsgSize)
	} else {
		// Create insecure gRPC server (for development only)
		grpcServer = grpc.NewServer(serverOpts...)
		setupLog.Info("WARNING: gRPC server running without TLS (insecure)")
	}

	configServer.RegisterServer(grpcServer)

	// Start gRPC server in a goroutine
	go func() {
		var lc net.ListenConfig
		lis, err := lc.Listen(context.Background(), "tcp", grpcAddr)
		if err != nil {
			setupLog.Error(err, "failed to listen for gRPC")
			os.Exit(1)
		}
		setupLog.Info("starting gRPC config server", "address", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			setupLog.Error(err, "failed to serve gRPC")
			os.Exit(1)
		}
	}()

	// Pass config server to reconcilers so they can trigger updates
	controller.SetConfigServer(configServer)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	// Graceful shutdown of gRPC server
	grpcServer.GracefulStop()
}
