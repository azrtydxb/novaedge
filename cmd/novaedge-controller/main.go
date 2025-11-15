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
	"flag"
	"net"
	"os"

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

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/controller"
	"github.com/piwi3910/novaedge/internal/controller/snapshot"
	"github.com/piwi3910/novaedge/internal/pkg/tlsutil"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(novaedgev1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var grpcAddr string
	var grpcTLSCert string
	var grpcTLSKey string
	var grpcTLSCA string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&grpcAddr, "grpc-bind-address", ":9090", "The address the gRPC config server binds to.")
	flag.StringVar(&grpcTLSCert, "grpc-tls-cert", "", "Path to gRPC server TLS certificate file (enables mTLS if provided)")
	flag.StringVar(&grpcTLSKey, "grpc-tls-key", "", "Path to gRPC server TLS key file")
	flag.StringVar(&grpcTLSCA, "grpc-tls-ca", "", "Path to gRPC CA certificate file for client verification")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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

	if err = (&controller.ProxyVIPReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProxyVIP")
		os.Exit(1)
	}

	if err = (&controller.ProxyGatewayReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
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
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
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

	if err = (&controller.HTTPRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HTTPRoute")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Create and start gRPC server for config distribution
	configServer := snapshot.NewServer(mgr.GetClient())

	// Create gRPC server with optional mTLS
	var grpcServer *grpc.Server
	if grpcTLSCert != "" && grpcTLSKey != "" && grpcTLSCA != "" {
		// Load TLS credentials for mTLS
		creds, err := tlsutil.LoadServerTLSCredentials(grpcTLSCert, grpcTLSKey, grpcTLSCA)
		if err != nil {
			setupLog.Error(err, "failed to load gRPC TLS credentials")
			os.Exit(1)
		}
		grpcServer = grpc.NewServer(grpc.Creds(creds))
		setupLog.Info("gRPC server configured with mTLS",
			"cert", grpcTLSCert,
			"ca", grpcTLSCA)
	} else {
		// Create insecure gRPC server (for development only)
		grpcServer = grpc.NewServer()
		setupLog.Info("WARNING: gRPC server running without TLS (insecure)")
	}

	configServer.RegisterServer(grpcServer)

	// Start gRPC server in a goroutine
	go func() {
		lis, err := net.Listen("tcp", grpcAddr)
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
