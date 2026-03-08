/**
 * Normalizes API responses to handle both flat (standalone) and nested (Kubernetes) formats.
 * The standalone backend returns flat structures like {name, namespace, listeners}
 * The Kubernetes backend returns nested structures like {metadata: {name, namespace}, spec: {listeners}}
 *
 * This normalizer converts flat format to nested format for consistent frontend handling.
 */

import type { Gateway, Route, Backend, Policy } from './types'

// Type guard to check if data is in flat format (has name at root level, not in metadata)
function isFlatFormat(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const obj = data as Record<string, unknown>
  // If it has 'name' at root but no 'metadata', it's flat format
  return 'name' in obj && !('metadata' in obj)
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyObject = Record<string, any>

/**
 * Normalizes a Gateway from flat format to nested Kubernetes-style format
 */
export function normalizeGateway(data: AnyObject): Gateway {
  if (!isFlatFormat(data)) {
    return data as Gateway
  }

  return {
    apiVersion: 'novaedge.io/v1alpha1',
    kind: 'ProxyGateway',
    metadata: {
      name: data.name,
      namespace: data.namespace,
      resourceVersion: data.resourceVersion,
    },
    spec: {
      listeners: data.listeners || [],
      tracing: data.tracing,
      accessLog: data.accessLog,
    },
    status: data.status || { ready: false },
  }
}

/**
 * Normalizes a Route from flat format to nested Kubernetes-style format
 */
export function normalizeRoute(data: AnyObject): Route {
  if (!isFlatFormat(data)) {
    return data as Route
  }

  return {
    apiVersion: 'novaedge.io/v1alpha1',
    kind: 'ProxyRoute',
    metadata: {
      name: data.name,
      namespace: data.namespace,
      resourceVersion: data.resourceVersion,
    },
    spec: {
      hostnames: data.hostnames,
      rules: data.matches ? [{
        matches: data.matches,
        backendRefs: data.backendRefs,
        filters: data.filters,
        timeout: data.timeout,
      }] : undefined,
      parentRefs: data.gatewayRef ? [data.gatewayRef] : undefined,
    },
    status: data.status,
  }
}

/**
 * Normalizes a Backend from flat format to nested Kubernetes-style format
 */
export function normalizeBackend(data: AnyObject): Backend {
  if (!isFlatFormat(data)) {
    return data as Backend
  }

  return {
    apiVersion: 'novaedge.io/v1alpha1',
    kind: 'ProxyBackend',
    metadata: {
      name: data.name,
      namespace: data.namespace,
      resourceVersion: data.resourceVersion,
    },
    spec: {
      endpoints: data.endpoints || [],
      loadBalancer: data.lbPolicy ? { algorithm: data.lbPolicy } : undefined,
      healthCheck: data.healthCheck,
      circuitBreaker: data.circuitBreaker,
      connectionPool: data.connectionPool,
      tls: data.tls,
    },
    status: data.status || {
      healthyEndpoints: data.endpoints?.length || 0,
      totalEndpoints: data.endpoints?.length || 0,
    },
  }
}

/**
 * Normalizes a Policy from flat format to nested Kubernetes-style format
 */
export function normalizePolicy(data: AnyObject): Policy {
  if (!isFlatFormat(data)) {
    return data as Policy
  }

  return {
    apiVersion: 'novaedge.io/v1alpha1',
    kind: 'ProxyPolicy',
    metadata: {
      name: data.name,
      namespace: data.namespace,
      resourceVersion: data.resourceVersion,
    },
    spec: {
      targetRef: data.targetRef,
      rateLimit: data.rateLimit,
      cors: data.cors,
      ipFilter: data.ipFilter,
      jwt: data.jwt,
    },
    status: data.status,
  }
}

/**
 * Normalizes an array of resources
 */
export function normalizeGateways(data: AnyObject[]): Gateway[] {
  return data.map(normalizeGateway)
}

export function normalizeRoutes(data: AnyObject[]): Route[] {
  return data.map(normalizeRoute)
}

export function normalizeBackends(data: AnyObject[]): Backend[] {
  return data.map(normalizeBackend)
}

export function normalizePolicies(data: AnyObject[]): Policy[] {
  return data.map(normalizePolicy)
}
