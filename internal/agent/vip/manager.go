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

package vip

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Manager manages VIP ownership and announces
type Manager interface {
	// ApplyVIPs applies VIP assignments from config snapshot
	ApplyVIPs(assignments []*pb.VIPAssignment) error

	// Release releases all VIPs
	Release() error

	// GetActiveVIPs returns currently active VIPs
	GetActiveVIPs() []string

	// Start starts the VIP manager
	Start(ctx context.Context) error
}

// VIPManager manages VIP lifecycle
type VIPManager struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Current VIP assignments
	assignments map[string]*pb.VIPAssignment

	// Mode-specific handlers
	l2Handler   *L2Handler
	bgpHandler  *BGPHandler
	ospfHandler *OSPFHandler
}

// NewManager creates a new VIP manager
func NewManager(logger *zap.Logger) (*VIPManager, error) {
	l2Handler, err := NewL2Handler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create L2 handler: %w", err)
	}

	bgpHandler, err := NewBGPHandler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create BGP handler: %w", err)
	}

	return &VIPManager{
		logger:      logger,
		assignments: make(map[string]*pb.VIPAssignment),
		l2Handler:   l2Handler,
		bgpHandler:  bgpHandler,
		// OSPF handler will be implemented in Phase 6
	}, nil
}

// Start starts the VIP manager
func (m *VIPManager) Start(ctx context.Context) error {
	m.logger.Info("Starting VIP manager")

	// Start L2 handler
	if err := m.l2Handler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start L2 handler: %w", err)
	}

	// Start BGP handler
	if err := m.bgpHandler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start BGP handler: %w", err)
	}

	return nil
}

// ApplyVIPs applies new VIP assignments
func (m *VIPManager) ApplyVIPs(assignments []*pb.VIPAssignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Applying VIP assignments", zap.Int("count", len(assignments)))

	// Build map of new assignments
	newAssignments := make(map[string]*pb.VIPAssignment)
	for _, assignment := range assignments {
		newAssignments[assignment.VipName] = assignment
	}

	// Release VIPs that are no longer assigned
	for vipName, oldAssignment := range m.assignments {
		if _, exists := newAssignments[vipName]; !exists {
			m.logger.Info("Releasing VIP", zap.String("vip", vipName))
			if err := m.releaseVIP(oldAssignment); err != nil {
				m.logger.Error("Failed to release VIP",
					zap.String("vip", vipName),
					zap.Error(err),
				)
			}
		}
	}

	// Apply new VIP assignments
	for vipName, assignment := range newAssignments {
		oldAssignment, exists := m.assignments[vipName]

		// Check if assignment changed
		if exists && assignmentsEqual(oldAssignment, assignment) {
			continue
		}

		m.logger.Info("Applying VIP assignment",
			zap.String("vip", vipName),
			zap.String("address", assignment.Address),
			zap.String("mode", assignment.Mode.String()),
			zap.Bool("is_active", assignment.IsActive),
		)

		if err := m.applyVIP(assignment); err != nil {
			m.logger.Error("Failed to apply VIP",
				zap.String("vip", vipName),
				zap.Error(err),
			)
			continue
		}
	}

	m.assignments = newAssignments
	return nil
}

// applyVIP applies a single VIP assignment
func (m *VIPManager) applyVIP(assignment *pb.VIPAssignment) error {
	if !assignment.IsActive {
		// Not active on this node, update metric and skip
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), false)
		return nil
	}

	var err error
	switch assignment.Mode {
	case pb.VIPMode_L2_ARP:
		err = m.l2Handler.AddVIP(assignment)
	case pb.VIPMode_BGP:
		err = m.bgpHandler.AddVIP(assignment)
	case pb.VIPMode_OSPF:
		// TODO: Implement OSPF mode in Phase 6
		m.logger.Warn("OSPF mode not yet implemented", zap.String("vip", assignment.VipName))
		err = nil
	default:
		err = fmt.Errorf("unsupported VIP mode: %v", assignment.Mode)
	}

	// Update VIP status metric
	if err == nil {
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), assignment.IsActive)
	}

	return err
}

// releaseVIP releases a single VIP
func (m *VIPManager) releaseVIP(assignment *pb.VIPAssignment) error {
	var err error
	switch assignment.Mode {
	case pb.VIPMode_L2_ARP:
		err = m.l2Handler.RemoveVIP(assignment)
	case pb.VIPMode_BGP:
		err = m.bgpHandler.RemoveVIP(assignment)
	case pb.VIPMode_OSPF:
		err = nil // TODO: OSPF
	default:
		err = fmt.Errorf("unsupported VIP mode: %v", assignment.Mode)
	}

	// Update VIP status metric to inactive
	if err == nil {
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), false)
	}

	return err
}

// Release releases all VIPs
func (m *VIPManager) Release() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Releasing all VIPs", zap.Int("count", len(m.assignments)))

	var errors []error
	for _, assignment := range m.assignments {
		if err := m.releaseVIP(assignment); err != nil {
			errors = append(errors, err)
		}
	}

	m.assignments = make(map[string]*pb.VIPAssignment)

	if len(errors) > 0 {
		return fmt.Errorf("failed to release some VIPs: %v", errors)
	}

	return nil
}

// GetActiveVIPs returns currently active VIPs
func (m *VIPManager) GetActiveVIPs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []string
	for vipName, assignment := range m.assignments {
		if assignment.IsActive {
			active = append(active, vipName)
		}
	}

	return active
}

// assignmentsEqual checks if two VIP assignments are equal
func assignmentsEqual(a, b *pb.VIPAssignment) bool {
	if a.VipName != b.VipName {
		return false
	}
	if a.Address != b.Address {
		return false
	}
	if a.Mode != b.Mode {
		return false
	}
	if a.IsActive != b.IsActive {
		return false
	}
	if len(a.Ports) != len(b.Ports) {
		return false
	}
	for i := range a.Ports {
		if a.Ports[i] != b.Ports[i] {
			return false
		}
	}
	return true
}

// OSPFHandler handles OSPF VIP mode (placeholder for Phase 6)
type OSPFHandler struct{}
