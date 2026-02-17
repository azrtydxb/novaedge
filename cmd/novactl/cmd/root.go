// Package cmd provides the command-line interface for novactl.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Build-time version info set from main via SetVersionInfo.
var (
	cliVersion = "dev"
	cliCommit  = "unknown"
	cliDate    = "unknown"
)

// SetVersionInfo sets the build-time version information from ldflags.
func SetVersionInfo(ver, com, dt string) {
	cliVersion = ver
	cliCommit = com
	cliDate = dt
}

var (
	kubeconfig string
	namespace  string
	config     *rest.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "novactl",
	Short: "NovaEdge CLI tool for managing load balancer resources",
	Long: `novactl is a command-line tool for managing NovaEdge resources,
debugging, and monitoring the distributed load balancer system.

It provides kubectl-style commands for managing ProxyGateway, ProxyRoute,
ProxyBackend, ProxyPolicy, and ProxyVIP resources, as well as Gateway API
resources (GatewayClass, Gateway, HTTPRoute), specialized commands for
debugging routing, viewing metrics, and inspecting agents.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		// Try in-cluster config first (for running inside a pod)
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to kubeconfig file
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to load kubeconfig: %w", err)
			}
		}
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	defaultKubeconfig := home + "/.kube/config"

	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig, "Path to kubeconfig file")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")

	// Add subcommands
	rootCmd.AddCommand(newGetCommand())
	rootCmd.AddCommand(newDescribeCommand())
	rootCmd.AddCommand(newDeleteCommand())
	rootCmd.AddCommand(newApplyCommand())
	rootCmd.AddCommand(newAgentsCommand())
	rootCmd.AddCommand(newDebugCommand())
	rootCmd.AddCommand(newMetricsCommand())
	rootCmd.AddCommand(newLogsCommand())
	rootCmd.AddCommand(newAgentQueryCommand())
	rootCmd.AddCommand(newTraceCommand())
	rootCmd.AddCommand(newWebCommand())
	rootCmd.AddCommand(newFederationCommand())
	rootCmd.AddCommand(newCacheCommand())
	rootCmd.AddCommand(newSDWANCommand())

	// Gateway API commands
	rootCmd.AddCommand(newGatewayAPICommand())
	rootCmd.AddCommand(newConformanceCommand())

	// Generation commands
	rootCmd.AddCommand(newGenerateCommand())

	// Version command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return nil // Skip kubeconfig loading
		},
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("novactl %s (commit: %s, built: %s)\n", cliVersion, cliCommit, cliDate)
		},
	})
}
