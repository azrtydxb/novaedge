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

package router

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/policy"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// policyMiddleware wraps a policy handler
type policyMiddleware struct {
	name    string
	handler func(http.Handler) http.Handler
}

// createPolicyMiddleware creates policy middleware for a route
func (r *Router) createPolicyMiddleware(route *pb.Route, snapshot *config.Snapshot) []policyMiddleware {
	var middlewares []policyMiddleware

	// Find policies attached to this route
	routeRef := fmt.Sprintf("%s/%s", route.Namespace, route.Name)

	for _, policyProto := range snapshot.Policies {
		// Check if policy targets this route
		if policyProto.TargetRef == nil {
			continue
		}

		targetRef := fmt.Sprintf("%s/%s", policyProto.TargetRef.Namespace, policyProto.TargetRef.Name)
		if targetRef != routeRef {
			continue
		}

		// Create middleware based on policy type
		switch policyProto.Type {
		case pb.PolicyType_RATE_LIMIT:
			if policyProto.RateLimit != nil {
				limiter := policy.NewRateLimiter(policyProto.RateLimit)
				middlewares = append(middlewares, policyMiddleware{
					name:    fmt.Sprintf("rate-limit-%s", policyProto.Name),
					handler: policy.HandleRateLimit(limiter),
				})
			}

		case pb.PolicyType_CORS:
			if policyProto.Cors != nil {
				cors := policy.NewCORS(policyProto.Cors)
				middlewares = append(middlewares, policyMiddleware{
					name:    fmt.Sprintf("cors-%s", policyProto.Name),
					handler: policy.HandleCORS(cors),
				})
			}

		case pb.PolicyType_IP_ALLOW_LIST:
			if policyProto.IpList != nil {
				filter, err := policy.NewIPAllowListFilter(policyProto.IpList.Cidrs)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("ip-allow-%s", policyProto.Name),
						handler: policy.HandleIPFilter(filter),
					})
				}
			}

		case pb.PolicyType_IP_DENY_LIST:
			if policyProto.IpList != nil {
				filter, err := policy.NewIPDenyListFilter(policyProto.IpList.Cidrs)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("ip-deny-%s", policyProto.Name),
						handler: policy.HandleIPFilter(filter),
					})
				}
			}

		case pb.PolicyType_JWT:
			if policyProto.Jwt != nil {
				validator, err := policy.NewJWTValidator(policyProto.Jwt)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("jwt-%s", policyProto.Name),
						handler: policy.HandleJWT(validator),
					})
				} else {
					r.logger.Error("Failed to create JWT validator",
						zap.String("policy", policyProto.Name),
						zap.Error(err),
					)
				}
			}
		}
	}

	return middlewares
}
