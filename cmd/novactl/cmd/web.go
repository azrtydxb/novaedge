package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	webAddress            string
	webPrometheusEndpoint string
	webOpenBrowser        bool
)

func newWebCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the NovaEdge web dashboard",
		Long: `Start a web-based dashboard for monitoring and managing NovaEdge resources.

The dashboard provides:
  - Real-time metrics visualization (requires Prometheus)
  - Gateway, Route, Backend, VIP, and Policy resource management
  - Agent status monitoring
  - Configuration viewer

The dashboard uses your current kubeconfig context to connect to the cluster.`,
		Example: `  # Start the web dashboard on default port
  novactl web

  # Start on a custom port
  novactl web --address :8080

  # Start with Prometheus metrics
  novactl web --prometheus-endpoint http://prometheus:9090

  # Start and open browser automatically
  novactl web --open`,
		RunE: runWeb,
	}

	cmd.Flags().StringVar(&webAddress, "address", ":9080",
		"Address to listen on (e.g., :9080 or 127.0.0.1:9080)")
	cmd.Flags().StringVar(&webPrometheusEndpoint, "prometheus-endpoint", "",
		"Prometheus endpoint URL for metrics (optional)")
	cmd.Flags().BoolVar(&webOpenBrowser, "open", false,
		"Open the dashboard in the default browser")

	return cmd
}

func runWeb(cmd *cobra.Command, args []string) error {
	// Get kubeconfig
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.RecommendedHomeFile
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create server config
	serverConfig := webui.Config{
		Address:            webAddress,
		KubeConfig:         config,
		PrometheusEndpoint: webPrometheusEndpoint,
	}

	// Create and start server
	server, err := webui.NewServer(serverConfig)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down web server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()
		if err := server.Stop(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error during shutdown: %v\n", err)
		}
	}()

	// Open browser if requested
	if webOpenBrowser {
		url := fmt.Sprintf("http://localhost%s", webAddress)
		if webAddress[0] != ':' {
			url = fmt.Sprintf("http://%s", webAddress)
		}
		fmt.Printf("Opening browser at %s\n", url)
		openBrowser(url)
	}

	// Start server
	if err := server.Start(); err != nil {
		return fmt.Errorf("web server error: %w", err)
	}

	return nil
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		fmt.Printf("Please open %s in your browser\n", url)
		return
	}

	// Execute in background - don't wait for result
	go func() {
		_ = exec.Command(cmd, args...).Start()
	}()
}
