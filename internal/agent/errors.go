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

// Package agent provides the NovaEdge node agent implementation for config management.
package agent

import (
	"errors"
	"fmt"
)

// Common agent errors
var (
	// ErrConfigInvalid indicates the configuration is invalid
	ErrConfigInvalid = errors.New("invalid configuration")

	// ErrEndpointNotFound indicates an endpoint was not found
	ErrEndpointNotFound = errors.New("endpoint not found")

	// ErrNoHealthyEndpoints indicates no healthy endpoints are available
	ErrNoHealthyEndpoints = errors.New("no healthy endpoints available")

	// ErrPoolNotFound indicates a connection pool was not found
	ErrPoolNotFound = errors.New("connection pool not found")

	// ErrLoadBalancerNotFound indicates a load balancer was not found
	ErrLoadBalancerNotFound = errors.New("load balancer not found")

	// ErrHealthCheckFailed indicates a health check failed
	ErrHealthCheckFailed = errors.New("health check failed")

	// ErrCircuitBreakerOpen indicates the circuit breaker is open
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
)

// ConfigError represents a configuration error with additional context
type ConfigError struct {
	Field   string // The configuration field that caused the error
	Value   string // The invalid value
	Message string // Human-readable error message
	Err     error  // Underlying error, if any
}

func (e *ConfigError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("config error in field %q (value: %q): %s: %v", e.Field, e.Value, e.Message, e.Err)
	}
	return fmt.Sprintf("config error in field %q (value: %q): %s", e.Field, e.Value, e.Message)
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NewConfigError creates a new configuration error
func NewConfigError(field, value, message string, err error) *ConfigError {
	return &ConfigError{
		Field:   field,
		Value:   value,
		Message: message,
		Err:     err,
	}
}

// EndpointError represents an endpoint-related error
type EndpointError struct {
	Address string // Endpoint address
	Port    uint32 // Endpoint port
	Message string // Human-readable error message
	Err     error  // Underlying error, if any
}

func (e *EndpointError) Error() string {
	endpoint := fmt.Sprintf("%s:%d", e.Address, e.Port)
	if e.Err != nil {
		return fmt.Sprintf("endpoint error %s: %s: %v", endpoint, e.Message, e.Err)
	}
	return fmt.Sprintf("endpoint error %s: %s", endpoint, e.Message)
}

func (e *EndpointError) Unwrap() error {
	return e.Err
}

// NewEndpointError creates a new endpoint error
func NewEndpointError(address string, port uint32, message string, err error) *EndpointError {
	return &EndpointError{
		Address: address,
		Port:    port,
		Message: message,
		Err:     err,
	}
}

// LoadBalancerError represents a load balancer error
type LoadBalancerError struct {
	Cluster  string // Cluster name
	LBPolicy string // Load balancing policy
	Message  string // Human-readable error message
	Err      error  // Underlying error, if any
}

func (e *LoadBalancerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("load balancer error cluster=%s policy=%s: %s: %v", e.Cluster, e.LBPolicy, e.Message, e.Err)
	}
	return fmt.Sprintf("load balancer error cluster=%s policy=%s: %s", e.Cluster, e.LBPolicy, e.Message)
}

func (e *LoadBalancerError) Unwrap() error {
	return e.Err
}

// NewLoadBalancerError creates a new load balancer error
func NewLoadBalancerError(cluster, lbPolicy, message string, err error) *LoadBalancerError {
	return &LoadBalancerError{
		Cluster:  cluster,
		LBPolicy: lbPolicy,
		Message:  message,
		Err:      err,
	}
}
