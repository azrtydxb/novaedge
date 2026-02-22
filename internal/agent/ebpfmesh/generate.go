// Package ebpfmesh provides an eBPF-based mesh traffic interception backend
// using BPF_PROG_TYPE_SK_LOOKUP to redirect matching connections to the
// NovaEdge TPROXY listener without nftables/iptables rules.
//
// The BPF program is compiled from bpf/mesh_redirect.c using bpf2go.
// Run `go generate` in this package to regenerate the Go bindings after
// modifying the C source.
package ebpfmesh

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -type mesh_svc_key -type mesh_svc_value meshRedirect ../../bpf/mesh_redirect.c -- -I/usr/include/bpf -I/usr/include
