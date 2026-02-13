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

package health

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestClusterStats_NetworkErrorRatio(t *testing.T) {
	tests := []struct {
		name     string
		stats    ClusterStats
		expected float64
	}{
		{
			name:     "zero requests returns zero",
			stats:    ClusterStats{TotalRequests: 0, NetworkErrors: 0},
			expected: 0,
		},
		{
			name:     "no errors returns zero",
			stats:    ClusterStats{TotalRequests: 100, NetworkErrors: 0},
			expected: 0,
		},
		{
			name:     "half errors",
			stats:    ClusterStats{TotalRequests: 100, NetworkErrors: 50},
			expected: 0.5,
		},
		{
			name:     "all errors",
			stats:    ClusterStats{TotalRequests: 100, NetworkErrors: 100},
			expected: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stats.NetworkErrorRatio()
			if got != tt.expected {
				t.Errorf("NetworkErrorRatio() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClusterStats_ResponseCodeRatio(t *testing.T) {
	stats := ClusterStats{
		TotalRequests: 1000,
		ResponseCodes: map[int]int64{
			200: 700,
			404: 100,
			500: 150,
			502: 50,
		},
	}

	tests := []struct {
		name                                       string
		codeFrom, codeTo, dividendFrom, dividendTo int
		expected                                   float64
	}{
		{
			name:     "5xx over all",
			codeFrom: 500, codeTo: 600,
			dividendFrom: 0, dividendTo: 600,
			expected: 0.2, // 200 / 1000
		},
		{
			name:     "500 only over all",
			codeFrom: 500, codeTo: 501,
			dividendFrom: 0, dividendTo: 600,
			expected: 0.15, // 150 / 1000
		},
		{
			name:     "empty dividend range returns zero",
			codeFrom: 500, codeTo: 600,
			dividendFrom: 700, dividendTo: 800,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stats.ResponseCodeRatio(tt.codeFrom, tt.codeTo, tt.dividendFrom, tt.dividendTo)
			if got != tt.expected {
				t.Errorf("ResponseCodeRatio(%d, %d, %d, %d) = %v, want %v",
					tt.codeFrom, tt.codeTo, tt.dividendFrom, tt.dividendTo, got, tt.expected)
			}
		})
	}
}

func TestClusterStats_LatencyAtQuantileMS(t *testing.T) {
	stats := ClusterStats{
		LatencyP50: 50 * time.Millisecond,
		LatencyP99: 200 * time.Millisecond,
	}

	tests := []struct {
		name     string
		quantile float64
		expected float64
	}{
		{"p50", 0.5, 50},
		{"p99", 0.99, 200},
		{"unsupported quantile", 0.75, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stats.LatencyAtQuantileMS(tt.quantile)
			if got != tt.expected {
				t.Errorf("LatencyAtQuantileMS(%v) = %v, want %v", tt.quantile, got, tt.expected)
			}
		})
	}
}

func TestParseCBExpression_SimpleNetworkErrorRatio(t *testing.T) {
	expr, err := ParseCBExpression("NetworkErrorRatio() > 0.3")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if expr == nil {
		t.Fatal("expected non-nil expression")
	}

	// Should trigger when error ratio > 0.3
	statsHigh := &ClusterStats{TotalRequests: 100, NetworkErrors: 50}
	if !expr.Evaluate(statsHigh) {
		t.Error("expected expression to evaluate to true for 50% error ratio")
	}

	// Should not trigger when error ratio <= 0.3
	statsLow := &ClusterStats{TotalRequests: 100, NetworkErrors: 20}
	if expr.Evaluate(statsLow) {
		t.Error("expected expression to evaluate to false for 20% error ratio")
	}
}

func TestParseCBExpression_CompoundOrExpression(t *testing.T) {
	expr, err := ParseCBExpression("NetworkErrorRatio() > 0.3 || ResponseCodeRatio(500, 600, 0, 600) > 0.25")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Only network errors high
	stats1 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 50,
		ResponseCodes: map[int]int64{200: 90, 500: 10},
	}
	if !expr.Evaluate(stats1) {
		t.Error("expected true: network error ratio is high")
	}

	// Only response code ratio high
	stats2 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 5,
		ResponseCodes: map[int]int64{200: 60, 500: 40},
	}
	if !expr.Evaluate(stats2) {
		t.Error("expected true: response code ratio is high")
	}

	// Neither condition met
	stats3 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 10,
		ResponseCodes: map[int]int64{200: 90, 500: 10},
	}
	if expr.Evaluate(stats3) {
		t.Error("expected false: neither condition met")
	}
}

func TestParseCBExpression_CompoundAndExpression(t *testing.T) {
	expr, err := ParseCBExpression("NetworkErrorRatio() > 0.1 && ResponseCodeRatio(500, 600, 0, 600) > 0.2")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Both conditions met
	stats1 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 20,
		ResponseCodes: map[int]int64{200: 60, 500: 40},
	}
	if !expr.Evaluate(stats1) {
		t.Error("expected true: both conditions met")
	}

	// Only one condition met
	stats2 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 5,
		ResponseCodes: map[int]int64{200: 60, 500: 40},
	}
	if expr.Evaluate(stats2) {
		t.Error("expected false: only response code ratio condition met")
	}
}

func TestParseCBExpression_LatencyExpression(t *testing.T) {
	expr, err := ParseCBExpression("LatencyAtQuantileMS(0.99) > 200")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	statsHigh := &ClusterStats{LatencyP99: 300 * time.Millisecond}
	if !expr.Evaluate(statsHigh) {
		t.Error("expected true: p99 latency is 300ms > 200ms")
	}

	statsLow := &ClusterStats{LatencyP99: 100 * time.Millisecond}
	if expr.Evaluate(statsLow) {
		t.Error("expected false: p99 latency is 100ms <= 200ms")
	}
}

func TestParseCBExpression_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		stats    *ClusterStats
		expected bool
	}{
		{
			name:     "greater than - true",
			expr:     "NetworkErrorRatio() > 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 60},
			expected: true,
		},
		{
			name:     "greater than - false",
			expr:     "NetworkErrorRatio() > 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 40},
			expected: false,
		},
		{
			name:     "less than - true",
			expr:     "NetworkErrorRatio() < 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 40},
			expected: true,
		},
		{
			name:     "less than - false",
			expr:     "NetworkErrorRatio() < 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 60},
			expected: false,
		},
		{
			name:     "greater than or equal - true at boundary",
			expr:     "NetworkErrorRatio() >= 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 50},
			expected: true,
		},
		{
			name:     "less than or equal - true at boundary",
			expr:     "NetworkErrorRatio() <= 0.5",
			stats:    &ClusterStats{TotalRequests: 100, NetworkErrors: 50},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseCBExpression(tt.expr)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			got := parsed.Evaluate(tt.stats)
			if got != tt.expected {
				t.Errorf("Evaluate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseCBExpression_Parentheses(t *testing.T) {
	expr, err := ParseCBExpression("(NetworkErrorRatio() > 0.3 || ResponseCodeRatio(500, 600, 0, 600) > 0.5) && LatencyAtQuantileMS(0.99) > 100")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Error ratio high + latency high -> true
	stats1 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 50,
		ResponseCodes: map[int]int64{200: 90, 500: 10},
		LatencyP99:    150 * time.Millisecond,
	}
	if !expr.Evaluate(stats1) {
		t.Error("expected true: error ratio and latency both high")
	}

	// Error ratio high but latency low -> false
	stats2 := &ClusterStats{
		TotalRequests: 100,
		NetworkErrors: 50,
		ResponseCodes: map[int]int64{200: 90, 500: 10},
		LatencyP99:    50 * time.Millisecond,
	}
	if expr.Evaluate(stats2) {
		t.Error("expected false: latency is low despite high error ratio")
	}
}

func TestParseCBExpression_InvalidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty expression", ""},
		{"missing operator", "NetworkErrorRatio()"},
		{"missing threshold", "NetworkErrorRatio() >"},
		{"unknown function", "UnknownFunc() > 0.5"},
		{"wrong arg count for NetworkErrorRatio", "NetworkErrorRatio(1) > 0.5"},
		{"wrong arg count for ResponseCodeRatio", "ResponseCodeRatio(500) > 0.5"},
		{"wrong arg count for LatencyAtQuantileMS", "LatencyAtQuantileMS() > 0.5"},
		{"invalid character", "NetworkErrorRatio() > 0.5 !! true"},
		{"unclosed parenthesis", "(NetworkErrorRatio() > 0.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCBExpression(tt.expr)
			if err == nil {
				t.Errorf("expected parse error for %q, got nil", tt.expr)
			}
		})
	}
}

func TestNewExpressionCircuitBreaker(t *testing.T) {
	logger := zap.NewNop()

	ecb, err := NewExpressionCircuitBreaker("NetworkErrorRatio() > 0.5", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ecb.CheckPeriod != 100*time.Millisecond {
		t.Errorf("expected CheckPeriod=100ms, got %v", ecb.CheckPeriod)
	}
	if ecb.FallbackDuration != 10*time.Second {
		t.Errorf("expected FallbackDuration=10s, got %v", ecb.FallbackDuration)
	}
	if ecb.RecoveryDuration != 10*time.Second {
		t.Errorf("expected RecoveryDuration=10s, got %v", ecb.RecoveryDuration)
	}
	if ecb.GetState() != StateClosed {
		t.Errorf("expected initial state Closed, got %v", ecb.GetState())
	}
}

func TestNewExpressionCircuitBreaker_InvalidExpression(t *testing.T) {
	logger := zap.NewNop()

	_, err := NewExpressionCircuitBreaker("invalid expression", logger)
	if err == nil {
		t.Error("expected error for invalid expression")
	}
}

func TestExpressionCircuitBreaker_EvaluateAndUpdate(t *testing.T) {
	logger := zap.NewNop()

	ecb, err := NewExpressionCircuitBreaker("NetworkErrorRatio() > 0.5", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ecb.FallbackDuration = 50 * time.Millisecond
	ecb.RecoveryDuration = 50 * time.Millisecond

	// Initially closed
	if ecb.GetState() != StateClosed {
		t.Fatal("expected closed state")
	}

	// Trigger with high error ratio -> open
	badStats := &ClusterStats{TotalRequests: 100, NetworkErrors: 60}
	ecb.EvaluateAndUpdate(badStats)
	if ecb.GetState() != StateOpen {
		t.Errorf("expected open state, got %v", ecb.GetState())
	}
	if !ecb.IsOpen() {
		t.Error("IsOpen should return true")
	}

	// Still open before fallback duration
	ecb.EvaluateAndUpdate(badStats)
	if ecb.GetState() != StateOpen {
		t.Error("should still be open before fallback duration")
	}

	// Wait for fallback duration -> half-open
	time.Sleep(100 * time.Millisecond)
	goodStats := &ClusterStats{TotalRequests: 100, NetworkErrors: 10}
	ecb.EvaluateAndUpdate(goodStats)
	if ecb.GetState() != StateHalfOpen {
		t.Errorf("expected half-open state after fallback duration, got %v", ecb.GetState())
	}

	// Wait for recovery duration with good stats -> closed
	time.Sleep(100 * time.Millisecond)
	ecb.EvaluateAndUpdate(goodStats)
	if ecb.GetState() != StateClosed {
		t.Errorf("expected closed state after recovery, got %v", ecb.GetState())
	}
}

func TestExpressionCircuitBreaker_HalfOpenReTrip(t *testing.T) {
	logger := zap.NewNop()

	ecb, err := NewExpressionCircuitBreaker("NetworkErrorRatio() > 0.5", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ecb.FallbackDuration = 50 * time.Millisecond
	ecb.RecoveryDuration = 50 * time.Millisecond

	// Trip the circuit
	badStats := &ClusterStats{TotalRequests: 100, NetworkErrors: 60}
	ecb.EvaluateAndUpdate(badStats)
	if ecb.GetState() != StateOpen {
		t.Fatal("expected open state")
	}

	// Wait for fallback -> half-open
	time.Sleep(100 * time.Millisecond)
	goodStats := &ClusterStats{TotalRequests: 100, NetworkErrors: 10}
	ecb.EvaluateAndUpdate(goodStats)
	if ecb.GetState() != StateHalfOpen {
		t.Fatalf("expected half-open state, got %v", ecb.GetState())
	}

	// Re-trip with bad stats -> open again
	ecb.EvaluateAndUpdate(badStats)
	if ecb.GetState() != StateOpen {
		t.Errorf("expected open state after re-trip, got %v", ecb.GetState())
	}
}

func TestTokenizer(t *testing.T) {
	input := "NetworkErrorRatio() > 0.3 && ResponseCodeRatio(500, 600, 0, 600) > 0.25"
	lex := newLexer(input)
	tokens, err := lex.tokenize()
	if err != nil {
		t.Fatalf("unexpected tokenize error: %v", err)
	}

	// Verify we got reasonable number of tokens (including EOF)
	if len(tokens) < 10 {
		t.Errorf("expected at least 10 tokens, got %d", len(tokens))
	}

	// Last token should be EOF
	if tokens[len(tokens)-1].kind != tokenEOF {
		t.Error("expected last token to be EOF")
	}
}

func TestTokenizer_InvalidCharacter(t *testing.T) {
	lex := newLexer("NetworkErrorRatio() > 0.5 @ invalid")
	_, err := lex.tokenize()
	if err == nil {
		t.Error("expected error for invalid character '@'")
	}
}
