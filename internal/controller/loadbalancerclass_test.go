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

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func TestShouldReconcileGateway_MatchingClass(t *testing.T) {
	r := &ProxyGatewayReconciler{
		ControllerClass: "novaedge.io/proxy",
	}

	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:            "test-vip",
			LoadBalancerClass: "novaedge.io/proxy",
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	if !r.shouldReconcileGateway(gw) {
		t.Error("expected shouldReconcileGateway to return true for matching class")
	}
}

func TestShouldReconcileGateway_NonMatchingClass(t *testing.T) {
	r := &ProxyGatewayReconciler{
		ControllerClass: "novaedge.io/proxy",
	}

	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:            "test-vip",
			LoadBalancerClass: "other-controller.io/lb",
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	if r.shouldReconcileGateway(gw) {
		t.Error("expected shouldReconcileGateway to return false for non-matching class")
	}
}

func TestShouldReconcileGateway_EmptyGatewayClass(t *testing.T) {
	r := &ProxyGatewayReconciler{
		ControllerClass: "novaedge.io/proxy",
	}

	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef: "test-vip",
			// No LoadBalancerClass set
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	// With default controller class, should reconcile gateways without class
	if !r.shouldReconcileGateway(gw) {
		t.Error("expected shouldReconcileGateway to return true for empty gateway class with default controller")
	}
}

func TestShouldReconcileGateway_EmptyControllerClass(t *testing.T) {
	r := &ProxyGatewayReconciler{
		ControllerClass: "", // Uses default
	}

	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:            "test-vip",
			LoadBalancerClass: "novaedge.io/proxy",
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	if !r.shouldReconcileGateway(gw) {
		t.Error("expected shouldReconcileGateway to return true when controller uses default class")
	}
}

func TestShouldReconcileGateway_CustomClass(t *testing.T) {
	r := &ProxyGatewayReconciler{
		ControllerClass: "custom.io/loadbalancer",
	}

	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:            "test-vip",
			LoadBalancerClass: "custom.io/loadbalancer",
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	if !r.shouldReconcileGateway(gw) {
		t.Error("expected shouldReconcileGateway to return true for matching custom class")
	}

	// With default class, should not match
	gwDefault := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "default-gw", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:            "test-vip",
			LoadBalancerClass: "novaedge.io/proxy",
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"},
			},
		},
	}

	if r.shouldReconcileGateway(gwDefault) {
		t.Error("expected shouldReconcileGateway to return false for default class when controller uses custom class")
	}
}
