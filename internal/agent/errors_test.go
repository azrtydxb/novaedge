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

package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommonErrors(t *testing.T) {
	// Verify all common errors are defined
	assert.Error(t, ErrConfigInvalid)
	assert.Error(t, ErrEndpointNotFound)
	assert.Error(t, ErrNoHealthyEndpoints)
	assert.Error(t, ErrPoolNotFound)
	assert.Error(t, ErrLoadBalancerNotFound)
	assert.Error(t, ErrVIPOperationFailed)
	assert.Error(t, ErrHealthCheckFailed)
	assert.Error(t, ErrCircuitBreakerOpen)

	// Verify error messages
	assert.Equal(t, "invalid configuration", ErrConfigInvalid.Error())
	assert.Equal(t, "endpoint not found", ErrEndpointNotFound.Error())
	assert.Equal(t, "no healthy endpoints available", ErrNoHealthyEndpoints.Error())
	assert.Equal(t, "connection pool not found", ErrPoolNotFound.Error())
	assert.Equal(t, "load balancer not found", ErrLoadBalancerNotFound.Error())
	assert.Equal(t, "VIP operation failed", ErrVIPOperationFailed.Error())
	assert.Equal(t, "health check failed", ErrHealthCheckFailed.Error())
	assert.Equal(t, "circuit breaker is open", ErrCircuitBreakerOpen.Error())
}

func TestConfigError(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		value    string
		message  string
		err      error
		expected string
	}{
		{
			name:     "with underlying error",
			field:    "port",
			value:    "-1",
			message:  "port must be positive",
			err:      errors.New("invalid port range"),
			expected: `config error in field "port" (value: "-1"): port must be positive: invalid port range`,
		},
		{
			name:     "without underlying error",
			field:    "address",
			value:    "",
			message:  "address is required",
			err:      nil,
			expected: `config error in field "address" (value: ""): address is required`,
		},
		{
			name:     "empty values",
			field:    "",
			value:    "",
			message:  "",
			err:      nil,
			expected: `config error in field "" (value: ""): `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configErr := NewConfigError(tt.field, tt.value, tt.message, tt.err)
			assert.Equal(t, tt.field, configErr.Field)
			assert.Equal(t, tt.value, configErr.Value)
			assert.Equal(t, tt.message, configErr.Message)
			assert.Equal(t, tt.err, configErr.Err)
			assert.Equal(t, tt.expected, configErr.Error())

			// Test Unwrap
			unwrapped := configErr.Unwrap()
			assert.Equal(t, tt.err, unwrapped)
		})
	}
}

func TestConfigError_Is(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	configErr := NewConfigError("field", "value", "message", underlyingErr)

	// Test errors.Is
	assert.True(t, errors.Is(configErr, underlyingErr))
	assert.False(t, errors.Is(configErr, errors.New("different error")))
}

func TestEndpointError(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		port     uint32
		message  string
		err      error
		expected string
	}{
		{
			name:     "with underlying error",
			address:  "192.168.1.1",
			port:     8080,
			message:  "connection refused",
			err:      errors.New("network unreachable"),
			expected: "endpoint error 192.168.1.1:8080: connection refused: network unreachable",
		},
		{
			name:     "without underlying error",
			address:  "10.0.0.1",
			port:     443,
			message:  "health check failed",
			err:      nil,
			expected: "endpoint error 10.0.0.1:443: health check failed",
		},
		{
			name:     "zero port",
			address:  "localhost",
			port:     0,
			message:  "invalid port",
			err:      nil,
			expected: "endpoint error localhost:0: invalid port",
		},
		{
			name:     "max port",
			address:  "example.com",
			port:     65535,
			message:  "connection timeout",
			err:      nil,
			expected: "endpoint error example.com:65535: connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpointErr := NewEndpointError(tt.address, tt.port, tt.message, tt.err)
			assert.Equal(t, tt.address, endpointErr.Address)
			assert.Equal(t, tt.port, endpointErr.Port)
			assert.Equal(t, tt.message, endpointErr.Message)
			assert.Equal(t, tt.err, endpointErr.Err)
			assert.Equal(t, tt.expected, endpointErr.Error())

			// Test Unwrap
			unwrapped := endpointErr.Unwrap()
			assert.Equal(t, tt.err, unwrapped)
		})
	}
}

func TestEndpointError_Is(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	endpointErr := NewEndpointError("192.168.1.1", 8080, "message", underlyingErr)

	// Test errors.Is
	assert.True(t, errors.Is(endpointErr, underlyingErr))
	assert.False(t, errors.Is(endpointErr, errors.New("different error")))
}

func TestVIPError(t *testing.T) {
	tests := []struct {
		name     string
		vipName  string
		address  string
		mode     string
		message  string
		err      error
		expected string
	}{
		{
			name:     "with underlying error",
			vipName:  "vip-1",
			address:  "192.168.1.100",
			mode:     "L2_ARP",
			message:  "failed to advertise",
			err:      errors.New("interface not found"),
			expected: "VIP error vip-1 (192.168.1.100) mode=L2_ARP: failed to advertise: interface not found",
		},
		{
			name:     "without underlying error",
			vipName:  "vip-2",
			address:  "10.0.0.100",
			mode:     "BGP",
			message:  "BGP session failed",
			err:      nil,
			expected: "VIP error vip-2 (10.0.0.100) mode=BGP: BGP session failed",
		},
		{
			name:     "OSPF mode",
			vipName:  "vip-3",
			address:  "172.16.0.100",
			mode:     "OSPF",
			message:  "OSPF neighbor down",
			err:      nil,
			expected: "VIP error vip-3 (172.16.0.100) mode=OSPF: OSPF neighbor down",
		},
		{
			name:     "empty values",
			vipName:  "",
			address:  "",
			mode:     "",
			message:  "",
			err:      nil,
			expected: "VIP error  () mode=: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vipErr := NewVIPError(tt.vipName, tt.address, tt.mode, tt.message, tt.err)
			assert.Equal(t, tt.vipName, vipErr.VIPName)
			assert.Equal(t, tt.address, vipErr.Address)
			assert.Equal(t, tt.mode, vipErr.Mode)
			assert.Equal(t, tt.message, vipErr.Message)
			assert.Equal(t, tt.err, vipErr.Err)
			assert.Equal(t, tt.expected, vipErr.Error())

			// Test Unwrap
			unwrapped := vipErr.Unwrap()
			assert.Equal(t, tt.err, unwrapped)
		})
	}
}

func TestVIPError_Is(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	vipErr := NewVIPError("vip-1", "192.168.1.100", "L2_ARP", "message", underlyingErr)

	// Test errors.Is
	assert.True(t, errors.Is(vipErr, underlyingErr))
	assert.False(t, errors.Is(vipErr, errors.New("different error")))
}

func TestLoadBalancerError(t *testing.T) {
	tests := []struct {
		name     string
		cluster  string
		lbPolicy string
		message  string
		err      error
		expected string
	}{
		{
			name:     "with underlying error",
			cluster:  "cluster-1",
			lbPolicy: "ROUND_ROBIN",
			message:  "no endpoints available",
			err:      errors.New("all endpoints unhealthy"),
			expected: "load balancer error cluster=cluster-1 policy=ROUND_ROBIN: no endpoints available: all endpoints unhealthy",
		},
		{
			name:     "without underlying error",
			cluster:  "cluster-2",
			lbPolicy: "LEAST_CONNECTION",
			message:  "connection pool exhausted",
			err:      nil,
			expected: "load balancer error cluster=cluster-2 policy=LEAST_CONNECTION: connection pool exhausted",
		},
		{
			name:     "RING_HASH policy",
			cluster:  "cluster-3",
			lbPolicy: "RING_HASH",
			message:  "hash ring not initialized",
			err:      nil,
			expected: "load balancer error cluster=cluster-3 policy=RING_HASH: hash ring not initialized",
		},
		{
			name:     "empty values",
			cluster:  "",
			lbPolicy: "",
			message:  "",
			err:      nil,
			expected: "load balancer error cluster= policy=: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbErr := NewLoadBalancerError(tt.cluster, tt.lbPolicy, tt.message, tt.err)
			assert.Equal(t, tt.cluster, lbErr.Cluster)
			assert.Equal(t, tt.lbPolicy, lbErr.LBPolicy)
			assert.Equal(t, tt.message, lbErr.Message)
			assert.Equal(t, tt.err, lbErr.Err)
			assert.Equal(t, tt.expected, lbErr.Error())

			// Test Unwrap
			unwrapped := lbErr.Unwrap()
			assert.Equal(t, tt.err, unwrapped)
		})
	}
}

func TestLoadBalancerError_Is(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	lbErr := NewLoadBalancerError("cluster-1", "ROUND_ROBIN", "message", underlyingErr)

	// Test errors.Is
	assert.True(t, errors.Is(lbErr, underlyingErr))
	assert.False(t, errors.Is(lbErr, errors.New("different error")))
}

func TestErrorChaining(t *testing.T) {
	// Test error chaining with multiple levels
	baseErr := errors.New("base error")
	configErr := NewConfigError("field", "value", "config failed", baseErr)
	endpointErr := NewEndpointError("192.168.1.1", 8080, "endpoint failed", configErr)

	// Test that we can unwrap through the chain
	assert.True(t, errors.Is(endpointErr, configErr))
	assert.True(t, errors.Is(endpointErr, baseErr))
	assert.True(t, errors.Is(configErr, baseErr))
}

func TestNewConfigError_NilError(t *testing.T) {
	configErr := NewConfigError("field", "value", "message", nil)
	assert.Nil(t, configErr.Err)
	assert.NotNil(t, configErr)
}

func TestNewEndpointError_NilError(t *testing.T) {
	endpointErr := NewEndpointError("192.168.1.1", 8080, "message", nil)
	assert.Nil(t, endpointErr.Err)
	assert.NotNil(t, endpointErr)
}

func TestNewVIPError_NilError(t *testing.T) {
	vipErr := NewVIPError("vip-1", "192.168.1.100", "L2_ARP", "message", nil)
	assert.Nil(t, vipErr.Err)
	assert.NotNil(t, vipErr)
}

func TestNewLoadBalancerError_NilError(t *testing.T) {
	lbErr := NewLoadBalancerError("cluster-1", "ROUND_ROBIN", "message", nil)
	assert.Nil(t, lbErr.Err)
	assert.NotNil(t, lbErr)
}
