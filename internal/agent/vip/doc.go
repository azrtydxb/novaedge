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

// Package vip implements Virtual IP (VIP) management for NovaEdge agents.
//
// VIPs enable high availability by allowing multiple nodes to serve traffic
// for the same IP address. The package supports three VIP modes with full
// IPv4, IPv6, and dual-stack support.
//
// # L2 ARP/NDP Mode
//
// In L2 mode, a single node owns each VIP at any given time. The VIP is:
//   - Bound to the node's network interface
//   - Announced via Gratuitous ARP (IPv4) or Unsolicited Neighbor Advertisement (IPv6)
//   - Transferred to another node on failure (controller-managed)
//
// L2 mode is suitable for:
//   - Simple deployments within a single L2 network
//   - Environments without BGP/OSPF infrastructure
//   - Small to medium clusters
//   - IPv6-only or dual-stack environments (NDP support)
//
// Example L2 VIP lifecycle:
//
//	handler := vip.NewL2Handler(logger)
//	handler.Start(ctx)
//	handler.AddVIP(&pb.VIPAssignment{
//	    VipName: "frontend-vip",
//	    Address: "192.168.1.100/24",
//	    Mode: pb.VIPMode_L2_ARP,
//	    IsActive: true,
//	})
//
// The handler automatically:
//   - Binds the IP to the primary network interface
//   - Sends periodic GARP (IPv4) or NA (IPv6) announcements
//   - Removes the IP when released
//
// # BGP Mode
//
// In BGP mode, all healthy nodes announce the VIP via BGP to upstream routers.
// Traffic is distributed using ECMP (Equal-Cost Multi-Path) by the router.
// BGP supports both IPv4 and IPv6 address families with MP_REACH_NLRI.
//
// BGP mode is suitable for:
//   - Large-scale deployments
//   - Multi-datacenter environments
//   - Environments with BGP-capable routers
//   - Dual-stack deployments requiring IPv4 and IPv6
//
// Example BGP VIP lifecycle:
//
//	handler := vip.NewBGPHandler(logger)
//	handler.Start(ctx)
//	handler.AddVIP(&pb.VIPAssignment{
//	    VipName: "api-vip",
//	    Address: "203.0.113.10/32",
//	    Mode: pb.VIPMode_BGP,
//	    IsActive: true,
//	    BgpConfig: &pb.BGPConfig{
//	        LocalAs: 65000,
//	        RouterId: "10.0.0.1",
//	        Peers: []*pb.BGPPeer{
//	            {Address: "10.0.0.254", As: 65001},
//	        },
//	    },
//	})
//
// The handler:
//   - Starts a BGP server (GoBGP)
//   - Establishes peering with configured routers
//   - Announces /32 (IPv4) or /128 (IPv6) host routes for each VIP
//   - Supports BFD for sub-second failure detection
//   - Withdraws routes on VIP removal or BFD session failure
//
// # BFD (Bidirectional Forwarding Detection)
//
// BFD provides sub-second failure detection for BGP sessions per RFC 5880.
// When enabled, BFD sessions monitor peer connectivity and trigger immediate
// route withdrawal when a peer becomes unreachable.
//
// The BFD implementation includes:
//   - Full state machine (AdminDown, Down, Init, Up)
//   - Configurable detection multiplier and timing intervals
//   - Session flap tracking and metrics
//   - Automatic integration with BGP route management
//
// Example BFD configuration:
//
//	handler.AddVIP(&pb.VIPAssignment{
//	    // ... BGP config ...
//	    BfdConfig: &pb.BFDConfig{
//	        Enabled: true,
//	        DetectMultiplier: 3,
//	        DesiredMinTxInterval: "300ms",
//	        RequiredMinRxInterval: "300ms",
//	    },
//	})
//
// # OSPF Mode
//
// OSPF mode announces VIPs as AS-External LSAs for active-active load
// distribution. The implementation supports both OSPFv2 (IPv4) and
// OSPFv3 (IPv6) with full protocol features.
//
// OSPF features:
//   - OSPFv2 Type-5 AS-External LSAs for IPv4
//   - OSPFv3 Type-0x4005 LSAs for IPv6
//   - Full neighbor state machine (Down through Full)
//   - Area support with configurable area ID
//   - Cost-based routing with configurable metrics
//   - MD5 and plaintext authentication
//   - Graceful restart support
//   - LSA aging and periodic refresh
//
// OSPF mode is suitable for:
//   - Enterprise networks using OSPF
//   - Integration with existing OSPF infrastructure
//   - Scenarios requiring fast convergence
//   - IPv6 networks using OSPFv3
//
// # VIP Manager
//
// The VIP Manager coordinates all VIP modes and handles configuration updates:
//
//	manager := vip.NewManager(logger)
//	manager.Start(ctx)
//	manager.ApplyVIPs([]*pb.VIPAssignment{...})
//
// The manager:
//   - Routes VIPs to the appropriate handler based on mode
//   - Handles VIP assignment changes (add, update, remove)
//   - Supports dual-stack by creating paired IPv4+IPv6 assignments
//   - Releases VIPs that are no longer assigned
//   - Exports metrics for VIP status
//
// # IPv6 and Dual-Stack Support
//
// All VIP modes support IPv6:
//   - L2 mode uses Unsolicited Neighbor Advertisements (NDP)
//   - BGP mode uses AFI_IP6/SAFI_UNICAST with MP_REACH_NLRI
//   - OSPF mode uses OSPFv3 with Type-0x4005 LSAs
//
// Dual-stack VIPs (addressFamily: "dual") bind both IPv4 and IPv6 addresses
// simultaneously. The VIP manager creates separate handler entries for each
// address family while maintaining them as a single logical VIP.
//
// # Failover and High Availability
//
// L2 Mode Failover:
//   - Controller detects node failure via health checks
//   - Reassigns VIP to a healthy node
//   - New node binds IP and sends GARP/NA
//   - Failover typically completes in 1-3 seconds
//
// BGP Mode Failover:
//   - Without BFD: depends on BGP hold timer (default 90s)
//   - With BFD: sub-second detection (detectMultiplier * minRxInterval)
//   - Routes automatically withdrawn on failure detection
//
// OSPF Mode Failover:
//   - Depends on OSPF dead interval (default 40s)
//   - Graceful restart prevents traffic disruption during planned restarts
//
// # Metrics and Observability
//
// VIP handlers export metrics for:
//   - Number of active VIPs per node
//   - VIP status (active/inactive)
//   - BGP peering status
//   - Route announcement success/failure
//   - BFD session state, packet counts, and flap tracking
//   - OSPF neighbor states and LSDB counts
//   - Failover events
//
// # Thread Safety
//
// All VIP handlers are safe for concurrent use:
//   - Mutex protection for VIP state
//   - Atomic operations where possible
//   - Safe for configuration updates during operation
package vip
