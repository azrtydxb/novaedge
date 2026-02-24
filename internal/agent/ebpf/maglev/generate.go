// Package maglev provides an eBPF-accelerated Maglev consistent hashing
// lookup table for XDP-based L4 load balancing. The Maglev table is stored
// in BPF maps and supports atomic table swap via ARRAY_OF_MAPS, ensuring
// zero-downtime backend updates.
//
// The BPF program is compiled from bpf/maglev_lookup.c using bpf2go.
// Run `go generate` in this package to regenerate the Go bindings after
// modifying the C source.
package maglev

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -type maglev_entry -type backend_key -type backend_value maglevLookup ../../../../bpf/maglev_lookup.c -- -I/usr/include/bpf -I/usr/include
