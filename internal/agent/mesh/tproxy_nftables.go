//go:build linux

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

package mesh

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

const (
	// nftTableName is the nftables table used for TPROXY rules.
	nftTableName = "novaedge_mesh"

	// nftChainName is the chain within the table for TPROXY rules.
	// Avoids "tproxy" which is a reserved keyword in the nft CLI.
	nftChainName = "mesh_intercept"

	// routingTable is the policy routing table number.
	routingTable = 100

	// fwmarkValue is the packet mark set by TPROXY.
	fwmarkValue = 1
)

// nftablesBackend implements RuleBackend using the nftables netlink API
// for atomic rule updates.
type nftablesBackend struct {
	logger *zap.Logger
	conn   *nftables.Conn
	table  *nftables.Table
	chain  *nftables.Chain
}

func newNFTablesBackend(logger *zap.Logger, conn *nftables.Conn) *nftablesBackend {
	return &nftablesBackend{
		logger: logger,
		conn:   conn,
	}
}

func (b *nftablesBackend) Name() string { return "nftables" }

// Setup creates the novaedge_mesh table, tproxy chain (prerouting hook,
// mangle priority), and configures policy routing via netlink.
func (b *nftablesBackend) Setup() error {
	b.table = b.conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	b.chain = b.conn.AddChain(&nftables.Chain{
		Name:     nftChainName,
		Table:    b.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle,
	})

	if err := b.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush (table+chain): %w", err)
	}

	if err := b.ensureRouting(); err != nil {
		return fmt.Errorf("policy routing setup: %w", err)
	}

	return nil
}

// ApplyRules atomically replaces all TPROXY rules: flush the chain, add
// one rule per target, and commit in a single netlink batch.
func (b *nftablesBackend) ApplyRules(targets []InterceptTarget, tproxyPort int32) error {
	// Flush all existing rules in the chain.
	b.conn.FlushChain(b.chain)

	// Add one rule per intercept target.
	for _, t := range targets {
		rule, err := b.buildRule(t, tproxyPort)
		if err != nil {
			return fmt.Errorf("build rule for %s: %w", t.Key(), err)
		}
		b.conn.AddRule(rule)
	}

	// Atomic commit.
	if err := b.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush (apply rules): %w", err)
	}
	return nil
}

// Cleanup removes the entire novaedge_mesh table (and all its chains/rules)
// and cleans up routing entries.
func (b *nftablesBackend) Cleanup() error {
	if b.table != nil {
		b.conn.DelTable(b.table)
		if err := b.conn.Flush(); err != nil {
			b.logger.Warn("Failed to delete nftables table", zap.Error(err))
		}
		b.table = nil
		b.chain = nil
	}

	b.cleanupRouting()
	return nil
}

// buildRule constructs an nftables rule matching TCP + dst IP + dst port,
// then sets the fwmark and TPROXY redirect.
func (b *nftablesBackend) buildRule(t InterceptTarget, tproxyPort int32) (*nftables.Rule, error) {
	ip := net.ParseIP(t.ClusterIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address: %s", t.ClusterIP)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(t.Port))

	tproxyPortBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(tproxyPortBytes, uint16(tproxyPort))

	markBytes := make([]byte, 4)
	binary.NativeEndian.PutUint32(markBytes, fwmarkValue)

	return &nftables.Rule{
		Table: b.table,
		Chain: b.chain,
		Exprs: []expr.Any{
			// Match L4 protocol == TCP.
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{unix.IPPROTO_TCP},
			},

			// Match destination IP address.
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       16, // IPv4 destination address offset
				Len:          4,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ip,
			},

			// Match destination port.
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       2, // TCP destination port offset
				Len:          2,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     portBytes,
			},

			// Set fwmark.
			&expr.Immediate{
				Register: 1,
				Data:     markBytes,
			},
			&expr.Meta{
				Key:            expr.MetaKeyMARK,
				SourceRegister: true,
				Register:       1,
			},

			// TPROXY redirect to local port.
			&expr.Immediate{
				Register: 1,
				Data:     tproxyPortBytes,
			},
			&expr.TProxy{
				Family:      byte(unix.NFPROTO_IPV4),
				TableFamily: byte(unix.NFPROTO_IPV4),
				RegPort:     1,
			},
		},
	}, nil
}

// ensureRouting adds the ip rule (fwmark 1 → table 100) and local route
// using the netlink API. Existing entries are tolerated (idempotent).
func (b *nftablesBackend) ensureRouting() error {
	// ip rule add fwmark 1 lookup 100
	rule := netlink.NewRule()
	rule.Mark = fwmarkValue
	rule.Table = routingTable
	if err := netlink.RuleAdd(rule); err != nil && !isExistError(err) {
		return fmt.Errorf("netlink RuleAdd: %w", err)
	}

	// ip route add local 0.0.0.0/0 dev lo table 100
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("netlink LinkByName(lo): %w", err)
	}

	route := &netlink.Route{
		Table:     routingTable,
		LinkIndex: lo.Attrs().Index,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Type:      unix.RTN_LOCAL,
		Scope:     netlink.SCOPE_HOST,
	}
	if err := netlink.RouteAdd(route); err != nil && !isExistError(err) {
		return fmt.Errorf("netlink RouteAdd: %w", err)
	}

	return nil
}

// cleanupRouting removes the policy routing rule and route added by ensureRouting.
func (b *nftablesBackend) cleanupRouting() {
	rule := netlink.NewRule()
	rule.Mark = fwmarkValue
	rule.Table = routingTable
	_ = netlink.RuleDel(rule)

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return
	}
	route := &netlink.Route{
		Table:     routingTable,
		LinkIndex: lo.Attrs().Index,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Type:      unix.RTN_LOCAL,
		Scope:     netlink.SCOPE_HOST,
	}
	_ = netlink.RouteDel(route)
}

// isExistError returns true if the error indicates the entry already exists.
func isExistError(err error) bool {
	return err == syscall.EEXIST
}

// detectBackend probes for nftables support and falls back to iptables.
func detectBackend(logger *zap.Logger) RuleBackend {
	conn, err := nftables.New()
	if err != nil {
		logger.Info("nftables not available, using iptables fallback", zap.Error(err))
		return newIPTablesBackend(logger)
	}
	// Verify nftables is functional by listing tables.
	if _, err := conn.ListTables(); err != nil {
		logger.Info("nftables not functional, using iptables fallback", zap.Error(err))
		return newIPTablesBackend(logger)
	}
	return newNFTablesBackend(logger, conn)
}
