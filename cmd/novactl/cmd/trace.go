package cmd

import (
	"errors"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/trace"
	"github.com/spf13/cobra"
)
var (
	errInvalidTagFormat = errors.New("invalid tag format")
)


var (
	traceEndpoint string
	traceLimit    int
	traceLookback string
	traceService  string
	traceOp       string
	traceStart    string
	traceEnd      string
	traceMinDur   string
	traceMaxDur   string
	traceTags     []string
)

const (
	defaultTraceEndpoint = "http://localhost:16686"
	defaultTraceLimit    = 20
	defaultLookback      = "1h"
)

func newTraceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Query distributed traces from OpenTelemetry",
		Long: `Query and display distributed traces from the OpenTelemetry backend
(Jaeger, Tempo, etc.) to debug request flows through NovaEdge.`,
	}

	cmd.PersistentFlags().StringVar(&traceEndpoint, "trace-endpoint", defaultTraceEndpoint,
		"OpenTelemetry trace backend endpoint")

	cmd.AddCommand(newTraceListCommand())
	cmd.AddCommand(newTraceGetCommand())
	cmd.AddCommand(newTraceSearchCommand())
	cmd.AddCommand(newTraceServicesCommand())

	return cmd
}

func newTraceListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent traces",
		Long:  `Display a list of recent traces from the tracing backend.`,
		Example: `  # List last 20 traces from the past hour
  novactl trace list

  # List last 50 traces from the past 24 hours
  novactl trace list --limit 50 --lookback 24h

  # List traces from custom endpoint
  novactl trace list --trace-endpoint http://jaeger:16686`,
		RunE: runTraceList,
	}

	cmd.Flags().IntVar(&traceLimit, "limit", defaultTraceLimit, "Maximum number of traces to return")
	cmd.Flags().StringVar(&traceLookback, "lookback", defaultLookback, "How far back to search (e.g., 1h, 24h, 7d)")

	return cmd
}

func runTraceList(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Parse lookback duration
	lookback, err := time.ParseDuration(traceLookback)
	if err != nil {
		return fmt.Errorf("invalid lookback duration %q: %w", traceLookback, err)
	}

	// Create trace client
	client := trace.NewClient(traceEndpoint)

	// List traces
	traces, err := client.ListTraces(ctx, traceLimit, lookback)
	if err != nil {
		return fmt.Errorf("failed to list traces: %w", err)
	}

	if len(traces) == 0 {
		fmt.Println("No traces found.")
		return nil
	}

	fmt.Printf("Recent Traces (last %s):\n\n", traceLookback)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TRACE ID\tOPERATION\tSERVICE\tDURATION\tSPANS\tSTART TIME")

	for _, t := range traces {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			truncateID(t.TraceID),
			truncateString(t.OperationName, 30),
			truncateString(t.ServiceName, 20),
			trace.FormatDuration(t.Duration),
			len(t.Spans),
			t.StartTime.Format("15:04:05"),
		)
	}

	_ = w.Flush()

	fmt.Printf("\nUse 'novactl trace get <trace-id>' to view details\n")

	return nil
}

func newTraceGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <trace-id>",
		Short: "Get details of a specific trace",
		Long:  `Retrieve and display detailed information about a specific trace by its ID.`,
		Example: `  # Get trace details
  novactl trace get abc123def456

  # Get trace from custom endpoint
  novactl trace get abc123def456 --trace-endpoint http://jaeger:16686`,
		Args: cobra.ExactArgs(1),
		RunE: runTraceGet,
	}

	return cmd
}

func runTraceGet(_ *cobra.Command, args []string) error {
	traceID := args[0]
	ctx := context.Background()

	// Create trace client
	client := trace.NewClient(traceEndpoint)

	// Get trace
	t, err := client.GetTrace(ctx, traceID)
	if err != nil {
		return fmt.Errorf("failed to get trace: %w", err)
	}

	// Display trace details
	fmt.Printf("Trace: %s\n", t.TraceID)
	fmt.Printf("Operation: %s\n", t.OperationName)
	fmt.Printf("Service: %s\n", t.ServiceName)
	fmt.Printf("Duration: %s\n", trace.FormatDuration(t.Duration))
	fmt.Printf("Start Time: %s\n", t.StartTime.Format("2006-01-02 15:04:05.000"))
	fmt.Printf("Spans: %d\n\n", len(t.Spans))

	if len(t.Spans) > 0 {
		fmt.Println("Span Tree:")
		fmt.Println()
		printSpanTree(t.Spans, "", nil)
	}

	return nil
}

func newTraceSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search for traces matching criteria",
		Long:  `Search for traces that match specific criteria such as service name, operation, or tags.`,
		Example: `  # Search for traces from novaedge-agent service
  novactl trace search --service novaedge-agent

  # Search for specific operation
  novactl trace search --service novaedge-agent --operation "HTTP GET /api/users"

  # Search with time range
  novactl trace search --service novaedge-agent --start "2025-11-15T10:00:00Z" --end "2025-11-15T12:00:00Z"

  # Search for slow traces (duration > 1s)
  novactl trace search --min-duration 1s

  # Search with tags
  novactl trace search --tag http.method=GET --tag http.status_code=500`,
		RunE: runTraceSearch,
	}

	cmd.Flags().StringVar(&traceService, "service", "", "Service name to filter by")
	cmd.Flags().StringVar(&traceOp, "operation", "", "Operation name to filter by")
	cmd.Flags().StringVar(&traceStart, "start", "", "Start time (RFC3339 format)")
	cmd.Flags().StringVar(&traceEnd, "end", "", "End time (RFC3339 format)")
	cmd.Flags().StringVar(&traceMinDur, "min-duration", "", "Minimum duration (e.g., 100ms, 1s)")
	cmd.Flags().StringVar(&traceMaxDur, "max-duration", "", "Maximum duration (e.g., 5s)")
	cmd.Flags().StringArrayVar(&traceTags, "tag", []string{}, "Tags to filter by (format: key=value)")
	cmd.Flags().IntVar(&traceLimit, "limit", defaultTraceLimit, "Maximum number of traces to return")

	return cmd
}

func runTraceSearch(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Build search parameters
	params := trace.SearchParams{
		ServiceName:   traceService,
		OperationName: traceOp,
		Limit:         traceLimit,
		Tags:          make(map[string]string),
	}

	// Parse start time
	if traceStart != "" {
		startTime, err := time.Parse(time.RFC3339, traceStart)
		if err != nil {
			return fmt.Errorf("invalid start time %q: %w", traceStart, err)
		}
		params.StartTime = startTime
	}

	// Parse end time
	if traceEnd != "" {
		endTime, err := time.Parse(time.RFC3339, traceEnd)
		if err != nil {
			return fmt.Errorf("invalid end time %q: %w", traceEnd, err)
		}
		params.EndTime = endTime
	}

	// Parse min duration
	if traceMinDur != "" {
		minDur, err := time.ParseDuration(traceMinDur)
		if err != nil {
			return fmt.Errorf("invalid min duration %q: %w", traceMinDur, err)
		}
		params.MinDuration = minDur
	}

	// Parse max duration
	if traceMaxDur != "" {
		maxDur, err := time.ParseDuration(traceMaxDur)
		if err != nil {
			return fmt.Errorf("invalid max duration %q: %w", traceMaxDur, err)
		}
		params.MaxDuration = maxDur
	}

	// Parse tags
	for _, tag := range traceTags {
		parts := strings.SplitN(tag, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("%w: %q, expected key=value", errInvalidTagFormat, tag)
		}
		params.Tags[parts[0]] = parts[1]
	}

	// Create trace client
	client := trace.NewClient(traceEndpoint)

	// Search traces
	traces, err := client.SearchTraces(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to search traces: %w", err)
	}

	if len(traces) == 0 {
		fmt.Println("No traces found matching the criteria.")
		return nil
	}

	fmt.Printf("Found %d trace(s):\n\n", len(traces))

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TRACE ID\tOPERATION\tSERVICE\tDURATION\tSPANS\tSTART TIME")

	for _, t := range traces {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			truncateID(t.TraceID),
			truncateString(t.OperationName, 30),
			truncateString(t.ServiceName, 20),
			trace.FormatDuration(t.Duration),
			len(t.Spans),
			t.StartTime.Format("15:04:05"),
		)
	}

	_ = w.Flush()

	fmt.Printf("\nUse 'novactl trace get <trace-id>' to view details\n")

	return nil
}

func newTraceServicesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "services",
		Short: "List services that have sent traces",
		Long:  `Display a list of all services that have sent traces to the backend.`,
		Example: `  # List all services
  novactl trace services`,
		RunE: runTraceServices,
	}

	return cmd
}

func runTraceServices(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create trace client
	client := trace.NewClient(traceEndpoint)

	// Get services
	services, err := client.GetServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to get services: %w", err)
	}

	if len(services) == 0 {
		fmt.Println("No services found.")
		return nil
	}

	fmt.Printf("Services (%d):\n\n", len(services))

	for _, service := range services {
		fmt.Printf("  %s\n", service)
	}

	return nil
}

// printSpanTree recursively prints the span tree
func printSpanTree(spans []trace.Span, indent string, parentID *string) {
	for _, span := range spans {
		// Check if this span's parent matches
		if parentID == nil && span.ParentSpanID == "" {
			// Root span
			fmt.Printf("%s├─ %s [%s] %s\n",
				indent,
				span.OperationName,
				span.ServiceName,
				trace.FormatDuration(span.Duration),
			)
			// Print tags if any
			if len(span.Tags) > 0 {
				for k, v := range span.Tags {
					fmt.Printf("%s│  %s: %s\n", indent, k, v)
				}
			}
			// Recurse for children
			printSpanTree(spans, indent+"│  ", &span.SpanID)
		} else if parentID != nil && span.ParentSpanID == *parentID {
			// Child span
			fmt.Printf("%s├─ %s [%s] %s\n",
				indent,
				span.OperationName,
				span.ServiceName,
				trace.FormatDuration(span.Duration),
			)
			// Print important tags
			if len(span.Tags) > 0 {
				for k, v := range span.Tags {
					if strings.HasPrefix(k, "http.") || strings.HasPrefix(k, "error") {
						fmt.Printf("%s│  %s: %s\n", indent, k, v)
					}
				}
			}
			// Recurse for children
			printSpanTree(spans, indent+"│  ", &span.SpanID)
		}
	}
}

// truncateID truncates a trace/span ID for display
func truncateID(id string) string {
	if len(id) > 16 {
		return id[:16]
	}
	return id
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
