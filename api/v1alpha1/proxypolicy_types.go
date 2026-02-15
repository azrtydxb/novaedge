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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PolicyType defines the type of policy
// +kubebuilder:validation:Enum=RateLimit;JWT;IPAllowList;IPDenyList;CORS;SecurityHeaders;DistributedRateLimit;WAF;WASMPlugin;BasicAuth;ForwardAuth;OIDC;MeshAuthorization
type PolicyType string

const (
	// PolicyTypeRateLimit applies rate limiting
	PolicyTypeRateLimit PolicyType = "RateLimit"
	// PolicyTypeJWT applies JWT authentication
	PolicyTypeJWT PolicyType = "JWT"
	// PolicyTypeIPAllowList allows only specific IPs
	PolicyTypeIPAllowList PolicyType = "IPAllowList"
	// PolicyTypeIPDenyList denies specific IPs
	PolicyTypeIPDenyList PolicyType = "IPDenyList"
	// PolicyTypeCORS applies CORS headers
	PolicyTypeCORS PolicyType = "CORS"
	// PolicyTypeSecurityHeaders applies security headers (HSTS, CSP, etc.)
	PolicyTypeSecurityHeaders PolicyType = "SecurityHeaders"
	// PolicyTypeDistributedRateLimit applies distributed rate limiting via Redis
	PolicyTypeDistributedRateLimit PolicyType = "DistributedRateLimit"
	// PolicyTypeWAF applies Web Application Firewall protection
	PolicyTypeWAF PolicyType = "WAF"
	// PolicyTypeWASMPlugin applies a WASM plugin middleware
	PolicyTypeWASMPlugin PolicyType = "WASMPlugin"
	// PolicyTypeBasicAuth applies HTTP Basic Authentication
	PolicyTypeBasicAuth PolicyType = "BasicAuth"
	// PolicyTypeForwardAuth delegates authentication to an external service
	PolicyTypeForwardAuth PolicyType = "ForwardAuth"
	// PolicyTypeOIDC applies OAuth2/OIDC authentication flow
	PolicyTypeOIDC PolicyType = "OIDC"
	// PolicyTypeMeshAuthorization applies identity-based authorization for mesh traffic
	PolicyTypeMeshAuthorization PolicyType = "MeshAuthorization"
)

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	// RequestsPerSecond is the maximum number of requests per second
	// +kubebuilder:validation:Minimum=1
	RequestsPerSecond int32 `json:"requestsPerSecond"`

	// Burst is the maximum burst size
	// +optional
	// +kubebuilder:validation:Minimum=1
	Burst *int32 `json:"burst,omitempty"`

	// Key determines what to rate limit by (e.g., source IP, header value)
	// +optional
	// +kubebuilder:default="source-ip"
	Key string `json:"key,omitempty"`
}

// JWTConfig defines JWT authentication configuration
type JWTConfig struct {
	// Issuer is the expected JWT issuer
	// +kubebuilder:validation:Required
	Issuer string `json:"issuer"`

	// Audience is the expected JWT audience
	// +optional
	Audience []string `json:"audience,omitempty"`

	// JWKSUri is the URL to fetch JWKS for verification
	// +kubebuilder:validation:Required
	JWKSUri string `json:"jwksUri"`

	// HeaderName is the header containing the JWT token
	// +optional
	// +kubebuilder:default="Authorization"
	HeaderName string `json:"headerName,omitempty"`

	// HeaderPrefix is the prefix before the token in the header
	// +optional
	// +kubebuilder:default="Bearer "
	HeaderPrefix string `json:"headerPrefix,omitempty"`

	// AllowedAlgorithms restricts accepted JWT signing algorithms.
	// Supported values: RS256, RS384, RS512, ES256, ES384, ES512, EdDSA.
	// If empty, all supported algorithms are allowed.
	// +optional
	AllowedAlgorithms []string `json:"allowedAlgorithms,omitempty"`

	// VaultSecretRef optionally references credentials stored in HashiCorp Vault
	// +optional
	VaultSecretRef *VaultSecretReference `json:"vaultSecretRef,omitempty"`
}

// VaultSecretReference references a secret stored in HashiCorp Vault
type VaultSecretReference struct {
	// Path is the Vault secret path (e.g., "secret/data/myapp/oidc")
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// Key is the specific key within the secret to use
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// Engine specifies the Vault secrets engine type
	// +optional
	// +kubebuilder:default="kv-v2"
	// +kubebuilder:validation:Enum=kv-v1;kv-v2
	Engine string `json:"engine,omitempty"`

	// RefreshInterval specifies how often to refresh the secret from Vault
	// +optional
	// +kubebuilder:default="5m"
	RefreshInterval string `json:"refreshInterval,omitempty"`
}

// IPListConfig defines IP allow/deny list configuration
type IPListConfig struct {
	// CIDRs is a list of CIDR blocks
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	CIDRs []string `json:"cidrs"`

	// SourceHeader specifies an HTTP header to extract the client IP from
	// (e.g., X-Forwarded-For, X-Real-IP)
	// +optional
	SourceHeader *string `json:"sourceHeader,omitempty"`
}

// CORSConfig defines CORS policy configuration
type CORSConfig struct {
	// AllowOrigins is a list of allowed origins
	// +optional
	AllowOrigins []string `json:"allowOrigins,omitempty"`

	// AllowMethods is a list of allowed HTTP methods
	// +optional
	AllowMethods []string `json:"allowMethods,omitempty"`

	// AllowHeaders is a list of allowed headers
	// +optional
	AllowHeaders []string `json:"allowHeaders,omitempty"`

	// ExposeHeaders is a list of headers to expose
	// +optional
	ExposeHeaders []string `json:"exposeHeaders,omitempty"`

	// MaxAge is how long the response to a preflight request can be cached
	// +optional
	MaxAge *metav1.Duration `json:"maxAge,omitempty"`

	// AllowCredentials indicates whether credentials are allowed
	// +optional
	AllowCredentials bool `json:"allowCredentials,omitempty"`
}

// SecurityHeadersConfig defines security headers policy configuration
type SecurityHeadersConfig struct {
	// HSTS configures HTTP Strict Transport Security
	// +optional
	HSTS *HSTSConfig `json:"hsts,omitempty"`

	// ContentSecurityPolicy sets the Content-Security-Policy header
	// +optional
	ContentSecurityPolicy string `json:"contentSecurityPolicy,omitempty"`

	// XFrameOptions sets the X-Frame-Options header (DENY, SAMEORIGIN, ALLOW-FROM uri)
	// +optional
	// +kubebuilder:validation:Enum=DENY;SAMEORIGIN
	XFrameOptions string `json:"xFrameOptions,omitempty"`

	// XContentTypeOptions enables X-Content-Type-Options: nosniff
	// +optional
	// +kubebuilder:default=true
	XContentTypeOptions bool `json:"xContentTypeOptions,omitempty"`

	// XXSSProtection sets the X-XSS-Protection header (e.g., "1; mode=block")
	// +optional
	XXSSProtection string `json:"xXssProtection,omitempty"`

	// ReferrerPolicy sets the Referrer-Policy header
	// +optional
	// +kubebuilder:validation:Enum=no-referrer;no-referrer-when-downgrade;origin;origin-when-cross-origin;same-origin;strict-origin;strict-origin-when-cross-origin;unsafe-url
	ReferrerPolicy string `json:"referrerPolicy,omitempty"`

	// PermissionsPolicy sets the Permissions-Policy header (replaces Feature-Policy)
	// +optional
	PermissionsPolicy string `json:"permissionsPolicy,omitempty"`

	// CrossOriginEmbedderPolicy sets the Cross-Origin-Embedder-Policy header
	// +optional
	// +kubebuilder:validation:Enum=unsafe-none;require-corp;credentialless
	CrossOriginEmbedderPolicy string `json:"crossOriginEmbedderPolicy,omitempty"`

	// CrossOriginOpenerPolicy sets the Cross-Origin-Opener-Policy header
	// +optional
	// +kubebuilder:validation:Enum=unsafe-none;same-origin-allow-popups;same-origin
	CrossOriginOpenerPolicy string `json:"crossOriginOpenerPolicy,omitempty"`

	// CrossOriginResourcePolicy sets the Cross-Origin-Resource-Policy header
	// +optional
	// +kubebuilder:validation:Enum=same-site;same-origin;cross-origin
	CrossOriginResourcePolicy string `json:"crossOriginResourcePolicy,omitempty"`
}

// HSTSConfig defines HSTS (HTTP Strict Transport Security) settings
type HSTSConfig struct {
	// Enabled enables HSTS
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// MaxAge is the time in seconds that the browser should remember this site is HTTPS only
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=31536000
	MaxAge int64 `json:"maxAge,omitempty"`

	// IncludeSubDomains applies HSTS to all subdomains
	// +optional
	IncludeSubDomains bool `json:"includeSubDomains,omitempty"`

	// Preload adds the site to the browser HSTS preload list
	// +optional
	Preload bool `json:"preload,omitempty"`
}

// DistributedRateLimitConfig defines distributed rate limiting via Redis
type DistributedRateLimitConfig struct {
	// RequestsPerSecond is the maximum number of requests per second
	// +kubebuilder:validation:Minimum=1
	RequestsPerSecond int32 `json:"requestsPerSecond"`

	// Burst is the maximum burst size
	// +optional
	// +kubebuilder:validation:Minimum=1
	Burst *int32 `json:"burst,omitempty"`

	// Algorithm is the rate limiting algorithm
	// +optional
	// +kubebuilder:validation:Enum=fixed-window;sliding-window;token-bucket
	// +kubebuilder:default="sliding-window"
	Algorithm string `json:"algorithm,omitempty"`

	// Key determines what to rate limit by
	// +optional
	// +kubebuilder:default="source-ip"
	Key string `json:"key,omitempty"`

	// RedisRef is the Redis connection configuration
	// +kubebuilder:validation:Required
	RedisRef RedisConfig `json:"redisRef"`
}

// RedisConfig defines Redis connection settings
type RedisConfig struct {
	// Address is the Redis server address (host:port)
	// +kubebuilder:validation:Required
	Address string `json:"address"`

	// Password references a secret containing the Redis password
	// +optional
	Password *SecretKeyReference `json:"password,omitempty"`

	// TLS enables TLS for the Redis connection
	// +optional
	TLS bool `json:"tls,omitempty"`

	// Database is the Redis database number
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	Database int32 `json:"database,omitempty"`

	// ClusterMode enables Redis cluster support
	// +optional
	ClusterMode bool `json:"clusterMode,omitempty"`
}

// SecretKeyReference references a key in a Secret
type SecretKeyReference struct {
	// Name is the name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key is the key in the Secret
	// +optional
	// +kubebuilder:default="password"
	Key string `json:"key,omitempty"`
}

// WAFConfig defines Web Application Firewall configuration
type WAFConfig struct {
	// Enabled enables WAF protection
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Mode is the WAF operating mode
	// +optional
	// +kubebuilder:validation:Enum=detection;prevention
	// +kubebuilder:default="prevention"
	Mode string `json:"mode,omitempty"`

	// ParanoiaLevel sets the OWASP CRS paranoia level (1-4)
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4
	// +kubebuilder:default=1
	ParanoiaLevel int32 `json:"paranoiaLevel,omitempty"`

	// AnomalyThreshold is the cumulative anomaly score threshold for blocking
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=5
	AnomalyThreshold int32 `json:"anomalyThreshold,omitempty"`

	// RulesConfigMap references a ConfigMap containing WAF rules
	// +optional
	RulesConfigMap *LocalObjectReference `json:"rulesConfigMap,omitempty"`

	// RuleExclusions is a list of rule IDs to exclude
	// +optional
	RuleExclusions []string `json:"ruleExclusions,omitempty"`

	// CustomRules are inline SecLang WAF rules applied after built-in rules
	// +optional
	CustomRules []string `json:"customRules,omitempty"`

	// MaxBodySize limits request body inspection in bytes. 0 disables body inspection.
	// +optional
	// +kubebuilder:default=131072
	MaxBodySize int64 `json:"maxBodySize,omitempty"`

	// ResponseBodyInspection enables response body scanning for data leakage
	// +optional
	ResponseBodyInspection bool `json:"responseBodyInspection,omitempty"`

	// MaxResponseBodySize limits response body inspection in bytes
	// +optional
	// +kubebuilder:default=131072
	MaxResponseBodySize int64 `json:"maxResponseBodySize,omitempty"`
}

// BasicAuthPolicyConfig defines HTTP Basic Authentication configuration
type BasicAuthPolicyConfig struct {
	// Realm is the authentication realm name shown in the browser dialog
	// +optional
	// +kubebuilder:default="Restricted"
	Realm string `json:"realm,omitempty"`

	// SecretRef references a Kubernetes Secret containing htpasswd-formatted credentials
	// The Secret must have a key "htpasswd" with lines in the format: username:password_hash
	// Supported hash formats: bcrypt, SHA-256 ({SHA256}), MD5 (apr1)
	// +kubebuilder:validation:Required
	SecretRef LocalObjectReference `json:"secretRef"`

	// StripAuth removes the Authorization header before forwarding to the backend
	// +optional
	// +kubebuilder:default=true
	StripAuth bool `json:"stripAuth,omitempty"`
}

// ForwardAuthPolicyConfig defines external auth delegation configuration
type ForwardAuthPolicyConfig struct {
	// Address is the URL of the external auth service
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://`
	Address string `json:"address"`

	// AuthHeaders specifies which headers from the original request to forward to the auth service
	// +optional
	AuthHeaders []string `json:"authHeaders,omitempty"`

	// ResponseHeaders specifies which headers from the auth response to copy to the upstream request
	// +optional
	ResponseHeaders []string `json:"responseHeaders,omitempty"`

	// Timeout for the auth subrequest (e.g., "5s", "10s")
	// +optional
	// +kubebuilder:default="5s"
	Timeout string `json:"timeout,omitempty"`

	// CacheTTL specifies how long to cache auth decisions (e.g., "5m", "1h")
	// Empty means no caching
	// +optional
	CacheTTL string `json:"cacheTTL,omitempty"`
}

// OIDCPolicyConfig defines OAuth2/OIDC authentication flow configuration
type OIDCPolicyConfig struct {
	// Provider is the OIDC provider type ("generic" or "keycloak")
	// +optional
	// +kubebuilder:default="generic"
	// +kubebuilder:validation:Enum=generic;keycloak
	Provider string `json:"provider,omitempty"`

	// IssuerURL is the OIDC provider's issuer URL for discovery
	// For Keycloak, this is auto-constructed from Keycloak config if not specified
	// +optional
	IssuerURL string `json:"issuerURL,omitempty"`

	// ClientID is the OAuth2 client ID
	// +kubebuilder:validation:Required
	ClientID string `json:"clientID"`

	// ClientSecretRef references a Secret containing the OAuth2 client secret
	// The Secret must have a key "client-secret"
	// +kubebuilder:validation:Required
	ClientSecretRef LocalObjectReference `json:"clientSecretRef"`

	// RedirectURL is the OAuth2 callback URL (e.g., https://myapp.example.com/oauth2/callback)
	// +kubebuilder:validation:Required
	RedirectURL string `json:"redirectURL"`

	// Scopes is the list of OAuth2 scopes to request
	// +optional
	// +kubebuilder:default={"openid","profile","email"}
	Scopes []string `json:"scopes,omitempty"`

	// SessionSecretRef references a Secret containing the session encryption key
	// The Secret must have a key "session-secret" (32 bytes, base64 encoded)
	// +kubebuilder:validation:Required
	SessionSecretRef LocalObjectReference `json:"sessionSecretRef"`

	// ForwardHeaders specifies which user info claims to forward as headers to the upstream
	// +optional
	ForwardHeaders []string `json:"forwardHeaders,omitempty"`

	// Keycloak contains Keycloak-specific configuration
	// +optional
	Keycloak *KeycloakPolicyConfig `json:"keycloak,omitempty"`

	// Authorization contains role-based access control configuration
	// +optional
	Authorization *AuthorizationPolicyConfig `json:"authorization,omitempty"`
}

// KeycloakPolicyConfig defines Keycloak-specific configuration
type KeycloakPolicyConfig struct {
	// ServerURL is the base Keycloak server URL (e.g., https://keycloak.example.com)
	// +kubebuilder:validation:Required
	ServerURL string `json:"serverURL"`

	// Realm is the Keycloak realm name
	// +kubebuilder:validation:Required
	Realm string `json:"realm"`

	// RoleClaim is the JWT claim containing realm roles
	// +optional
	// +kubebuilder:default="realm_access.roles"
	RoleClaim string `json:"roleClaim,omitempty"`

	// GroupClaim is the JWT claim containing groups
	// +optional
	// +kubebuilder:default="groups"
	GroupClaim string `json:"groupClaim,omitempty"`
}

// AuthorizationPolicyConfig defines role-based access control for authenticated users
type AuthorizationPolicyConfig struct {
	// RequiredRoles specifies roles the user must have
	// +optional
	RequiredRoles []string `json:"requiredRoles,omitempty"`

	// RequiredGroups specifies groups the user must belong to
	// +optional
	RequiredGroups []string `json:"requiredGroups,omitempty"`

	// Mode specifies whether all requirements must be met ("all") or any one ("any")
	// +optional
	// +kubebuilder:default="any"
	// +kubebuilder:validation:Enum=any;all
	Mode string `json:"mode,omitempty"`
}

// MeshAuthorizationConfig defines identity-based authorization for mesh traffic
type MeshAuthorizationConfig struct {
	// Rules defines authorization rules
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Rules []MeshAuthorizationRule `json:"rules"`

	// Action is the policy action: ALLOW or DENY
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ALLOW;DENY
	Action string `json:"action"`
}

// MeshAuthorizationRule defines a single authorization rule
type MeshAuthorizationRule struct {
	// From specifies allowed source identities
	// +optional
	From []MeshSource `json:"from,omitempty"`

	// To specifies allowed destination traffic properties
	// +optional
	To []MeshDestination `json:"to,omitempty"`
}

// MeshSource identifies allowed source workloads by identity
type MeshSource struct {
	// Namespaces restricts to source workloads in these namespaces
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// ServiceAccounts restricts to source workloads with these service accounts
	// +optional
	ServiceAccounts []string `json:"serviceAccounts,omitempty"`

	// SpiffeIDs restricts to sources matching these SPIFFE ID patterns (glob)
	// +optional
	SpiffeIDs []string `json:"spiffeIds,omitempty"`
}

// MeshDestination matches destination traffic properties for L7 authorization
type MeshDestination struct {
	// Methods restricts to these HTTP methods
	// +optional
	Methods []string `json:"methods,omitempty"`

	// Paths restricts to these URL path patterns (glob)
	// +optional
	Paths []string `json:"paths,omitempty"`
}

// TargetRef identifies the resource(s) this policy applies to
type TargetRef struct {
	// Kind is the kind of resource (e.g., ProxyGateway, ProxyRoute, Service)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ProxyGateway;ProxyRoute;ProxyBackend;Service
	Kind string `json:"kind"`

	// Name is the name of the resource
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the resource (defaults to policy namespace)
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}

// WASMPluginConfig defines WASM plugin configuration
type WASMPluginConfig struct {
	// Source is a reference to the WASM binary (ConfigMap name or URL)
	// +kubebuilder:validation:Required
	Source string `json:"source"`

	// ConfigRef is an optional reference to a ConfigMap containing plugin configuration
	// +optional
	ConfigRef *LocalObjectReference `json:"configRef,omitempty"`

	// Config is an inline key-value configuration map passed to the WASM plugin
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// Phase determines when the plugin executes: request, response, or both
	// +optional
	// +kubebuilder:default="request"
	// +kubebuilder:validation:Enum=request;response;both
	Phase string `json:"phase,omitempty"`

	// Priority determines execution order within the middleware pipeline (lower = earlier)
	// +optional
	// +kubebuilder:default=100
	Priority int `json:"priority,omitempty"`
}

// ProxyPolicySpec defines the desired state of ProxyPolicy
type ProxyPolicySpec struct {
	// Type is the type of policy
	// +kubebuilder:validation:Required
	Type PolicyType `json:"type"`

	// TargetRef identifies the resource this policy applies to
	// +kubebuilder:validation:Required
	TargetRef TargetRef `json:"targetRef"`

	// RateLimit configuration (for RateLimit type)
	// +optional
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`

	// JWT configuration (for JWT type)
	// +optional
	JWT *JWTConfig `json:"jwt,omitempty"`

	// IPList configuration (for IPAllowList/IPDenyList types)
	// +optional
	IPList *IPListConfig `json:"ipList,omitempty"`

	// CORS configuration (for CORS type)
	// +optional
	CORS *CORSConfig `json:"cors,omitempty"`

	// SecurityHeaders configuration (for SecurityHeaders type)
	// +optional
	SecurityHeaders *SecurityHeadersConfig `json:"securityHeaders,omitempty"`

	// DistributedRateLimit configuration (for DistributedRateLimit type)
	// +optional
	DistributedRateLimit *DistributedRateLimitConfig `json:"distributedRateLimit,omitempty"`

	// WAF configuration (for WAF type)
	// +optional
	WAF *WAFConfig `json:"waf,omitempty"`

	// WASMPlugin configuration (for WASMPlugin type)
	// +optional
	WASMPlugin *WASMPluginConfig `json:"wasmPlugin,omitempty"`

	// BasicAuth configuration (for BasicAuth type)
	// +optional
	BasicAuth *BasicAuthPolicyConfig `json:"basicAuth,omitempty"`

	// ForwardAuth configuration (for ForwardAuth type)
	// +optional
	ForwardAuth *ForwardAuthPolicyConfig `json:"forwardAuth,omitempty"`

	// OIDC configuration (for OIDC type)
	// +optional
	OIDC *OIDCPolicyConfig `json:"oidc,omitempty"`

	// MeshAuthorization configuration (for MeshAuthorization type)
	// +optional
	MeshAuthorization *MeshAuthorizationConfig `json:"meshAuthorization,omitempty"`
}

// ProxyPolicyStatus defines the observed state of ProxyPolicy
type ProxyPolicyStatus struct {
	// Conditions represent the latest available observations of the policy's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Target Kind",type=string,JSONPath=`.spec.targetRef.kind`
// +kubebuilder:printcolumn:name="Target Name",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyPolicy defines authentication, rate-limiting, and other policies
type ProxyPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyPolicySpec   `json:"spec,omitempty"`
	Status ProxyPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyPolicyList contains a list of ProxyPolicy
type ProxyPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyPolicy{}, &ProxyPolicyList{})
}
