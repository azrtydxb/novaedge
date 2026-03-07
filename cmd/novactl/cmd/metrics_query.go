package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/prometheus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	prometheusEndpoint  string
	prometheusStart     string
	prometheusEnd       string
	prometheusStep      string
	metricsOutputFormat string
	topLimit            int
)

const (
	defaultPrometheusEndpoint = "http://localhost:9090"
	defaultTopLimit           = 10
)

func newMetricsQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <promql>",
		Short: "Execute a PromQL query",
		Long: `Execute a custom PromQL query against the Prometheus endpoint
and display the results.`,
		Example: `  # Query current request rate
  novactl metrics query 'rate(novaedge_agent_requests_total[5m])'

  # Query with custom endpoint
  novactl metrics query 'novaedge_agent_active_connections' --prometheus-endpoint http://prometheus:9090

  # Query with JSON output
  novactl metrics query 'novaedge_agent_request_duration_seconds' -o json

  # Range query
  novactl metrics query 'rate(novaedge_agent_requests_total[5m])' --start "2025-11-15T10:00:00Z" --end "2025-11-15T12:00:00Z"`,
		Args: cobra.ExactArgs(1),
		RunE: runMetricsQuery,
	}

	cmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", defaultPrometheusEndpoint,
		"Prometheus endpoint URL")
	cmd.Flags().StringVar(&prometheusStart, "start", "", "Start time for range query (RFC3339)")
	cmd.Flags().StringVar(&prometheusEnd, "end", "", "End time for range query (RFC3339)")
	cmd.Flags().StringVar(&prometheusStep, "step", "15s", "Query resolution step (e.g., 15s, 1m)")
	cmd.Flags().StringVarP(&metricsOutputFormat, "output", "o", "table", "Output format (table, json, yaml)")

	return cmd
}

func runMetricsQuery(_ *cobra.Command, args []string) error {
	query := args[0]
	ctx := context.Background()

	// Create Prometheus client
	client := prometheus.NewClient(prometheusEndpoint)

	var result *prometheus.QueryResult
	var err error

	// Determine if this is a range query or instant query
	if prometheusStart != "" || prometheusEnd != "" {
		// Range query
		params := prometheus.RangeQueryParams{
			Query: query,
		}

		if prometheusStart != "" {
			params.Start, err = time.Parse(time.RFC3339, prometheusStart)
			if err != nil {
				return fmt.Errorf("invalid start time: %w", err)
			}
		} else {
			params.Start = time.Now().Add(-1 * time.Hour)
		}

		if prometheusEnd != "" {
			params.End, err = time.Parse(time.RFC3339, prometheusEnd)
			if err != nil {
				return fmt.Errorf("invalid end time: %w", err)
			}
		} else {
			params.End = time.Now()
		}

		if prometheusStep != "" {
			params.Step, err = time.ParseDuration(prometheusStep)
			if err != nil {
				return fmt.Errorf("invalid step duration: %w", err)
			}
		}

		result, err = client.QueryRange(ctx, params)
	} else {
		// Instant query
		result, err = client.Query(ctx, query)
	}

	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	// Display results based on format
	switch metricsOutputFormat {
	case "json":
		return displayJSON(result)
	case "yaml":
		return displayYAML(result)
	default:
		return displayTable(result, query)
	}
}

func newMetricsTopBackendsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top-backends",
		Short: "Show top backends by request count",
		Long:  `Display the backends with the highest request counts.`,
		Example: `  # Show top 10 backends
  novactl metrics top-backends

  # Show top 20 backends
  novactl metrics top-backends --limit 20`,
		RunE: runMetricsTopBackends,
	}

	cmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", defaultPrometheusEndpoint,
		"Prometheus endpoint URL")
	cmd.Flags().IntVar(&topLimit, "limit", defaultTopLimit, "Number of top results to show")

	return cmd
}

func runMetricsTopBackends(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create Prometheus client
	client := prometheus.NewClient(prometheusEndpoint)

	// Query for backend request counts
	query := `sum by (backend, namespace) (rate(novaedge_backend_requests_total[5m]))`

	result, err := client.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if len(result.Data.Result) == 0 {
		fmt.Println("No backend metrics found.")
		fmt.Println("\nNote: This requires NovaEdge agents to export metrics to Prometheus.")
		return nil
	}

	// Sort by value
	type backendMetric struct {
		Backend   string
		Namespace string
		RPS       float64
	}

	metrics := make([]backendMetric, 0, len(result.Data.Result))

	for _, r := range result.Data.Result {
		value, err := prometheus.ValueAsFloat(r)
		if err != nil {
			continue
		}

		metrics = append(metrics, backendMetric{
			Backend:   r.Metric["backend"],
			Namespace: r.Metric["namespace"],
			RPS:       value,
		})
	}

	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].RPS > metrics[j].RPS
	})

	// Limit results
	if len(metrics) > topLimit {
		metrics = metrics[:topLimit]
	}

	fmt.Printf("Top %d Backends by Request Rate (5m avg):\n\n", len(metrics))

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "BACKEND\tNAMESPACE\tREQUESTS/SEC")

	for _, m := range metrics {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%.2f\n", m.Backend, m.Namespace, m.RPS)
	}

	_ = w.Flush()

	return nil
}

func newMetricsTopRoutesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top-routes",
		Short: "Show top routes by latency",
		Long:  `Display the routes with the highest average latency.`,
		Example: `  # Show top 10 slowest routes
  novactl metrics top-routes

  # Show top 20 slowest routes
  novactl metrics top-routes --limit 20`,
		RunE: runMetricsTopRoutes,
	}

	cmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", defaultPrometheusEndpoint,
		"Prometheus endpoint URL")
	cmd.Flags().IntVar(&topLimit, "limit", defaultTopLimit, "Number of top results to show")

	return cmd
}

func runMetricsTopRoutes(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create Prometheus client
	client := prometheus.NewClient(prometheusEndpoint)

	// Query for route latencies
	query := `avg by (route, namespace) (novaedge_route_request_duration_seconds)`

	result, err := client.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if len(result.Data.Result) == 0 {
		fmt.Println("No route metrics found.")
		fmt.Println("\nNote: This requires NovaEdge agents to export metrics to Prometheus.")
		return nil
	}

	// Sort by latency
	type routeMetric struct {
		Route     string
		Namespace string
		Latency   float64
	}

	metrics := make([]routeMetric, 0, len(result.Data.Result))

	for _, r := range result.Data.Result {
		value, err := prometheus.ValueAsFloat(r)
		if err != nil {
			continue
		}

		metrics = append(metrics, routeMetric{
			Route:     r.Metric["route"],
			Namespace: r.Metric["namespace"],
			Latency:   value,
		})
	}

	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Latency > metrics[j].Latency
	})

	// Limit results
	if len(metrics) > topLimit {
		metrics = metrics[:topLimit]
	}

	fmt.Printf("Top %d Routes by Latency:\n\n", len(metrics))

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ROUTE\tNAMESPACE\tAVG LATENCY")

	for _, m := range metrics {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
			m.Route,
			m.Namespace,
			prometheus.FormatDuration(m.Latency),
		)
	}

	_ = w.Flush()

	return nil
}

func newMetricsDashboardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Display key metrics dashboard",
		Long:  `Display a dashboard of key NovaEdge metrics including request rates, latencies, and errors.`,
		Example: `  # Show metrics dashboard
  novactl metrics dashboard`,
		RunE: runMetricsDashboard,
	}

	cmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", defaultPrometheusEndpoint,
		"Prometheus endpoint URL")

	return cmd
}

func runMetricsDashboard(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create Prometheus client
	client := prometheus.NewClient(prometheusEndpoint)

	fmt.Println("NovaEdge Metrics Dashboard")
	fmt.Println("==========================")
	fmt.Println()

	// Query 1: Overall request rate
	fmt.Println("Request Rate (5m avg):")
	rateResult, err := client.Query(ctx, `sum(rate(novaedge_agent_requests_total[5m]))`)
	if err == nil && len(rateResult.Data.Result) > 0 {
		value, _ := prometheus.ValueAsFloat(rateResult.Data.Result[0])
		fmt.Printf("  Total: %.2f req/sec\n", value)
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	// Query 2: Active connections
	fmt.Println("Active Connections:")
	connResult, err := client.Query(ctx, `sum(novaedge_agent_active_connections)`)
	if err == nil && len(connResult.Data.Result) > 0 {
		value, _ := prometheus.ValueAsFloat(connResult.Data.Result[0])
		fmt.Printf("  Total: %.0f connections\n", value)
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	// Query 3: Error rate
	fmt.Println("Error Rate (5m avg):")
	errorResult, err := client.Query(ctx, `sum(rate(novaedge_agent_requests_total{status=~"5.."}[5m]))`)
	if err == nil && len(errorResult.Data.Result) > 0 {
		value, _ := prometheus.ValueAsFloat(errorResult.Data.Result[0])
		fmt.Printf("  5xx Errors: %.2f req/sec\n", value)
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	// Query 4: Average latency
	fmt.Println("Request Latency:")
	latencyResult, err := client.Query(ctx, `avg(novaedge_agent_request_duration_seconds)`)
	if err == nil && len(latencyResult.Data.Result) > 0 {
		value, _ := prometheus.ValueAsFloat(latencyResult.Data.Result[0])
		fmt.Printf("  Average: %s\n", prometheus.FormatDuration(value))
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	// Query 5: VIP failovers
	fmt.Println("VIP Failovers (24h):")
	vipResult, err := client.Query(ctx, `sum(increase(novaedge_vip_failovers_total[24h]))`)
	if err == nil && len(vipResult.Data.Result) > 0 {
		value, _ := prometheus.ValueAsFloat(vipResult.Data.Result[0])
		fmt.Printf("  Total: %.0f failovers\n", value)
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	// Query 6: Agent health
	fmt.Println("Agent Health:")
	agentResult, err := client.Query(ctx, `count(up{job="novaedge-agent"} == 1)`)
	if err == nil && len(agentResult.Data.Result) > 0 {
		healthy, _ := prometheus.ValueAsFloat(agentResult.Data.Result[0])
		totalResult, err := client.Query(ctx, `count(up{job="novaedge-agent"})`)
		if err == nil && len(totalResult.Data.Result) > 0 {
			total, _ := prometheus.ValueAsFloat(totalResult.Data.Result[0])
			fmt.Printf("  Healthy: %.0f / %.0f agents\n", healthy, total)
		}
	} else {
		fmt.Println("  No data available")
	}
	fmt.Println()

	fmt.Println("Note: All queries are executed against Prometheus at:", prometheusEndpoint)
	fmt.Println("If no data is available, ensure NovaEdge agents are exporting metrics to Prometheus.")

	return nil
}

func displayJSON(result *prometheus.QueryResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func displayYAML(result *prometheus.QueryResult) error {
	encoder := yaml.NewEncoder(os.Stdout)
	defer func() { _ = encoder.Close() }()
	return encoder.Encode(result)
}

func displayTable(result *prometheus.QueryResult, query string) error {
	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Result Type: %s\n", result.Data.ResultType)
	fmt.Printf("Results: %d\n\n", len(result.Data.Result))

	if len(result.Data.Result) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	// Display as table
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)

	// Header: show all unique label names
	labelNames := make(map[string]bool)
	for _, r := range result.Data.Result {
		for k := range r.Metric {
			labelNames[k] = true
		}
	}

	labels := make([]string, 0, len(labelNames))
	for k := range labelNames {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	// Print header
	for _, label := range labels {
		_, _ = fmt.Fprintf(w, "%s\t", label)
	}
	_, _ = fmt.Fprintln(w, "VALUE")

	// Print rows
	for _, r := range result.Data.Result {
		for _, label := range labels {
			if val, ok := r.Metric[label]; ok {
				_, _ = fmt.Fprintf(w, "%s\t", val)
			} else {
				_, _ = fmt.Fprint(w, "-\t")
			}
		}

		// Print value
		if len(r.Value) > 0 {
			value, err := prometheus.ValueAsFloat(r)
			if err == nil {
				// Try to format based on query
				_, _ = fmt.Fprintf(w, "%s\n", prometheus.FormatValue(query, value))
			} else {
				_, _ = fmt.Fprintf(w, "%v\n", r.Value[1])
			}
		} else if len(r.Values) > 0 {
			// For range queries, show the latest value
			lastValue := r.Values[len(r.Values)-1]
			if len(lastValue) > 1 {
				_, _ = fmt.Fprintf(w, "%v (latest)\n", lastValue[1])
			}
		}
	}

	_ = w.Flush()

	return nil
}
