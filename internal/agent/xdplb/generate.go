// Package xdplb provides XDP-based L4 load balancing that operates at the
// NIC driver level, bypassing the kernel network stack entirely for
// matched VIP traffic.
//
// The BPF program is compiled from bpf/xdp_lb.c using bpf2go.
// Run `go generate` in this package to regenerate the Go bindings after
// modifying the C source.
package xdplb

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -type vip_key -type vip_meta -type backend_entry -type backend_list_key xdpLb ../../../bpf/xdp_lb.c -- -I/usr/include/bpf -I/usr/include
