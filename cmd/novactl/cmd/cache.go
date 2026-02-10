package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
)

func newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the NovaEdge response cache",
		Long:  `Commands for managing the NovaEdge HTTP response cache, including purging and viewing statistics.`,
	}

	cmd.AddCommand(newCachePurgeCommand())
	cmd.AddCommand(newCacheStatsCommand())
	return cmd
}

func newCachePurgeCommand() *cobra.Command {
	var pattern string
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Purge cached responses",
		Long:  `Purge cached HTTP responses from the NovaEdge agent cache. Use --pattern to match specific keys.`,
		Example: `  # Purge all cached entries
  novactl cache purge --agent-addr localhost:8082

  # Purge entries matching a pattern
  novactl cache purge --pattern "/api/*" --agent-addr localhost:8082`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCachePurge(agentAddr, pattern)
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "*", "Pattern to match cache keys for purging")
	cmd.Flags().StringVar(&agentAddr, "agent-addr", "localhost:8082", "Address of the NovaEdge agent health server")
	return cmd
}

func newCacheStatsCommand() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache statistics",
		Long:  `Display hit/miss statistics and memory usage for the NovaEdge response cache.`,
		Example: `  # Show cache stats
  novactl cache stats --agent-addr localhost:8082`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCacheStats(agentAddr)
		},
	}

	cmd.Flags().StringVar(&agentAddr, "agent-addr", "localhost:8082", "Address of the NovaEdge agent health server")
	return cmd
}

func runCachePurge(agentAddr, pattern string) error {
	u := url.URL{
		Scheme: "http",
		Host:   agentAddr,
		Path:   "/_novaedge/cache",
	}
	q := u.Query()
	q.Set("pattern", pattern)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodDelete, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to agent at %s: %w", agentAddr, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cache purge failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Cache purge successful: %v entries purged (pattern: %s)\n",
		result["purged"], result["pattern"])
	return nil
}

func runCacheStats(agentAddr string) error {
	u := url.URL{
		Scheme: "http",
		Host:   agentAddr,
		Path:   "/_novaedge/cache",
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return fmt.Errorf("failed to connect to agent at %s: %w", agentAddr, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cache stats failed (status %d): %s", resp.StatusCode, string(body))
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Fprintln(os.Stdout, "NovaEdge Cache Statistics:")
	fmt.Fprintf(os.Stdout, "  Entries:        %.0f\n", stats["entries"])
	fmt.Fprintf(os.Stdout, "  Memory Used:    %.0f bytes\n", stats["memoryUsed"])
	fmt.Fprintf(os.Stdout, "  Max Memory:     %.0f bytes\n", stats["maxMemory"])
	fmt.Fprintf(os.Stdout, "  Hit Count:      %.0f\n", stats["hitCount"])
	fmt.Fprintf(os.Stdout, "  Miss Count:     %.0f\n", stats["missCount"])
	fmt.Fprintf(os.Stdout, "  Eviction Count: %.0f\n", stats["evictionCount"])

	return nil
}
