// Package conntrack provides an eBPF LRU connection tracking table for
// pinning established connections to backends across Maglev table rebuilds.
// The LRU hash map automatically evicts idle entries, while a Go-side GC
// goroutine periodically removes entries exceeding a configurable max age.
//
// The BPF program is compiled from bpf/conntrack.c using bpf2go.
// Run `go generate` in this package to regenerate the Go bindings after
// modifying the C source.
package conntrack

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -type ct_key -type ct_entry conntrack ../../../../bpf/conntrack.c -- -I/usr/include/bpf -I/usr/include
