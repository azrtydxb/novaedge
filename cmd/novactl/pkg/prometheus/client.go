// Package prometheus provides Prometheus query API client functionality.
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var (
	errQueryFailed           = errors.New("query failed")
	errInvalidValueFormat    = errors.New("invalid value format")
	errValueIsNotAString     = errors.New("value is not a string")
	errValueIsNaNOrInf       = errors.New("value is NaN or Inf")
	errTimestampIsNotANumber = errors.New("timestamp is not a number")
)

// Client provides methods for querying Prometheus
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a new Prometheus client
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// QueryResult represents a Prometheus query result
type QueryResult struct {
	Status    string    `json:"status"`
	Data      QueryData `json:"data"`
	Error     string    `json:"error,omitempty"`
	ErrorType string    `json:"errorType,omitempty"`
}

// QueryData contains the result data
type QueryData struct {
	ResultType string   `json:"resultType"`
	Result     []Result `json:"result"`
}

// Result represents a single result entry
type Result struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`  // For instant queries
	Values [][]any           `json:"values,omitempty"` // For range queries
}

// InstantQueryParams defines parameters for instant queries
type InstantQueryParams struct {
	Query string
	Time  time.Time
}

// RangeQueryParams defines parameters for range queries
type RangeQueryParams struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Query executes an instant PromQL query
func (c *Client) Query(ctx context.Context, query string) (*QueryResult, error) {
	return c.QueryAt(ctx, query, time.Now())
}

// QueryAt executes an instant PromQL query at a specific time
func (c *Client) QueryAt(ctx context.Context, query string, t time.Time) (*QueryResult, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query", c.endpoint)

	params := url.Values{}
	params.Set("query", query)
	params.Set("time", fmt.Sprintf("%d", t.Unix()))

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	if _, parseErr := url.ParseRequestURI(fullURL); parseErr != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result QueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("%w: %s (%s)", errQueryFailed, result.Error, result.ErrorType)
	}

	return &result, nil
}

// QueryRange executes a range PromQL query
func (c *Client) QueryRange(ctx context.Context, params RangeQueryParams) (*QueryResult, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query_range", c.endpoint)

	queryParams := url.Values{}
	queryParams.Set("query", params.Query)
	queryParams.Set("start", fmt.Sprintf("%d", params.Start.Unix()))
	queryParams.Set("end", fmt.Sprintf("%d", params.End.Unix()))

	if params.Step > 0 {
		queryParams.Set("step", fmt.Sprintf("%ds", int(params.Step.Seconds())))
	} else {
		// Default step: 15s
		queryParams.Set("step", "15s")
	}

	fullURL := fmt.Sprintf("%s?%s", endpoint, queryParams.Encode())
	if _, parseErr := url.ParseRequestURI(fullURL); parseErr != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result QueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("%w: %s (%s)", errQueryFailed, result.Error, result.ErrorType)
	}

	return &result, nil
}

// GetLabels retrieves all label names
func (c *Client) GetLabels(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/labels", c.endpoint)
	if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetLabelValues retrieves all values for a specific label
func (c *Client) GetLabelValues(ctx context.Context, label string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/label/%s/values", c.endpoint, url.PathEscape(label))
	if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get label values: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetSeries retrieves time series matching label selectors
func (c *Client) GetSeries(ctx context.Context, matchers []string, start, end time.Time) ([]map[string]string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/series", c.endpoint)

	params := url.Values{}
	for _, matcher := range matchers {
		params.Add("match[]", matcher)
	}
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	if _, parseErr := url.ParseRequestURI(fullURL); parseErr != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("failed to get series: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// ValueAsFloat extracts the value from a Result as a float64
func ValueAsFloat(r Result) (float64, error) {
	if len(r.Value) < 2 {
		return 0, errInvalidValueFormat
	}

	valueStr, ok := r.Value[1].(string)
	if !ok {
		return 0, errValueIsNotAString
	}

	v, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errValueIsNaNOrInf
	}
	return v, nil
}

// TimestampFromValue extracts the timestamp from a Result
func TimestampFromValue(r Result) (time.Time, error) {
	if len(r.Value) < 1 {
		return time.Time{}, errInvalidValueFormat
	}

	timestamp, ok := r.Value[0].(float64)
	if !ok {
		return time.Time{}, errTimestampIsNotANumber
	}

	return time.Unix(int64(timestamp), 0), nil
}

// MetricStats represents aggregated statistics for a metric
type MetricStats struct {
	Name   string
	Labels map[string]string
	Min    float64
	Max    float64
	Avg    float64
	Latest float64
	Count  int
}

// CalculateStats calculates statistics from range query results
func CalculateStats(results []Result) []MetricStats {
	stats := make([]MetricStats, 0, len(results))

	for _, result := range results {
		if len(result.Values) == 0 {
			continue
		}

		var sum, minVal, maxVal, latest float64
		count := 0
		first := true

		for _, value := range result.Values {
			if len(value) < 2 {
				continue
			}

			valueStr, ok := value[1].(string)
			if !ok {
				continue
			}

			v, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				continue
			}

			if first {
				minVal = v
				maxVal = v
				first = false
			} else {
				if v < minVal {
					minVal = v
				}
				if v > maxVal {
					maxVal = v
				}
			}

			sum += v
			count++
			latest = v
		}

		if count > 0 {
			stats = append(stats, MetricStats{
				Labels: result.Metric,
				Min:    minVal,
				Max:    maxVal,
				Avg:    sum / float64(count),
				Latest: latest,
				Count:  count,
			})
		}
	}

	return stats
}

// FormatValue formats a metric value based on its name
func FormatValue(metricName string, value float64) string {
	// Format based on metric suffix
	switch {
	case containsSuffix(metricName, "_bytes"):
		return FormatBytes(value)
	case containsSuffix(metricName, "_seconds"):
		return FormatDuration(value)
	case containsSuffix(metricName, "_percent"):
		return fmt.Sprintf("%.2f%%", value)
	default:
		return fmt.Sprintf("%.2f", value)
	}
}

// FormatBytes formats bytes to human-readable format
func FormatBytes(bytes float64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%.0f B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", bytes/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats seconds to human-readable duration
func FormatDuration(seconds float64) string {
	duration := time.Duration(seconds * float64(time.Second))

	if duration < time.Millisecond {
		return fmt.Sprintf("%.2fµs", seconds*1000000)
	}

	if duration < time.Second {
		return fmt.Sprintf("%.2fms", seconds*1000)
	}

	return duration.Round(time.Millisecond).String()
}

func containsSuffix(s string, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
