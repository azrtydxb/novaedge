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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLoadBalancingPolicyConstants(t *testing.T) {
	assert.Equal(t, LoadBalancingPolicy("RoundRobin"), LBPolicyRoundRobin)
	assert.Equal(t, LoadBalancingPolicy("P2C"), LBPolicyP2C)
	assert.Equal(t, LoadBalancingPolicy("EWMA"), LBPolicyEWMA)
	assert.Equal(t, LoadBalancingPolicy("RingHash"), LBPolicyRingHash)
	assert.Equal(t, LoadBalancingPolicy("Maglev"), LBPolicyMaglev)
	assert.Equal(t, LoadBalancingPolicy("LeastConn"), LBPolicyLeastConn)
}

func TestServiceReferenceFromBackend(t *testing.T) {
	namespace := "backend-ns"
	svc := ServiceReference{
		Name:      "backend-service",
		Namespace: &namespace,
		Port:      9090,
	}

	assert.Equal(t, "backend-service", svc.Name)
	assert.NotNil(t, svc.Namespace)
	assert.Equal(t, "backend-ns", *svc.Namespace)
	assert.Equal(t, int32(9090), svc.Port)
}

func TestServiceReferenceFromBackend_NilNamespace(t *testing.T) {
	svc := ServiceReference{
		Name: "backend-service",
		Port: 9090,
	}

	assert.Equal(t, "backend-service", svc.Name)
	assert.Nil(t, svc.Namespace)
	assert.Equal(t, int32(9090), svc.Port)
}

func TestCircuitBreakerFromBackend(t *testing.T) {
	maxConn := int32(100)
	maxPending := int32(50)
	maxReq := int32(200)
	maxRetries := int32(3)
	consecFailures := int32(5)

	cb := CircuitBreaker{
		MaxConnections:      &maxConn,
		MaxPendingRequests:  &maxPending,
		MaxRequests:         &maxReq,
		MaxRetries:          &maxRetries,
		ConsecutiveFailures: &consecFailures,
		Interval:            metav1.Duration{Duration: 10},
	}

	assert.Equal(t, int32(100), *cb.MaxConnections)
	assert.Equal(t, int32(50), *cb.MaxPendingRequests)
	assert.Equal(t, int32(200), *cb.MaxRequests)
	assert.Equal(t, int32(3), *cb.MaxRetries)
	assert.Equal(t, int32(5), *cb.ConsecutiveFailures)
}

func TestCircuitBreakerFromBackend_Empty(t *testing.T) {
	cb := CircuitBreaker{}

	assert.Nil(t, cb.MaxConnections)
	assert.Nil(t, cb.MaxPendingRequests)
	assert.Nil(t, cb.MaxRequests)
	assert.Nil(t, cb.MaxRetries)
	assert.Nil(t, cb.ConsecutiveFailures)
}
