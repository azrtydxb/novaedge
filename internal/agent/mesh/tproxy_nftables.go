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
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

var (
	errInvalidIPv4Addr = errors.New("invalid IPv4 address")
	errPortOutOfRange  = errors.New("port out of range")
)

const (
	// nftTableName is the nftables table used for mesh interception rules.
	nftTableName = "novaedge_mesh"

	// nftChainName is the NAT chain that redirects intercepted packets to
	// the local transparent listener. Uses priority dstnat - 1 (-101) to
	// fire before kube-proxy's DNAT rules, preserving the original ClusterIP
	// destination in conntrack so SO_ORIGINAL_DST can retrieve it.
	nftChainName = "mesh_redirect"
)

// nftablesBackend implements RuleBackend using the nftables netlink API
// for atomic rule updates. It uses DNAT to 127.0.0.1 (not REDIRECT or
// TPROXY) for universal CNI compatibility -- see the package-level
// documentation in tproxy.go for the full rationale and trade-off analysis.
//
// We use explicit DNAT instead of nftables "redirect" because redirect
// derives the destination IP from the incoming interface's primary IPv4
// address. CNIs that use veth pairs without IPv4 addresses (e.g. NovaNet,
// Calico eBPF mode) cause redirect to silently drop packets because there
// is no IPv4 address on the veth to redirect to.
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

// Setup creates the novaedge_mesh table with a single NAT chain:
//   - mesh_redirect (prerouting, dstnat - 1 priority): DNAT matching
//     TCP packets to the local transparent listener at 127.0.0.1.
//
// The priority of -101 (NF_IP_PRI_NAT_DST - 1) ensures our DNAT fires
// before kube-proxy's DNAT rules at priority -100, so the original ClusterIP
// destination is preserved in conntrack for SO_ORIGINAL_DST retrieval.
func (b *nftablesBackend) Setup() error {
	// Enable route_localnet so the kernel accepts DNAT to 127.0.0.1 on non-loopback interfaces.
	if err := os.WriteFile("/proc/sys/net/ipv4/conf/all/route_localnet", []byte("1"), 0o600); err != nil {
		return fmt.Errorf("failed to set route_localnet: %w", err)
	}

	// Delete existing table first to remove any stale chains/rules from
	// previous versions (e.g. upgrading from TPROXY to REDIRECT).
	b.conn.DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})
	_ = b.conn.Flush() // Ignore error: table may not exist yet.

	b.table = b.conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	// NAT chain at priority dstnat - 1 = -101, before kube-proxy (-100).
	b.chain = b.conn.AddChain(&nftables.Chain{
		Name:     nftChainName,
		Table:    b.table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityRef(-101),
	})

	if err := b.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush (table+chain): %w", err)
	}

	return nil
}

// ApplyRules atomically replaces all DNAT rules: flush the chain, add
// one DNAT rule per target, then commit in a single netlink batch.
func (b *nftablesBackend) ApplyRules(targets []InterceptTarget, tproxyPort int32) error {
	b.conn.FlushChain(b.chain)

	for _, t := range targets {
		rule, err := b.buildRedirectRule(t, tproxyPort)
		if err != nil {
			return fmt.Errorf("build redirect rule for %s: %w", t.Key(), err)
		}
		b.conn.AddRule(rule)
	}

	if err := b.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush (apply rules): %w", err)
	}
	return nil
}

// Cleanup removes the entire novaedge_mesh table and all its chains/rules.
func (b *nftablesBackend) Cleanup() error {
	if b.table != nil {
		b.conn.DelTable(b.table)
		if err := b.conn.Flush(); err != nil {
			return fmt.Errorf("nftables flush (cleanup): %w", err)
		}
		b.table = nil
		b.chain = nil
	}
	return nil
}

// buildRedirectRule constructs an nftables rule matching TCP + dst IP + dst
// port and DNAT-ing to 127.0.0.1:<tproxyPort>. This is equivalent to:
//
//	ip daddr <clusterIP> tcp dport <port> dnat to 127.0.0.1:<tproxyPort>
//
// We use explicit DNAT instead of nftables "redirect" because redirect
// derives the destination IP from the incoming interface's primary IPv4
// address. CNIs that use veth pairs without IPv4 addresses (e.g. NovaNet,
// Calico eBPF mode) cause redirect to silently fail because there is no
// IPv4 address to redirect to. Explicit DNAT to 127.0.0.1 works
// universally regardless of the incoming interface configuration.
func (b *nftablesBackend) buildRedirectRule(t InterceptTarget, tproxyPort int32) (*nftables.Rule, error) {
	ip := net.ParseIP(t.ClusterIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("%w: %s", errInvalidIPv4Addr, t.ClusterIP)
	}
	if t.Port < 1 || t.Port > 65535 {
		return nil, fmt.Errorf("destination %w: %d", errPortOutOfRange, t.Port)
	}
	if tproxyPort < 1 || tproxyPort > 65535 {
		return nil, fmt.Errorf("redirect %w: %d", errPortOutOfRange, tproxyPort)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(t.Port))

	redirPortBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(redirPortBytes, uint16(tproxyPort))

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

			// Load 127.0.0.1 into register 1 for DNAT destination address.
			&expr.Immediate{
				Register: 1,
				Data:     net.IPv4(127, 0, 0, 1).To4(),
			},

			// Load redirect port into register 2 for DNAT destination port.
			&expr.Immediate{
				Register: 2,
				Data:     redirPortBytes,
			},

			// DNAT to 127.0.0.1:<tproxyPort>. Unlike "redirect", DNAT uses
			// an explicit destination address and does not depend on the
			// incoming interface having an IPv4 address.
			&expr.NAT{
				Type:        expr.NATTypeDestNAT,
				Family:      unix.NFPROTO_IPV4,
				RegAddrMin:  1,
				RegProtoMin: 2,
			},
		},
	}, nil
}

// detectBackend probes for the best available netfilter-based interception
// backend: nftables (preferred) or iptables (fallback).
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
