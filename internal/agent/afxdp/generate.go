// Package afxdp provides AF_XDP (XSK) zero-copy packet processing for
// high-throughput data plane acceleration. The XDP filter program is
// compiled from bpf/afxdp_redirect.c using bpf2go.
//
// Run `go generate` in this package to regenerate the Go bindings after
// modifying the C source.
package afxdp

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -type vip_key afxdpRedirect ../../bpf/afxdp_redirect.c -- -I/usr/include/bpf -I/usr/include
