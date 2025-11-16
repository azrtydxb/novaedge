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
// for the same IP address. The package supports three VIP modes:
//
// # L2 ARP Mode
//
// In L2 mode, a single node owns each VIP at any given time. The VIP is:
//   - Bound to the node's network interface
//   - Announced via Gratuitous ARP (GARP) broadcasts
//   - Transferred to another node on failure (controller-managed)
//
// L2 mode is suitable for:
//   - Simple deployments within a single L2 network
//   - Environments without BGP/OSPF infrastructure
//   - Small to medium clusters
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
//   - Sends periodic GARP announcements
//   - Removes the IP when released
//
// # BGP Mode
//
// In BGP mode, all healthy nodes announce the VIP via BGP to upstream routers.
// Traffic is distributed using ECMP (Equal-Cost Multi-Path) by the router.
//
// BGP mode is suitable for:
//   - Large-scale deployments
//   - Multi-datacenter environments
//   - Environments with BGP-capable routers
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
//   - Announces /32 host routes for each VIP
//   - Withdraws routes on VIP removal
//
// # OSPF Mode
//
// OSPF mode works similarly to BGP but uses OSPF LSAs for route advertisement.
// Traffic distribution is handled by OSPF-capable routers.
//
// OSPF mode is suitable for:
//   - Enterprise networks using OSPF
//   - Integration with existing OSPF infrastructure
//   - Scenarios requiring fast convergence
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
//   - Handles VIP assignment changes
//   - Releases VIPs that are no longer assigned
//   - Exports metrics for VIP status
//
// # Failover and High Availability
//
// L2 Mode Failover:
//   - Controller detects node failure via health checks
//   - Reassigns VIP to a healthy node
//   - New node binds IP and sends GARP
//   - Failover typically completes in 1-3 seconds
//
// BGP/OSPF Mode Failover:
//   - Node failure stops BGP/OSPF announcements
//   - Routers detect withdrawal and update routing tables
//   - Traffic automatically flows to remaining healthy nodes
//   - Failover speed depends on routing protocol convergence
//
// # Network Interface Management
//
// The L2 handler automatically detects the primary network interface by:
//   - Finding the interface associated with the default route
//   - Falling back to the first non-loopback interface
//   - Binding VIPs to this interface
//
// Interface selection can be customized if needed.
//
// # Metrics and Observability
//
// VIP handlers export metrics for:
//   - Number of active VIPs per node
//   - VIP status (active/inactive)
//   - BGP peering status
//   - Route announcement success/failure
//   - Failover events
//
// # Thread Safety
//
// All VIP handlers are safe for concurrent use:
//   - Mutex protection for VIP state
//   - Atomic operations where possible
//   - Safe for configuration updates during operation
package vip
