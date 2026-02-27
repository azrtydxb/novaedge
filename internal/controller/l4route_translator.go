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
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)
var (
	errTCPRouteRule = errors.New("TCPRoute rule")
	errTLSRouteRule = errors.New("TLSRoute rule")
)


// TranslateTCPRouteToProxyRoute translates a Gateway API TCPRoute to a NovaEdge ProxyRoute
// TCPRoute is simpler than HTTPRoute: it only specifies backend refs without match conditions
func TranslateTCPRouteToProxyRoute(tcpRoute *gatewayv1alpha2.TCPRoute) (*novaedgev1alpha1.ProxyRoute, error) {
	rules := make([]novaedgev1alpha1.HTTPRouteRule, 0, len(tcpRoute.Spec.Rules))

	for i, gwRule := range tcpRoute.Spec.Rules {
		if len(gwRule.BackendRefs) == 0 {
			return nil, fmt.Errorf("%w: %d has no backend refs", errTCPRouteRule, i)
		}

		rule := novaedgev1alpha1.HTTPRouteRule{
			BackendRefs: make([]novaedgev1alpha1.BackendRef, 0, len(gwRule.BackendRefs)),
		}

		for _, br := range gwRule.BackendRefs {
			backendNS := tcpRoute.Namespace
			if br.Namespace != nil {
				backendNS = string(*br.Namespace)
			}

			backendRef := novaedgev1alpha1.BackendRef{
				Name:      string(br.Name),
				Namespace: &backendNS,
			}
			if br.Weight != nil {
				backendRef.Weight = br.Weight
			} else {
				defaultWeight := int32(1)
				backendRef.Weight = &defaultWeight
			}

			rule.BackendRefs = append(rule.BackendRefs, backendRef)
		}

		rules = append(rules, rule)
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tcpRoute.Name,
			Namespace: tcpRoute.Namespace,
			Labels:    tcpRoute.Labels,
			Annotations: map[string]string{
				OwnerAnnotation:           fmt.Sprintf("TCPRoute/%s/%s", tcpRoute.Namespace, tcpRoute.Name),
				"novaedge.io/l4-protocol": "TCP",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1alpha2.GroupVersion.String(),
					Kind:       "TCPRoute",
					Name:       tcpRoute.Name,
					UID:        tcpRoute.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Rules: rules,
		},
	}

	return proxyRoute, nil
}

// TranslateTLSRouteToProxyRoute translates a Gateway API TLSRoute to a NovaEdge ProxyRoute
// TLSRoute specifies SNI hostnames and backend refs for TLS passthrough
func TranslateTLSRouteToProxyRoute(tlsRoute *gatewayv1alpha2.TLSRoute) (*novaedgev1alpha1.ProxyRoute, error) {
	hostnames := make([]string, 0, len(tlsRoute.Spec.Hostnames))
	for _, hostname := range tlsRoute.Spec.Hostnames {
		hostnames = append(hostnames, string(hostname))
	}

	rules := make([]novaedgev1alpha1.HTTPRouteRule, 0, len(tlsRoute.Spec.Rules))

	for i, gwRule := range tlsRoute.Spec.Rules {
		if len(gwRule.BackendRefs) == 0 {
			return nil, fmt.Errorf("%w: %d has no backend refs", errTLSRouteRule, i)
		}

		rule := novaedgev1alpha1.HTTPRouteRule{
			BackendRefs: make([]novaedgev1alpha1.BackendRef, 0, len(gwRule.BackendRefs)),
		}

		for _, br := range gwRule.BackendRefs {
			backendNS := tlsRoute.Namespace
			if br.Namespace != nil {
				backendNS = string(*br.Namespace)
			}

			backendRef := novaedgev1alpha1.BackendRef{
				Name:      string(br.Name),
				Namespace: &backendNS,
			}
			if br.Weight != nil {
				backendRef.Weight = br.Weight
			} else {
				defaultWeight := int32(1)
				backendRef.Weight = &defaultWeight
			}

			rule.BackendRefs = append(rule.BackendRefs, backendRef)
		}

		rules = append(rules, rule)
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsRoute.Name,
			Namespace: tlsRoute.Namespace,
			Labels:    tlsRoute.Labels,
			Annotations: map[string]string{
				OwnerAnnotation:           fmt.Sprintf("TLSRoute/%s/%s", tlsRoute.Namespace, tlsRoute.Name),
				"novaedge.io/l4-protocol": "TLS",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1alpha2.GroupVersion.String(),
					Kind:       "TLSRoute",
					Name:       tlsRoute.Name,
					UID:        tlsRoute.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: hostnames,
			Rules:     rules,
		},
	}

	return proxyRoute, nil
}
