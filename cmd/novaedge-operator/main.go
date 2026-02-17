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

// Package main implements the novaedge-operator binary, which manages the
// NovaEdge lifecycle via the NovaEdgeCluster CRD.
package main

import (
	"flag"
	"os"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/operator/controller"
	"github.com/piwi3910/novaedge/internal/operator/webhook"
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
}

func main() {
	// Core flags
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	// Leader election tuning flags
	var leaseDuration time.Duration
	var renewDeadline time.Duration
	var retryPeriod time.Duration

	flag.DurationVar(&leaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"The duration that non-leader candidates will wait to force acquire leadership.")
	flag.DurationVar(&renewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"The duration that the acting controlplane will retry refreshing leadership before giving up.")
	flag.DurationVar(&retryPeriod, "leader-elect-retry-period", 2*time.Second,
		"The duration the LeaderElector clients should wait between tries of actions.")

	// Logging flags
	var logLevel string
	var logFormat string

	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warn, error.")
	flag.StringVar(&logFormat, "log-format", "json", "Log format: json, text.")

	// Webhook flags
	var webhookPort int

	flag.IntVar(&webhookPort, "webhook-port", 0, "Port for the webhook server. 0 disables webhooks.")

	// Managed image override flags
	var controllerImage string
	var agentImage string
	var novactlImage string

	flag.StringVar(&controllerImage, "controller-image", "", "Override image for managed controller deployments.")
	flag.StringVar(&agentImage, "agent-image", "", "Override image for managed agent daemonsets.")
	flag.StringVar(&novactlImage, "novactl-image", "", "Override image for managed novactl/webui deployments.")

	opts := ctrlzap.Options{
		Development: logFormat == "text",
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Configure zap logger based on log-level and log-format flags
	var zapLevel zap.AtomicLevel
	switch logLevel {
	case "debug":
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		zapLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	ctrl.SetLogger(ctrlzap.New(
		ctrlzap.UseFlagOptions(&opts),
		ctrlzap.Level(&zapLevel),
	))

	setupLog.Info("Starting NovaEdge operator",
		"version", version, "commit", commit, "date", date)

	// Build manager options
	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "novaedge-operator-leader-election",
		LeaseDuration:          &leaseDuration,
		RenewDeadline:          &renewDeadline,
		RetryPeriod:            &retryPeriod,
	}

	// Configure webhook server if enabled
	if webhookPort > 0 {
		mgrOpts.WebhookServer = ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: webhookPort,
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Set up NovaEdgeCluster controller
	clusterReconciler := &controller.NovaEdgeClusterReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ControllerImage: controllerImage,
		AgentImage:      agentImage,
		NovactlImage:    novactlImage,
	}
	if err = clusterReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NovaEdgeCluster")
		os.Exit(1)
	}

	// Create zap logger for components that require go.uber.org/zap
	zapLogger, zapErr := zap.NewProduction()
	if logFormat == "text" {
		zapLogger, zapErr = zap.NewDevelopment()
	}
	if zapErr != nil {
		setupLog.Error(zapErr, "unable to create zap logger")
		os.Exit(1)
	}

	// Create remote cluster registry for tracking connected spoke clusters
	remoteClusterRegistry := controller.NewRemoteClusterRegistry()

	tunnelManager := controller.NewInMemoryTunnelManager(zapLogger.Named("tunnel"))

	if err = (&controller.NovaEdgeRemoteClusterReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Registry:      remoteClusterRegistry,
		TunnelManager: tunnelManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NovaEdgeRemoteCluster")
		os.Exit(1)
	}

	// Set up NovaEdgeFederation controller
	if err = (&controller.NovaEdgeFederationReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Logger:         zapLogger.Named("federation"),
		ControllerName: "novaedge-operator",
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NovaEdgeFederation")
		os.Exit(1)
	}

	// Set up webhooks if enabled
	if webhookPort > 0 {
		if err = (&webhook.FederationValidator{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "FederationValidator")
			os.Exit(1)
		}
		if err = (&webhook.FederationDefaulter{}).SetupDefaulterWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "FederationDefaulter")
			os.Exit(1)
		}
		setupLog.Info("Webhook server enabled", "port", webhookPort)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
