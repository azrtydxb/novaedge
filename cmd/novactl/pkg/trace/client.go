// Package trace provides OpenTelemetry trace query functionality.
package trace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	errTraceQueryFailedWithStatus      = errors.New("trace query failed with status")
	errTrace                           = errors.New("trace")
	errTraceSearchFailedWithStatus     = errors.New("trace search failed with status")
	errServicesQueryFailedWithStatus   = errors.New("services query failed with status")
	errOperationsQueryFailedWithStatus = errors.New("operations query failed with status")
)

// Client provides methods for querying distributed traces.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a new trace query client.
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Trace represents a distributed trace
type Trace struct {
	TraceID       string    `json:"traceId"`
	SpanID        string    `json:"spanId"`
	OperationName string    `json:"operationName"`
	StartTime     time.Time `json:"startTime"`
	Duration      int64     `json:"duration"` // microseconds
	ServiceName   string    `json:"serviceName"`
	Spans         []Span    `json:"spans"`
}

// Span represents a single span in a trace
type Span struct {
	SpanID        string            `json:"spanId"`
	ParentSpanID  string            `json:"parentSpanId"`
	OperationName string            `json:"operationName"`
	ServiceName   string            `json:"serviceName"`
	StartTime     time.Time         `json:"startTime"`
	Duration      int64             `json:"duration"` // microseconds
	Tags          map[string]string `json:"tags"`
	Logs          []Log             `json:"logs"`
}

// Log represents a span log entry
type Log struct {
	Timestamp time.Time         `json:"timestamp"`
	Fields    map[string]string `json:"fields"`
}

// SearchParams defines parameters for searching traces.
type SearchParams struct {
	ServiceName   string
	OperationName string
	StartTime     time.Time
	EndTime       time.Time
	MinDuration   time.Duration
	MaxDuration   time.Duration
	Limit         int
	Tags          map[string]string
}

// ListTraces retrieves recent traces
func (c *Client) ListTraces(ctx context.Context, limit int, lookback time.Duration) ([]Trace, error) {
	// This is a simplified implementation
	// Actual implementation depends on the tracing backend (Jaeger, Tempo, etc.)

	// For Jaeger API:
	// GET /api/traces?service={service}&start={start}&end={end}&limit={limit}

	endpoint := fmt.Sprintf("%s/api/traces", c.endpoint)

	params := url.Values{}
	params.Set("service", "novaedge-agent")
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", time.Now().Add(-lookback).UnixMicro()))
	params.Set("end", fmt.Sprintf("%d", time.Now().UnixMicro()))

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	if _, parseErr := url.ParseRequestURI(fullURL); parseErr != nil {
		return nil, fmt.Errorf("invalid trace endpoint URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to query traces: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", errTraceQueryFailedWithStatus, resp.StatusCode, string(body))
	}

	var result struct {
		Data []Trace `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetTrace retrieves a specific trace by ID
func (c *Client) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	// For Jaeger API:
	// GET /api/traces/{traceID}

	endpoint := fmt.Sprintf("%s/api/traces/%s", c.endpoint, url.PathEscape(traceID))
	if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
		return nil, fmt.Errorf("invalid trace endpoint URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get trace: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s not found", errTrace, traceID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", errTraceQueryFailedWithStatus, resp.StatusCode, string(body))
	}

	var result struct {
		Data []Trace `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("%w: %s not found", errTrace, traceID)
	}

	return &result.Data[0], nil
}

// SearchTraces searches for traces matching the given parameters
func (c *Client) SearchTraces(ctx context.Context, params SearchParams) ([]Trace, error) {
	// For Jaeger API:
	// GET /api/traces?service={service}&operation={operation}&tags={tags}&start={start}&end={end}&limit={limit}

	endpoint := fmt.Sprintf("%s/api/traces", c.endpoint)

	queryParams := url.Values{}

	if params.ServiceName != "" {
		queryParams.Set("service", params.ServiceName)
	}

	if params.OperationName != "" {
		queryParams.Set("operation", params.OperationName)
	}

	if !params.StartTime.IsZero() {
		queryParams.Set("start", fmt.Sprintf("%d", params.StartTime.UnixMicro()))
	}

	if !params.EndTime.IsZero() {
		queryParams.Set("end", fmt.Sprintf("%d", params.EndTime.UnixMicro()))
	} else {
		queryParams.Set("end", fmt.Sprintf("%d", time.Now().UnixMicro()))
	}

	if params.MinDuration > 0 {
		queryParams.Set("minDuration", fmt.Sprintf("%dus", params.MinDuration.Microseconds()))
	}

	if params.MaxDuration > 0 {
		queryParams.Set("maxDuration", fmt.Sprintf("%dus", params.MaxDuration.Microseconds()))
	}

	if params.Limit > 0 {
		queryParams.Set("limit", fmt.Sprintf("%d", params.Limit))
	} else {
		queryParams.Set("limit", "20")
	}

	// Add tags as JSON
	if len(params.Tags) > 0 {
		tagsJSON, _ := json.Marshal(params.Tags)
		queryParams.Set("tags", string(tagsJSON))
	}

	fullURL := fmt.Sprintf("%s?%s", endpoint, queryParams.Encode())
	if _, parseErr := url.ParseRequestURI(fullURL); parseErr != nil {
		return nil, fmt.Errorf("invalid trace endpoint URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to search traces: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", errTraceSearchFailedWithStatus, resp.StatusCode, string(body))
	}

	var result struct {
		Data []Trace `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetServices retrieves the list of services that have sent traces
func (c *Client) GetServices(ctx context.Context) ([]string, error) {
	// For Jaeger API:
	// GET /api/services

	endpoint := fmt.Sprintf("%s/api/services", c.endpoint)
	if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
		return nil, fmt.Errorf("invalid trace endpoint URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", errServicesQueryFailedWithStatus, resp.StatusCode, string(body))
	}

	var result struct {
		Data []string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetOperations retrieves the list of operations for a service
func (c *Client) GetOperations(ctx context.Context, serviceName string) ([]string, error) {
	// For Jaeger API:
	// GET /api/services/{service}/operations

	endpoint := fmt.Sprintf("%s/api/services/%s/operations", c.endpoint, url.PathEscape(serviceName))
	if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
		return nil, fmt.Errorf("invalid trace endpoint URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get operations: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", errOperationsQueryFailedWithStatus, resp.StatusCode, string(body))
	}

	var result struct {
		Data []string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// FormatDuration formats a duration in microseconds to a human-readable string
func FormatDuration(microseconds int64) string {
	duration := time.Duration(microseconds) * time.Microsecond

	if duration < time.Millisecond {
		return fmt.Sprintf("%dµs", microseconds)
	}

	if duration < time.Second {
		return fmt.Sprintf("%.2fms", float64(microseconds)/1000.0)
	}

	return fmt.Sprintf("%.2fs", float64(microseconds)/1000000.0)
}
