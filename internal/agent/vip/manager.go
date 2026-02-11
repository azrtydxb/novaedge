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
	"net"
	"sync"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Manager manages VIP ownership and announces
type Manager interface {
	// ApplyVIPs applies VIP assignments from config snapshot
	ApplyVIPs(ctx context.Context, assignments []*pb.VIPAssignment) error

	// Release releases all VIPs
	Release() error

	// GetActiveVIPs returns currently active VIPs
	GetActiveVIPs() []string

	// Start starts the VIP manager
	Start(ctx context.Context) error
}

// VIPManager manages VIP lifecycle with dual-stack support
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

	ospfHandler, err := NewOSPFHandler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSPF handler: %w", err)
	}

	return &VIPManager{
		logger:      logger,
		assignments: make(map[string]*pb.VIPAssignment),
		l2Handler:   l2Handler,
		bgpHandler:  bgpHandler,
		ospfHandler: ospfHandler,
	}, nil
}

// Start starts the VIP manager
func (m *VIPManager) Start(ctx context.Context) error {
	m.logger.Info("Starting VIP manager")

	if err := m.l2Handler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start L2 handler: %w", err)
	}

	if err := m.bgpHandler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start BGP handler: %w", err)
	}

	if err := m.ospfHandler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start OSPF handler: %w", err)
	}

	return nil
}

// ApplyVIPs applies new VIP assignments
func (m *VIPManager) ApplyVIPs(ctx context.Context, assignments []*pb.VIPAssignment) error {
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
			if err := m.releaseVIP(ctx, oldAssignment); err != nil {
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

		if err := m.applyVIP(ctx, assignment); err != nil {
			m.logger.Error("Failed to apply VIP",
				zap.String("vip", vipName),
				zap.Error(err),
			)
			continue
		}

		// For dual-stack: also apply the IPv6 address if present
		if assignment.Ipv6Address != "" && assignment.IsActive {
			ipv6Assignment := cloneAssignmentWithAddress(assignment, assignment.Ipv6Address)
			ipv6Assignment.VipName = vipName + "-v6"

			if err := m.applyVIP(ctx, ipv6Assignment); err != nil {
				m.logger.Error("Failed to apply IPv6 VIP",
					zap.String("vip", vipName),
					zap.String("ipv6_address", assignment.Ipv6Address),
					zap.Error(err),
				)
			}
		}
	}

	m.assignments = newAssignments
	return nil
}

// cloneAssignmentWithAddress creates a copy of a VIPAssignment with a different address
func cloneAssignmentWithAddress(orig *pb.VIPAssignment, address string) *pb.VIPAssignment {
	return &pb.VIPAssignment{
		VipName:    orig.VipName,
		Address:    address,
		Mode:       orig.Mode,
		Ports:      orig.Ports,
		IsActive:   orig.IsActive,
		BgpConfig:  orig.BgpConfig,
		OspfConfig: orig.OspfConfig,
		BfdConfig:  orig.BfdConfig,
	}
}

// applyVIP applies a single VIP assignment
func (m *VIPManager) applyVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	if !assignment.IsActive {
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), false)
		return nil
	}

	// Detect address family
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf(errInvalidVIPAddressFmt, assignment.Address, err)
	}

	isIPv6 := ip.To4() == nil

	m.logger.Debug("Applying VIP",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.Bool("ipv6", isIPv6),
	)

	var applyErr error
	switch assignment.Mode {
	case pb.VIPMode_L2_ARP:
		applyErr = m.l2Handler.AddVIP(ctx, assignment)
	case pb.VIPMode_BGP:
		applyErr = m.bgpHandler.AddVIP(ctx, assignment)
	case pb.VIPMode_OSPF:
		applyErr = m.ospfHandler.AddVIP(ctx, assignment)
	default:
		applyErr = fmt.Errorf("unsupported VIP mode: %v", assignment.Mode)
	}

	if applyErr == nil {
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), assignment.IsActive)
	}

	return applyErr
}

// releaseVIP releases a single VIP
func (m *VIPManager) releaseVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	var err error
	switch assignment.Mode {
	case pb.VIPMode_L2_ARP:
		err = m.l2Handler.RemoveVIP(ctx, assignment)
	case pb.VIPMode_BGP:
		err = m.bgpHandler.RemoveVIP(ctx, assignment)
	case pb.VIPMode_OSPF:
		err = m.ospfHandler.RemoveVIP(ctx, assignment)
	default:
		err = fmt.Errorf("unsupported VIP mode: %v", assignment.Mode)
	}

	if err == nil {
		metrics.SetVIPStatus(assignment.VipName, assignment.Address, assignment.Mode.String(), false)
	}

	// Also release dual-stack IPv6 address if present
	if assignment.Ipv6Address != "" {
		ipv6Assignment := cloneAssignmentWithAddress(assignment, assignment.Ipv6Address)
		ipv6Assignment.VipName = assignment.VipName + "-v6"

		var ipv6Err error
		switch assignment.Mode {
		case pb.VIPMode_L2_ARP:
			ipv6Err = m.l2Handler.RemoveVIP(ctx, ipv6Assignment)
		case pb.VIPMode_BGP:
			ipv6Err = m.bgpHandler.RemoveVIP(ctx, ipv6Assignment)
		case pb.VIPMode_OSPF:
			ipv6Err = m.ospfHandler.RemoveVIP(ctx, ipv6Assignment)
		}
		if ipv6Err != nil {
			m.logger.Error("Failed to release IPv6 VIP",
				zap.String("vip", assignment.VipName),
				zap.Error(ipv6Err),
			)
		}
	}

	return err
}

// Release releases all VIPs
func (m *VIPManager) Release() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Releasing all VIPs", zap.Int("count", len(m.assignments)))

	var errs []error
	for _, assignment := range m.assignments {
		if err := m.releaseVIP(context.Background(), assignment); err != nil {
			errs = append(errs, err)
		}
	}

	m.assignments = make(map[string]*pb.VIPAssignment)

	if len(errs) > 0 {
		return fmt.Errorf("failed to release some VIPs: %v", errs)
	}

	return nil
}

// GetActiveVIPs returns currently active VIPs
func (m *VIPManager) GetActiveVIPs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	active := make([]string, 0, len(m.assignments))
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
	if a.Ipv6Address != b.Ipv6Address {
		return false
	}
	if a.Mode != b.Mode {
		return false
	}
	if a.IsActive != b.IsActive {
		return false
	}
	if a.AddressFamily != b.AddressFamily {
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
