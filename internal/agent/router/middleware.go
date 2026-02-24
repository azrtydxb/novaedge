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
	"context"
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
func (r *Router) createPolicyMiddleware(ctx context.Context, route *pb.Route, snapshot *config.Snapshot) []policyMiddleware {
	var middlewares []policyMiddleware

	// Find policies attached to this route
	routeRef := fmt.Sprintf("%s/%s", route.Namespace, route.Name)

	for _, policyProto := range snapshot.Policies {
		// Check if policy targets this route
		if policyProto.TargetRef == nil {
			continue
		}

		// Match by Kind: only apply policies that target ProxyRoute resources.
		if policyProto.TargetRef.Kind != "ProxyRoute" {
			continue
		}

		targetRef := fmt.Sprintf("%s/%s", policyProto.TargetRef.Namespace, policyProto.TargetRef.Name)
		if targetRef != routeRef {
			continue
		}

		// Create middleware based on policy type
		mw := r.buildPolicyHandler(ctx, policyProto)
		if mw != nil {
			middlewares = append(middlewares, *mw)
		}
	}

	return middlewares
}

// buildPolicyHandler creates a single policyMiddleware for the given policy proto.
// Returns nil if the policy type has no configuration or is unrecognized.
func (r *Router) buildPolicyHandler(ctx context.Context, p *pb.Policy) *policyMiddleware {
	switch p.Type {
	case pb.PolicyType_RATE_LIMIT:
		return r.buildRateLimitPolicy(p)
	case pb.PolicyType_CORS:
		return r.buildCORSPolicy(p)
	case pb.PolicyType_IP_ALLOW_LIST:
		return r.buildIPAllowListPolicy(p)
	case pb.PolicyType_IP_DENY_LIST:
		return r.buildIPDenyListPolicy(p)
	case pb.PolicyType_JWT:
		return r.buildJWTPolicy(ctx, p)
	case pb.PolicyType_SECURITY_HEADERS:
		return r.buildSecurityHeadersPolicy(p)
	case pb.PolicyType_DISTRIBUTED_RATE_LIMIT:
		return r.buildDistributedRateLimitPolicy(p)
	case pb.PolicyType_WAF:
		return r.buildWAFPolicy(p)
	case pb.PolicyType_BASIC_AUTH:
		return r.buildBasicAuthPolicy(p)
	case pb.PolicyType_FORWARD_AUTH:
		return r.buildForwardAuthPolicy(ctx, p)
	case pb.PolicyType_OIDC:
		return r.buildOIDCPolicy(ctx, p)
	default:
		return nil
	}
}

func (r *Router) buildRateLimitPolicy(p *pb.Policy) *policyMiddleware {
	if p.RateLimit == nil {
		return nil
	}
	limiter := policy.NewRateLimiter(p.RateLimit)
	// Attach eBPF rate limiter for per-source-IP fast-path if available.
	// The policy.RateLimiter will use BPF maps for L3/L4 rate limiting and
	// fall back to Go-side token buckets for L7 policies (per-header, etc).
	if r.ebpfRateLimiter != nil {
		limiter.SetEBPFRateLimiter(r.ebpfRateLimiter)
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("rate-limit-%s", p.Name),
		handler: policy.HandleRateLimit(limiter),
	}
}

func (r *Router) buildCORSPolicy(p *pb.Policy) *policyMiddleware {
	if p.Cors == nil {
		return nil
	}
	cors := policy.NewCORS(p.Cors)
	return &policyMiddleware{
		name:    fmt.Sprintf("cors-%s", p.Name),
		handler: policy.HandleCORS(cors),
	}
}

func (r *Router) buildIPAllowListPolicy(p *pb.Policy) *policyMiddleware {
	if p.IpList == nil {
		return nil
	}
	filter, err := policy.NewIPAllowListFilter(p.IpList.Cidrs)
	if err != nil {
		r.logger.Error("Failed to create IP allow list filter, failing closed",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("ip-allow-%s-fail-closed", p.Name),
			handler: failClosedMiddleware("IP allow list", p.Name, r.logger),
		}
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("ip-allow-%s", p.Name),
		handler: policy.HandleIPFilter(filter),
	}
}

func (r *Router) buildIPDenyListPolicy(p *pb.Policy) *policyMiddleware {
	if p.IpList == nil {
		return nil
	}
	filter, err := policy.NewIPDenyListFilter(p.IpList.Cidrs)
	if err != nil {
		r.logger.Error("Failed to create IP deny list filter, failing closed",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("ip-deny-%s-fail-closed", p.Name),
			handler: failClosedMiddleware("IP deny list", p.Name, r.logger),
		}
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("ip-deny-%s", p.Name),
		handler: policy.HandleIPFilter(filter),
	}
}

func (r *Router) buildJWTPolicy(ctx context.Context, p *pb.Policy) *policyMiddleware {
	if p.Jwt == nil {
		return nil
	}
	validator, err := policy.NewJWTValidator(ctx, p.Jwt)
	if err != nil {
		r.logger.Error("Failed to create JWT validator",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return nil
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("jwt-%s", p.Name),
		handler: policy.HandleJWT(validator),
	}
}

func (r *Router) buildSecurityHeadersPolicy(p *pb.Policy) *policyMiddleware {
	if p.SecurityHeaders == nil {
		return nil
	}
	sh := policy.NewSecurityHeaders(p.SecurityHeaders)
	return &policyMiddleware{
		name:    fmt.Sprintf("security-headers-%s", p.Name),
		handler: policy.HandleSecurityHeaders(sh),
	}
}

func (r *Router) buildDistributedRateLimitPolicy(p *pb.Policy) *policyMiddleware {
	if p.DistributedRateLimit == nil {
		return nil
	}
	redisClient, err := policy.NewRedisClient(p.DistributedRateLimit.Redis, "", r.logger)
	if err != nil {
		r.logger.Error("Failed to create Redis client for distributed rate limit",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("distributed-ratelimit-%s", p.Name),
			handler: failClosedMiddleware("distributed rate limit", p.Name, r.logger),
		}
	}
	limiter := policy.NewDistributedRateLimiter(p.DistributedRateLimit, redisClient, r.logger)
	return &policyMiddleware{
		name:    fmt.Sprintf("distributed-ratelimit-%s", p.Name),
		handler: policy.HandleDistributedRateLimit(limiter),
	}
}

func (r *Router) buildWAFPolicy(p *pb.Policy) *policyMiddleware {
	if p.Waf == nil {
		return nil
	}
	engine, err := policy.NewWAFEngine(p.Waf, r.logger)
	if err != nil {
		r.logger.Error("Failed to create WAF engine",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("waf-%s", p.Name),
			handler: failClosedMiddleware("WAF", p.Name, r.logger),
		}
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("waf-%s", p.Name),
		handler: policy.HandleWAF(engine),
	}
}

func (r *Router) buildBasicAuthPolicy(p *pb.Policy) *policyMiddleware {
	if p.BasicAuth == nil {
		return nil
	}
	validator, err := policy.NewBasicAuthValidator(p.BasicAuth)
	if err != nil {
		r.logger.Error("Failed to create Basic Auth validator, failing closed",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("basic-auth-%s-fail-closed", p.Name),
			handler: failClosedMiddleware("Basic Auth", p.Name, r.logger),
		}
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("basic-auth-%s", p.Name),
		handler: policy.HandleBasicAuth(validator),
	}
}

func (r *Router) buildForwardAuthPolicy(ctx context.Context, p *pb.Policy) *policyMiddleware {
	if p.ForwardAuth == nil {
		return nil
	}
	handler := policy.NewForwardAuthHandler(ctx, p.ForwardAuth, r.logger)
	return &policyMiddleware{
		name:    fmt.Sprintf("forward-auth-%s", p.Name),
		handler: policy.HandleForwardAuth(handler),
	}
}

func (r *Router) buildOIDCPolicy(ctx context.Context, p *pb.Policy) *policyMiddleware {
	if p.Oidc == nil {
		return nil
	}
	handler, err := policy.NewOIDCHandler(ctx, p.Oidc, r.logger)
	if err != nil {
		r.logger.Error("Failed to create OIDC handler, failing closed",
			zap.String("policy", p.Name),
			zap.Error(err),
		)
		return &policyMiddleware{
			name:    fmt.Sprintf("oidc-%s-fail-closed", p.Name),
			handler: failClosedMiddleware("OIDC", p.Name, r.logger),
		}
	}
	return &policyMiddleware{
		name:    fmt.Sprintf("oidc-%s", p.Name),
		handler: policy.HandleOIDC(handler),
	}
}

// failClosedMiddleware returns a middleware that rejects all requests with 503 Service Unavailable.
// This is used when a security policy (WAF, rate limiter, auth) fails to initialize, ensuring
// requests are not silently allowed through without protection.
func failClosedMiddleware(policyType, policyName string, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.Warn("Request rejected: security policy unavailable",
				zap.String("policy_type", policyType),
				zap.String("policy", policyName),
				zap.String("path", r.URL.Path),
			)
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		})
	}
}
