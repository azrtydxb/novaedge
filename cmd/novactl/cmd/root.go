// Package cmd provides the command-line interface for novactl.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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
ProxyBackend, ProxyPolicy, and ProxyVIP resources, as well as specialized
commands for debugging routing, viewing metrics, and inspecting agents.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		// Load kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig: %w", err)
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
}
