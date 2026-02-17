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

package sdwan

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestEWMA_Basic(t *testing.T) {
	e := newEWMA(0.5)

	// First value should be set directly
	e.Add(10.0)
	if got := e.Value(); got != 10.0 {
		t.Errorf("expected 10.0 after first Add, got %f", got)
	}

	// Second value: 0.5*20 + 0.5*10 = 15
	e.Add(20.0)
	if got := e.Value(); math.Abs(got-15.0) > 0.001 {
		t.Errorf("expected 15.0 after second Add, got %f", got)
	}

	// Third value: 0.5*10 + 0.5*15 = 12.5
	e.Add(10.0)
	if got := e.Value(); math.Abs(got-12.5) > 0.001 {
		t.Errorf("expected 12.5 after third Add, got %f", got)
	}
}

func TestEWMA_InvalidAlpha(t *testing.T) {
	// Alpha <= 0 should fall back to default
	e := newEWMA(-1.0)
	if e.alpha != defaultEWMAAlpha {
		t.Errorf("expected default alpha %f, got %f", defaultEWMAAlpha, e.alpha)
	}

	e2 := newEWMA(0)
	if e2.alpha != defaultEWMAAlpha {
		t.Errorf("expected default alpha %f for zero, got %f", defaultEWMAAlpha, e2.alpha)
	}
}

func TestEWMA_AlphaOne(t *testing.T) {
	// Alpha=1 means EWMA tracks the latest value exactly
	e := newEWMA(1.0)
	e.Add(100.0)
	e.Add(200.0)
	if got := e.Value(); got != 200.0 {
		t.Errorf("expected 200.0 with alpha=1, got %f", got)
	}
}

func TestJitterTracker_Basic(t *testing.T) {
	j := newJitterTracker(4)

	// Not enough samples
	if got := j.Jitter(); got != 0 {
		t.Errorf("expected 0 jitter with no samples, got %f", got)
	}

	j.Add(10.0)
	if got := j.Jitter(); got != 0 {
		t.Errorf("expected 0 jitter with 1 sample, got %f", got)
	}

	// Two identical samples -> 0 jitter
	j.Add(10.0)
	if got := j.Jitter(); got != 0 {
		t.Errorf("expected 0 jitter with identical samples, got %f", got)
	}

	// Add varying samples: [10, 10, 20, 20]
	j.Add(20.0)
	j.Add(20.0)
	// Mean = 15, Variance = ((10-15)^2 + (10-15)^2 + (20-15)^2 + (20-15)^2)/4 = 25
	// StdDev = 5
	if got := j.Jitter(); math.Abs(got-5.0) > 0.001 {
		t.Errorf("expected jitter ~5.0, got %f", got)
	}
}

func TestJitterTracker_Wrap(t *testing.T) {
	j := newJitterTracker(3)

	// Fill buffer
	j.Add(10.0)
	j.Add(20.0)
	j.Add(30.0)

	// Wrap: oldest (10) is replaced by 40 -> [40, 20, 30]
	j.Add(40.0)

	// Mean = 30, Variance = ((40-30)^2 + (20-30)^2 + (30-30)^2)/3 = 200/3
	expected := math.Sqrt(200.0 / 3.0)
	if got := j.Jitter(); math.Abs(got-expected) > 0.001 {
		t.Errorf("expected jitter %f, got %f", expected, got)
	}
}

func TestJitterTracker_InvalidSize(t *testing.T) {
	j := newJitterTracker(0)
	if j.size != defaultJitterWindowSize {
		t.Errorf("expected default size %d, got %d", defaultJitterWindowSize, j.size)
	}
}

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name       string
		latencyMs  float64
		jitterMs   float64
		packetLoss float64
		expected   float64
	}{
		{
			name:       "perfect link (zero latency)",
			latencyMs:  0,
			jitterMs:   0,
			packetLoss: 0,
			expected:   1.0,
		},
		{
			name:       "total packet loss",
			latencyMs:  10,
			jitterMs:   1,
			packetLoss: 1.0,
			expected:   0.0,
		},
		{
			name:       "normal link",
			latencyMs:  10,
			jitterMs:   2,
			packetLoss: 0.0,
			expected:   1.0 / 12.0,
		},
		{
			name:       "lossy link",
			latencyMs:  10,
			jitterMs:   0,
			packetLoss: 0.5,
			expected:   0.05,
		},
		{
			name:       "high latency",
			latencyMs:  100,
			jitterMs:   10,
			packetLoss: 0.01,
			expected:   0.99 / 110.0,
		},
		{
			name:       "score clamped to 1",
			latencyMs:  0.001,
			jitterMs:   0,
			packetLoss: 0,
			expected:   1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateScore(tt.latencyMs, tt.jitterMs, tt.packetLoss)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("calculateScore(%f, %f, %f) = %f, want %f",
					tt.latencyMs, tt.jitterMs, tt.packetLoss, got, tt.expected)
			}
		})
	}
}

func TestProber_AddRemoveTarget(t *testing.T) {
	logger := zap.NewNop()
	p := NewProber(logger)

	p.AddTarget("link1", "site-b", "10.0.0.1:5000", nil)

	q := p.GetQuality("link1")
	if q == nil {
		t.Fatal("expected quality for link1, got nil")
	}
	if q.LinkName != "link1" {
		t.Errorf("expected link name 'link1', got %q", q.LinkName)
	}
	if q.RemoteSite != "site-b" {
		t.Errorf("expected remote site 'site-b', got %q", q.RemoteSite)
	}
	if !q.Healthy {
		t.Error("expected new link to be healthy")
	}

	p.RemoveTarget("link1")
	if q := p.GetQuality("link1"); q != nil {
		t.Error("expected nil quality after removal")
	}
}

func TestProber_GetAllQualities(t *testing.T) {
	logger := zap.NewNop()
	p := NewProber(logger)

	p.AddTarget("link1", "site-a", "10.0.0.1:5000", nil)
	p.AddTarget("link2", "site-b", "10.0.0.2:5000", nil)

	all := p.GetAllQualities()
	if len(all) != 2 {
		t.Errorf("expected 2 qualities, got %d", len(all))
	}
	if _, ok := all["link1"]; !ok {
		t.Error("expected link1 in all qualities")
	}
	if _, ok := all["link2"]; !ok {
		t.Error("expected link2 in all qualities")
	}
}

func TestProber_Lifecycle(t *testing.T) {
	// Start a local TCP listener so probes succeed
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Accept connections in background
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	logger := zap.NewNop()
	p := NewProber(logger)

	sla := &WANLinkSLA{
		MaxLatencyMs:  500,
		MaxJitterMs:   100,
		MaxPacketLoss: 0.5,
	}

	p.AddTarget("test-link", "remote-site", ln.Addr().String(), sla)

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	// Wait for at least one probe cycle
	time.Sleep(3 * time.Second)

	q := p.GetQuality("test-link")
	if q == nil {
		t.Fatal("expected quality after probing, got nil")
	}

	if q.LatencyMs <= 0 {
		t.Errorf("expected positive latency, got %f", q.LatencyMs)
	}
	if !q.Healthy {
		t.Error("expected link to be healthy against generous SLA")
	}
	if q.Score <= 0 {
		t.Errorf("expected positive score, got %f", q.Score)
	}

	cancel()
	p.Stop()
}

func TestProber_EvaluateSLA(t *testing.T) {
	logger := zap.NewNop()
	p := NewProber(logger)

	tests := []struct {
		name     string
		sla      *WANLinkSLA
		latency  float64
		jitter   float64
		loss     float64
		expectOK bool
	}{
		{
			name:     "nil SLA always healthy",
			sla:      nil,
			latency:  1000,
			jitter:   500,
			loss:     0.9,
			expectOK: true,
		},
		{
			name:     "within SLA",
			sla:      &WANLinkSLA{MaxLatencyMs: 50, MaxJitterMs: 10, MaxPacketLoss: 0.05},
			latency:  20,
			jitter:   5,
			loss:     0.01,
			expectOK: true,
		},
		{
			name:     "latency exceeds SLA",
			sla:      &WANLinkSLA{MaxLatencyMs: 50, MaxJitterMs: 10, MaxPacketLoss: 0.05},
			latency:  100,
			jitter:   5,
			loss:     0.01,
			expectOK: false,
		},
		{
			name:     "jitter exceeds SLA",
			sla:      &WANLinkSLA{MaxLatencyMs: 50, MaxJitterMs: 10, MaxPacketLoss: 0.05},
			latency:  20,
			jitter:   15,
			loss:     0.01,
			expectOK: false,
		},
		{
			name:     "packet loss exceeds SLA",
			sla:      &WANLinkSLA{MaxLatencyMs: 50, MaxJitterMs: 10, MaxPacketLoss: 0.05},
			latency:  20,
			jitter:   5,
			loss:     0.1,
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.evaluateSLA(tt.sla, tt.latency, tt.jitter, tt.loss)
			if got != tt.expectOK {
				t.Errorf("evaluateSLA() = %v, want %v", got, tt.expectOK)
			}
		})
	}
}
