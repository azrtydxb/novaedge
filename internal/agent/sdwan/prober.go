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
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// defaultProbeInterval is the default interval between consecutive probes.
	defaultProbeInterval = 2 * time.Second

	// defaultProbeTimeout is the default timeout for a single probe.
	defaultProbeTimeout = 1 * time.Second

	// defaultEWMAAlpha is the default EWMA smoothing factor.
	// A value of 0.3 gives reasonable weight to recent measurements
	// while smoothing out transient spikes.
	defaultEWMAAlpha = 0.3

	// defaultJitterWindowSize is the default number of samples used
	// to compute jitter as the standard deviation of latency.
	defaultJitterWindowSize = 20

	// probePacketCount is the number of probe packets sent per cycle
	// to compute packet loss.
	probePacketCount = 5
)

// WANLinkSLA defines SLA thresholds for a WAN link.
// This is a local struct to avoid an import cycle with the CRD package.
type WANLinkSLA struct {
	MaxLatencyMs  float64
	MaxJitterMs   float64
	MaxPacketLoss float64 // 0.0-1.0
}

// ewma implements an exponentially weighted moving average.
type ewma struct {
	alpha       float64
	value       float64
	initialized bool
}

// newEWMA creates a new EWMA tracker with the given smoothing factor.
// Alpha must be between 0 and 1 inclusive.
func newEWMA(alpha float64) *ewma {
	if alpha <= 0 || alpha > 1 {
		alpha = defaultEWMAAlpha
	}
	return &ewma{alpha: alpha}
}

// Add records a new sample into the EWMA.
func (e *ewma) Add(value float64) {
	if !e.initialized {
		e.value = value
		e.initialized = true
		return
	}
	e.value = e.alpha*value + (1-e.alpha)*e.value
}

// Value returns the current smoothed value.
func (e *ewma) Value() float64 {
	return e.value
}

// jitterTracker computes jitter as the standard deviation of recent latency samples
// using a circular buffer.
type jitterTracker struct {
	samples []float64
	pos     int
	full    bool
	size    int
}

// newJitterTracker creates a new jitter tracker with the given window size.
func newJitterTracker(size int) *jitterTracker {
	if size <= 0 {
		size = defaultJitterWindowSize
	}
	return &jitterTracker{
		samples: make([]float64, size),
		size:    size,
	}
}

// Add records a new latency sample.
func (j *jitterTracker) Add(sample float64) {
	j.samples[j.pos] = sample
	j.pos++
	if j.pos >= j.size {
		j.pos = 0
		j.full = true
	}
}

// Jitter returns the standard deviation of recorded samples.
// Returns 0 if fewer than 2 samples are available.
func (j *jitterTracker) Jitter() float64 {
	n := j.count()
	if n < 2 {
		return 0
	}

	// Compute mean
	var sum float64
	for i := 0; i < n; i++ {
		sum += j.samples[i]
	}
	mean := sum / float64(n)

	// Compute variance
	var variance float64
	for i := 0; i < n; i++ {
		diff := j.samples[i] - mean
		variance += diff * diff
	}
	variance /= float64(n)

	return math.Sqrt(variance)
}

func (j *jitterTracker) count() int {
	if j.full {
		return j.size
	}
	return j.pos
}

// calculateScore computes a composite quality score for a WAN link.
// The score is in the range [0, 1] where higher is better.
//
// Formula: score = (1 - packetLoss) * (1 / (latencyMs * (1 + jitterMs/latencyMs)))
// Simplifies to: score = (1 - packetLoss) / (latencyMs + jitterMs)
//
// Special cases:
//   - If latencyMs == 0, score = 1.0 (perfect link)
//   - If packetLoss >= 1.0, score = 0.0 (completely lossy link)
func calculateScore(latencyMs, jitterMs, packetLoss float64) float64 {
	if packetLoss >= 1.0 {
		return 0.0
	}
	if latencyMs <= 0 {
		return 1.0
	}
	denominator := latencyMs + jitterMs
	if denominator <= 0 {
		return 1.0
	}
	score := (1 - packetLoss) / denominator
	// Clamp to [0, 1]
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}

// LinkQuality holds the measured quality metrics for a single WAN link.
type LinkQuality struct {
	LinkName    string
	RemoteSite  string
	LatencyMs   float64
	JitterMs    float64
	PacketLoss  float64 // 0.0-1.0
	Score       float64
	LastUpdated time.Time
	Healthy     bool
}

// probeTarget represents a single probe destination.
type probeTarget struct {
	linkName   string
	remoteSite string
	addr       string
	sla        *WANLinkSLA
}

// Prober performs periodic quality measurements for WAN links using
// TCP connect probes. It tracks latency via EWMA smoothing, jitter
// via standard deviation, and packet loss via probe success ratio.
type Prober struct {
	mu        sync.RWMutex
	targets   map[string]*probeTarget
	qualities map[string]*LinkQuality
	latencies map[string]*ewma
	jitters   map[string]*jitterTracker
	logger    *zap.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
}

// NewProber creates a new WAN link quality prober.
func NewProber(logger *zap.Logger) *Prober {
	return &Prober{
		targets:   make(map[string]*probeTarget),
		qualities: make(map[string]*LinkQuality),
		latencies: make(map[string]*ewma),
		jitters:   make(map[string]*jitterTracker),
		logger:    logger.Named("sdwan-prober"),
	}
}

// AddTarget registers a new probe target for continuous monitoring.
func (p *Prober) AddTarget(linkName, remoteSite, addr string, sla *WANLinkSLA) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.targets[linkName] = &probeTarget{
		linkName:   linkName,
		remoteSite: remoteSite,
		addr:       addr,
		sla:        sla,
	}
	p.qualities[linkName] = &LinkQuality{
		LinkName:   linkName,
		RemoteSite: remoteSite,
		Healthy:    true,
	}
	p.latencies[linkName] = newEWMA(defaultEWMAAlpha)
	p.jitters[linkName] = newJitterTracker(defaultJitterWindowSize)

	p.logger.Info("Added probe target",
		zap.String("link", linkName),
		zap.String("remote_site", remoteSite),
		zap.String("addr", addr),
	)

	// If the prober is already running, start a probe loop for this target
	if p.started {
		p.wg.Add(1)
		go p.probeLoop(p.targets[linkName])
	}
}

// RemoveTarget unregisters a probe target.
func (p *Prober) RemoveTarget(linkName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.targets, linkName)
	delete(p.qualities, linkName)
	delete(p.latencies, linkName)
	delete(p.jitters, linkName)

	p.logger.Info("Removed probe target", zap.String("link", linkName))
}

// Start begins the probe loops for all registered targets.
// Targets added after Start is called will also have probe loops started automatically.
func (p *Prober) Start(ctx context.Context) {
	p.mu.Lock()
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	// Start a probe loop for each existing target
	for _, target := range p.targets {
		p.wg.Add(1)
		go p.probeLoop(target)
	}
	p.mu.Unlock()

	p.logger.Info("Prober started")
}

// Stop cancels all running probe loops and waits for them to finish.
func (p *Prober) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	p.logger.Info("Prober stopped")
}

// GetQuality returns the current quality metrics for a link.
// Returns nil if the link is not being probed.
func (p *Prober) GetQuality(linkName string) *LinkQuality {
	p.mu.RLock()
	defer p.mu.RUnlock()

	q, exists := p.qualities[linkName]
	if !exists {
		return nil
	}
	// Return a copy to avoid races
	copy := *q
	return &copy
}

// GetAllQualities returns a snapshot of quality metrics for all probed links.
func (p *Prober) GetAllQualities() map[string]*LinkQuality {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*LinkQuality, len(p.qualities))
	for k, v := range p.qualities {
		copy := *v
		result[k] = &copy
	}
	return result
}

// probeLoop runs the continuous probe cycle for a single target.
func (p *Prober) probeLoop(target *probeTarget) {
	defer p.wg.Done()

	ticker := time.NewTicker(defaultProbeInterval)
	defer ticker.Stop()

	p.logger.Debug("Starting probe loop",
		zap.String("link", target.linkName),
		zap.String("addr", target.addr),
	)

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.performProbe(target)
		}
	}
}

// performProbe executes a probe cycle: sends probePacketCount TCP connect probes,
// measures latency and packet loss, updates EWMA and jitter trackers,
// and evaluates SLA compliance.
func (p *Prober) performProbe(target *probeTarget) {
	var totalLatency float64
	var successCount int

	for i := 0; i < probePacketCount; i++ {
		latency, err := p.tcpProbe(target.addr)
		if err != nil {
			p.logger.Debug("Probe failed",
				zap.String("link", target.linkName),
				zap.String("addr", target.addr),
				zap.Error(err),
			)
			continue
		}
		totalLatency += latency
		successCount++
	}

	packetLoss := 1.0 - float64(successCount)/float64(probePacketCount)

	p.mu.Lock()
	defer p.mu.Unlock()

	latencyEWMA, latencyExists := p.latencies[target.linkName]
	jitter, jitterExists := p.jitters[target.linkName]
	quality, qualityExists := p.qualities[target.linkName]
	if !latencyExists || !jitterExists || !qualityExists {
		return
	}

	if successCount > 0 {
		avgLatency := totalLatency / float64(successCount)
		latencyEWMA.Add(avgLatency)
		jitter.Add(avgLatency)
	}

	currentLatency := latencyEWMA.Value()
	currentJitter := jitter.Jitter()

	quality.LatencyMs = currentLatency
	quality.JitterMs = currentJitter
	quality.PacketLoss = packetLoss
	quality.Score = calculateScore(currentLatency, currentJitter, packetLoss)
	quality.LastUpdated = time.Now()

	// Evaluate SLA compliance
	quality.Healthy = p.evaluateSLA(target.sla, currentLatency, currentJitter, packetLoss)
}

// tcpProbe performs a single TCP connect probe and returns the latency in milliseconds.
func (p *Prober) tcpProbe(addr string) (float64, error) {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", addr, defaultProbeTimeout)
	if err != nil {
		return 0, fmt.Errorf("tcp probe to %s failed: %w", addr, err)
	}
	_ = conn.Close()

	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0
	return latencyMs, nil
}

// evaluateSLA checks whether current metrics satisfy the SLA thresholds.
// If no SLA is defined, the link is considered healthy.
func (p *Prober) evaluateSLA(sla *WANLinkSLA, latencyMs, jitterMs, packetLoss float64) bool {
	if sla == nil {
		return true
	}
	if sla.MaxLatencyMs > 0 && latencyMs > sla.MaxLatencyMs {
		return false
	}
	if sla.MaxJitterMs > 0 && jitterMs > sla.MaxJitterMs {
		return false
	}
	if sla.MaxPacketLoss > 0 && packetLoss > sla.MaxPacketLoss {
		return false
	}
	return true
}
