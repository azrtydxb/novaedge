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
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestValidateOverlayCIDR(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		wantErr bool
	}{
		{"valid IPv4 CIDR", "10.200.1.0/24", false},
		{"valid IPv4 host CIDR", "10.200.1.1/24", false},
		{"valid IPv6 CIDR", "fd00:200::/64", false},
		{"valid /32", "192.168.1.1/32", false},
		{"empty string", "", true},
		{"no mask", "10.200.1.0", true},
		{"invalid format", "not-a-cidr", true},
		{"invalid IP", "999.999.999.999/24", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOverlayCIDR(tt.cidr)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOverlayCIDR(%q) error = %v, wantErr %v", tt.cidr, err, tt.wantErr)
			}
		})
	}
}

func TestAnnounceOverlayPrefixValidation(t *testing.T) {
	logger := zap.NewNop()
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("failed to create BGP handler: %v", err)
	}

	ctx := context.Background()
	bgpConfig := &pb.BGPConfig{
		LocalAs:  65000,
		RouterId: "10.0.0.1",
	}

	// Test empty CIDR
	if err := handler.AnnounceOverlayPrefix(ctx, "", bgpConfig); err == nil {
		t.Error("expected error for empty CIDR")
	}

	// Test invalid CIDR
	if err := handler.AnnounceOverlayPrefix(ctx, "invalid", bgpConfig); err == nil {
		t.Error("expected error for invalid CIDR")
	}

	// Test nil config
	if err := handler.AnnounceOverlayPrefix(ctx, "10.200.1.0/24", nil); err == nil {
		t.Error("expected error for nil BGP config")
	}

	// Test with BGP server not started (valid CIDR and config but no server)
	if err := handler.AnnounceOverlayPrefix(ctx, "10.200.1.0/24", bgpConfig); err == nil {
		t.Error("expected error when BGP server is not started")
	}
}

func TestWithdrawOverlayPrefixValidation(t *testing.T) {
	logger := zap.NewNop()
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("failed to create BGP handler: %v", err)
	}

	ctx := context.Background()
	bgpConfig := &pb.BGPConfig{
		LocalAs:  65000,
		RouterId: "10.0.0.1",
	}

	// Test empty CIDR
	if err := handler.WithdrawOverlayPrefix(ctx, "", bgpConfig); err == nil {
		t.Error("expected error for empty CIDR")
	}

	// Test invalid CIDR
	if err := handler.WithdrawOverlayPrefix(ctx, "invalid", bgpConfig); err == nil {
		t.Error("expected error for invalid CIDR")
	}

	// Test nil config
	if err := handler.WithdrawOverlayPrefix(ctx, "10.200.1.0/24", nil); err == nil {
		t.Error("expected error for nil BGP config")
	}

	// Test with BGP server not started
	if err := handler.WithdrawOverlayPrefix(ctx, "10.200.1.0/24", bgpConfig); err == nil {
		t.Error("expected error when BGP server is not started")
	}
}
