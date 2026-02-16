// TypeScript types matching Kubernetes CRD structure

// Common Kubernetes metadata
export interface ResourceMetadata {
  name: string
  namespace?: string
  creationTimestamp?: string
  resourceVersion?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
}

// Common condition type
export interface Condition {
  type: string
  status: 'True' | 'False' | 'Unknown'
  lastTransitionTime?: string
  reason?: string
  message?: string
}

// Gateway types
export interface Gateway {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: GatewaySpec
  status?: GatewayStatus
}

export interface GatewaySpec {
  listeners?: Listener[]
  vipRef?: ObjectRef
  tracing?: Tracing
  accessLog?: AccessLog
}

export interface GatewayStatus {
  ready?: boolean
  conditions?: Condition[]
}

export interface Listener {
  name: string
  port: number
  protocol: 'HTTP' | 'HTTPS' | 'TCP' | 'TLS'
  tls?: TLS
  hostnames?: string[]
  maxRequestBodySize?: number
}

export interface TLS {
  mode?: 'Terminate' | 'Passthrough'
  certificateRefs?: ObjectRef[]
  certFile?: string
  keyFile?: string
  minVersion?: string
  cipherSuites?: string[]
}

export interface ObjectRef {
  name: string
  namespace?: string
  group?: string
  kind?: string
}

export interface Tracing {
  enabled?: boolean
  samplingRate?: number
  requestIdHeader?: string
}

export interface AccessLog {
  enabled?: boolean
  format?: 'json' | 'common' | 'combined'
  path?: string
}

// Route types
export interface Route {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: RouteSpec
  status?: RouteStatus
}

export interface RouteSpec {
  parentRefs?: ObjectRef[]
  hostnames?: string[]
  rules?: RouteRule[]
}

export interface RouteRule {
  matches?: RouteMatch[]
  backendRefs?: BackendRef[]
  filters?: Filter[]
  timeout?: string
}

export interface RouteStatus {
  conditions?: Condition[]
}

export interface RouteMatch {
  path?: PathMatch
  headers?: HeaderMatch[]
  method?: string
}

export interface PathMatch {
  type: 'Exact' | 'PathPrefix' | 'RegularExpression'
  value: string
}

export interface HeaderMatch {
  name: string
  value: string
  type?: 'Exact' | 'RegularExpression'
}

export interface BackendRef {
  name: string
  namespace?: string
  port?: number
  weight?: number
}

export interface Filter {
  type: 'RequestHeaderModifier' | 'ResponseHeaderModifier' | 'URLRewrite' | 'RequestRedirect'
  requestHeaderModifier?: HeaderModifier
  responseHeaderModifier?: HeaderModifier
  urlRewrite?: URLRewrite
  requestRedirect?: Redirect
}

export interface HeaderModifier {
  set?: HeaderValue[]
  add?: HeaderValue[]
  remove?: string[]
}

export interface HeaderValue {
  name: string
  value: string
}

export interface URLRewrite {
  hostname?: string
  path?: PathModifier
}

export interface PathModifier {
  type: 'ReplaceFullPath' | 'ReplacePrefixMatch'
  replaceFullPath?: string
  replacePrefixMatch?: string
}

export interface Redirect {
  scheme?: string
  hostname?: string
  port?: number
  path?: PathModifier
  statusCode?: number
}

// Backend types
export interface Backend {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: BackendSpec
  status?: BackendStatus
}

export interface BackendSpec {
  endpoints?: Endpoint[]
  loadBalancer?: LoadBalancerConfig
  healthCheck?: HealthCheck
  circuitBreaker?: CircuitBreaker
  connectionPool?: ConnectionPool
  tls?: BackendTLS
}

export interface BackendStatus {
  healthyEndpoints?: number
  totalEndpoints?: number
  conditions?: Condition[]
}

export interface Endpoint {
  address: string
  port?: number
  weight?: number
  healthy?: boolean
}

export interface LoadBalancerConfig {
  algorithm?: 'RoundRobin' | 'P2C' | 'EWMA' | 'RingHash' | 'Maglev'
}

export interface HealthCheck {
  enabled?: boolean
  protocol?: 'HTTP' | 'TCP'
  path?: string
  port?: number
  interval?: string
  timeout?: string
  healthyThreshold?: number
  unhealthyThreshold?: number
}

export interface CircuitBreaker {
  maxConnections?: number
  maxPendingRequests?: number
  maxRequests?: number
  maxRetries?: number
  consecutiveErrors?: number
  interval?: string
  baseEjectionTime?: string
  maxEjectionPercent?: number
}

export interface ConnectionPool {
  maxConnections?: number
  maxIdleConnections?: number
  idleTimeout?: string
  maxConnectionLifetime?: string
}

export interface BackendTLS {
  enabled?: boolean
  insecureSkipVerify?: boolean
  caFile?: string
  certFile?: string
  keyFile?: string
  serverName?: string
}

// VIP types
export interface VIP {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: VIPSpec
  status?: VIPStatus
}

export interface VIPSpec {
  address?: string
  mode?: 'L2' | 'BGP' | 'OSPF'
  interface?: string
  gatewayRef?: ObjectRef
  bgp?: BGPConfig
  ospf?: OSPFConfig
}

export interface VIPStatus {
  bound?: boolean
  node?: string
  conditions?: Condition[]
}

export interface BGPConfig {
  localAS?: number
  routerID?: string
  peerAS?: number
  peerIP?: string
  holdTime?: number
  keepaliveTime?: number
}

export interface OSPFConfig {
  routerID?: string
  area?: string
  interface?: string
}

// Policy types
export interface Policy {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: PolicySpec
  status?: PolicyStatus
}

export interface PolicySpec {
  targetRef?: ObjectRef
  rateLimit?: RateLimitConfig
  cors?: CORSConfig
  ipFilter?: IPFilterConfig
  jwt?: JWTConfig
}

export interface PolicyStatus {
  conditions?: Condition[]
}

export interface RateLimitConfig {
  requestsPerSecond?: number
  burst?: number
  key?: string
}

export interface CORSConfig {
  allowOrigins?: string[]
  allowMethods?: string[]
  allowHeaders?: string[]
  exposeHeaders?: string[]
  maxAge?: number
  allowCredentials?: boolean
}

export interface IPFilterConfig {
  allowList?: string[]
  denyList?: string[]
}

export interface JWTConfig {
  issuer?: string
  audience?: string[]
  jwksUri?: string
  secretKey?: string
}

// Agent types
export interface AgentInfo {
  podName?: string
  name?: string
  namespace?: string
  nodeName?: string
  podIP?: string
  phase?: string
  ready?: boolean
  startTime?: string
  version?: string
  configVersion?: string
  metrics?: AgentMetrics
}

export interface AgentMetrics {
  activeConnections?: number
  requestsPerSecond?: number
  memoryUsage?: number
  cpuUsage?: number
}

// Worker metrics for individual agents
export interface WorkerMetrics {
  instance: string
  cpuUsage: number      // CPU usage percentage (0-100)
  memoryUsage: number   // Memory usage in bytes
  memoryLimit: number   // Memory limit in bytes (if set)
  goroutines: number    // Number of goroutines
  uptime: number        // Uptime in seconds
  requestsRate: number  // Requests per second for this worker
}

// Dashboard metrics types
export interface DashboardMetrics {
  requestsPerSecond?: number
  requestRate?: number
  activeConnections?: number
  errorRate?: number
  avgLatency?: number
  avgLatencyMs?: number
  bandwidthIn?: number
  bandwidthOut?: number
  vipFailovers?: number
  healthyAgents?: number
  totalAgents?: number
  // Resource metrics (totals across all workers)
  totalCpuUsage?: number      // Total CPU usage percentage
  totalMemoryUsage?: number   // Total memory usage in bytes
  totalGoroutines?: number    // Total goroutines across all workers
  // Per-worker metrics
  workers?: WorkerMetrics[]
  timestamp?: number
  requestRateHistory?: MetricDataPoint[]
  latencyHistory?: MetricDataPoint[]
}

export interface MetricDataPoint {
  timestamp: string
  value: number
}

// Config types
export interface Config {
  gateways?: Gateway[]
  routes?: Route[]
  backends?: Backend[]
  vips?: VIP[]
  policies?: Policy[]
}

// Import/Export types
export interface ImportResult {
  created?: ResourceRef[]
  updated?: ResourceRef[]
  skipped?: ResourceRef[]
  errors?: ImportError[]
  dryRun?: boolean
}

export interface ResourceRef {
  kind: string
  name: string
  namespace?: string
}

export interface ImportError {
  resource: ResourceRef
  error: string
}

export interface ValidationResult {
  valid: boolean
  errors?: ValidationError[]
}

export interface ValidationError {
  field: string
  message: string
}

// API Mode types
export interface ModeInfo {
  mode: 'kubernetes' | 'standalone'
  readOnly: boolean
}

// History types
export interface HistoryEntry {
  id: string
  timestamp: string
  type: 'create' | 'update' | 'delete'
  resourceType: string
  resourceName: string
  namespace: string
  snapshot?: string
}

// Certificate types
export interface Certificate {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: CertificateSpec
  status?: CertificateStatus
}

export interface CertificateSpec {
  domains: string[]
  issuer: string
  secretName?: string
  renewBefore?: string
  acme?: {
    server: string
    email: string
    challenge: string
    dnsProvider?: string
  }
}

export interface CertificateStatus {
  ready: boolean
  notAfter?: string
  notBefore?: string
  serialNumber?: string
  issuer?: string
  renewalTime?: string
  conditions?: Condition[]
}

// IPPool types
export interface IPPool {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: IPPoolSpec
  status?: IPPoolStatus
}

export interface IPPoolSpec {
  cidrs?: string[]
  addresses?: string[]
  protocol?: string
  autoAssign?: boolean
}

export interface IPPoolStatus {
  allocated: number
  total: number
  used?: string[]
  conditions?: Condition[]
}

// Generic CRD model (for Cluster, Federation, RemoteCluster)
export interface GenericResource {
  apiVersion?: string
  kind?: string
  metadata?: ResourceMetadata
  spec?: Record<string, unknown>
  status?: Record<string, unknown>
}

// Auth types
export interface AuthSession {
  authenticated: boolean
  authEnabled: boolean
  oidcEnabled: boolean
}

export interface LoginResult {
  success: boolean
}

// Traces
export interface Trace {
  traceID: string
  spans: Span[]
  services: string[]
  operationName?: string
  duration?: number
  startTime?: number
}

export interface Span {
  traceID: string
  spanID: string
  operationName: string
  serviceName: string
  duration: number
  startTime: number
  tags?: Record<string, string>
  logs?: SpanLog[]
  references?: SpanReference[]
}

export interface SpanLog {
  timestamp: number
  fields: Record<string, string>
}

export interface SpanReference {
  refType: string
  traceID: string
  spanID: string
}

// Logs
export interface LogEntry {
  timestamp: string
  level: string
  message: string
  pod?: string
}

// Events
export interface KubeEvent {
  timestamp: string
  type: string
  reason: string
  message: string
  involvedObject: {
    name: string
    kind: string
    namespace?: string
  }
}

// WAF
export interface WAFSummary {
  totalRequests: number
  blockedRequests: number
  loggedRequests: number
  topRules: WAFRuleHit[]
}

export interface WAFRuleHit {
  ruleId: string
  ruleMsg: string
  count: number
}

// Mesh
export interface MeshStatus {
  totalServices: number
  mtlsEnabled: number
  services: MeshService[]
}

export interface MeshService {
  name: string
  namespace: string
  spiffeId?: string
  meshEnabled: boolean
  mtlsStatus: string
}

export interface MeshTopology {
  nodes: MeshNode[]
  edges: MeshEdge[]
}

export interface MeshNode {
  id: string
  name: string
  namespace: string
  spiffeId?: string
}

export interface MeshEdge {
  source: string
  target: string
  mtls: boolean
  traffic?: number
}

// Config snapshots
export interface ConfigSnapshot {
  id: string
  timestamp: string
  config: string
  comment: string
}

export interface ConfigDiff {
  from: ConfigSnapshot
  to: ConfigSnapshot
}

// Resource type for generic handling
export type ResourceType = 'gateway' | 'route' | 'backend' | 'vip' | 'policy' | 'certificate' | 'ippool'
export type Resource = Gateway | Route | Backend | VIP | Policy | Certificate | IPPool
