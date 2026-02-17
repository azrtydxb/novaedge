import type {
  Gateway,
  Route,
  Backend,
  VIP,
  Policy,
  AgentInfo,
  DashboardMetrics,
  ModeInfo,
  ImportResult,
  ValidationResult,
  HistoryEntry,
  Certificate,
  IPPool,
  GenericResource,
  AuthSession,
  LoginResult,
  Trace,
  KubeEvent,
  WAFSummary,
  MeshStatus,
  MeshTopology,
  ConfigSnapshot,
  ConfigDiff,
  Federation,
  RemoteCluster,
  OverloadConfig,
  OverloadStatus,
  WASMPluginConfig,
  WASMPluginStatus,
} from './types'
import {
  normalizeGateways,
  normalizeGateway,
  normalizeRoutes,
  normalizeRoute,
  normalizeBackends,
  normalizeBackend,
  normalizeVIPs,
  normalizeVIP,
  normalizePolicies,
  normalizePolicy,
} from './normalizer'

const API_BASE = '/api/v1'

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(error.error || 'Request failed')
  }

  return response.json()
}

async function fetchText(url: string, options?: RequestInit): Promise<string> {
  const response = await fetch(url, {
    ...options,
    headers: {
      ...options?.headers,
    },
  })

  if (!response.ok) {
    const error = await response.text().catch(() => response.statusText)
    throw new Error(error || 'Request failed')
  }

  return response.text()
}

// Resource CRUD operations with normalization for each type
const gatewaysAPI = {
  list: async (namespace: string = 'all'): Promise<Gateway[]> => {
    const data = await fetchJSON<unknown[]>(`${API_BASE}/gateways?namespace=${namespace}`)
    return normalizeGateways(data as Record<string, unknown>[])
  },
  get: async (namespace: string, name: string): Promise<Gateway> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/gateways/${namespace}/${name}`)
    return normalizeGateway(data as Record<string, unknown>)
  },
  create: async (resource: Gateway): Promise<Gateway> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/gateways`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
    return normalizeGateway(data as Record<string, unknown>)
  },
  update: async (namespace: string, name: string, resource: Gateway): Promise<Gateway> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/gateways/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
    return normalizeGateway(data as Record<string, unknown>)
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/gateways/${namespace}/${name}`, { method: 'DELETE' })
  },
}

const routesAPI = {
  list: async (namespace: string = 'all'): Promise<Route[]> => {
    const data = await fetchJSON<unknown[]>(`${API_BASE}/routes?namespace=${namespace}`)
    return normalizeRoutes(data as Record<string, unknown>[])
  },
  get: async (namespace: string, name: string): Promise<Route> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/routes/${namespace}/${name}`)
    return normalizeRoute(data as Record<string, unknown>)
  },
  create: async (resource: Route): Promise<Route> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/routes`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
    return normalizeRoute(data as Record<string, unknown>)
  },
  update: async (namespace: string, name: string, resource: Route): Promise<Route> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/routes/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
    return normalizeRoute(data as Record<string, unknown>)
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/routes/${namespace}/${name}`, { method: 'DELETE' })
  },
}

const backendsAPI = {
  list: async (namespace: string = 'all'): Promise<Backend[]> => {
    const data = await fetchJSON<unknown[]>(`${API_BASE}/backends?namespace=${namespace}`)
    return normalizeBackends(data as Record<string, unknown>[])
  },
  get: async (namespace: string, name: string): Promise<Backend> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/backends/${namespace}/${name}`)
    return normalizeBackend(data as Record<string, unknown>)
  },
  create: async (resource: Backend): Promise<Backend> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/backends`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
    return normalizeBackend(data as Record<string, unknown>)
  },
  update: async (namespace: string, name: string, resource: Backend): Promise<Backend> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/backends/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
    return normalizeBackend(data as Record<string, unknown>)
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/backends/${namespace}/${name}`, { method: 'DELETE' })
  },
}

const vipsAPI = {
  list: async (namespace: string = 'all'): Promise<VIP[]> => {
    const data = await fetchJSON<unknown[]>(`${API_BASE}/vips?namespace=${namespace}`)
    return normalizeVIPs(data as Record<string, unknown>[])
  },
  get: async (namespace: string, name: string): Promise<VIP> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/vips/${namespace}/${name}`)
    return normalizeVIP(data as Record<string, unknown>)
  },
  create: async (resource: VIP): Promise<VIP> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/vips`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
    return normalizeVIP(data as Record<string, unknown>)
  },
  update: async (namespace: string, name: string, resource: VIP): Promise<VIP> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/vips/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
    return normalizeVIP(data as Record<string, unknown>)
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/vips/${namespace}/${name}`, { method: 'DELETE' })
  },
}

const policiesAPI = {
  list: async (namespace: string = 'all'): Promise<Policy[]> => {
    const data = await fetchJSON<unknown[]>(`${API_BASE}/policies?namespace=${namespace}`)
    return normalizePolicies(data as Record<string, unknown>[])
  },
  get: async (namespace: string, name: string): Promise<Policy> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/policies/${namespace}/${name}`)
    return normalizePolicy(data as Record<string, unknown>)
  },
  create: async (resource: Policy): Promise<Policy> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/policies`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
    return normalizePolicy(data as Record<string, unknown>)
  },
  update: async (namespace: string, name: string, resource: Policy): Promise<Policy> => {
    const data = await fetchJSON<unknown>(`${API_BASE}/policies/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
    return normalizePolicy(data as Record<string, unknown>)
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/policies/${namespace}/${name}`, { method: 'DELETE' })
  },
}

// Certificates CRUD
const certificatesAPI = {
  list: async (namespace: string = 'all'): Promise<Certificate[]> => {
    return fetchJSON<Certificate[]>(`${API_BASE}/certificates?namespace=${namespace}`)
  },
  get: async (namespace: string, name: string): Promise<Certificate> => {
    return fetchJSON<Certificate>(`${API_BASE}/certificates/${namespace}/${name}`)
  },
  create: async (resource: Certificate): Promise<Certificate> => {
    return fetchJSON<Certificate>(`${API_BASE}/certificates`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (namespace: string, name: string, resource: Certificate): Promise<Certificate> => {
    return fetchJSON<Certificate>(`${API_BASE}/certificates/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/certificates/${namespace}/${name}`, { method: 'DELETE' })
  },
}

// IP Pools CRUD
const ippoolsAPI = {
  list: async (): Promise<IPPool[]> => {
    return fetchJSON<IPPool[]>(`${API_BASE}/ippools`)
  },
  get: async (name: string): Promise<IPPool> => {
    return fetchJSON<IPPool>(`${API_BASE}/ippools/${name}`)
  },
  create: async (resource: IPPool): Promise<IPPool> => {
    return fetchJSON<IPPool>(`${API_BASE}/ippools`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (name: string, resource: IPPool): Promise<IPPool> => {
    return fetchJSON<IPPool>(`${API_BASE}/ippools/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/ippools/${name}`, { method: 'DELETE' })
  },
}

// Clusters CRUD (NovaEdgeCluster)
const clustersAPI = {
  list: async (namespace: string = 'all'): Promise<GenericResource[]> => {
    return fetchJSON<GenericResource[]>(`${API_BASE}/clusters?namespace=${namespace}`)
  },
  get: async (namespace: string, name: string): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/clusters/${namespace}/${name}`)
  },
  create: async (resource: GenericResource): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/clusters`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (namespace: string, name: string, resource: GenericResource): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/clusters/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/clusters/${namespace}/${name}`, { method: 'DELETE' })
  },
}

// Federations CRUD
const federationsAPI = {
  list: async (namespace: string = 'all'): Promise<Federation[]> => {
    return fetchJSON<Federation[]>(`${API_BASE}/federations?namespace=${namespace}`)
  },
  get: async (namespace: string, name: string): Promise<Federation> => {
    return fetchJSON<Federation>(`${API_BASE}/federations/${namespace}/${name}`)
  },
  create: async (resource: Federation): Promise<Federation> => {
    return fetchJSON<Federation>(`${API_BASE}/federations`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (namespace: string, name: string, resource: Federation): Promise<Federation> => {
    return fetchJSON<Federation>(`${API_BASE}/federations/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/federations/${namespace}/${name}`, { method: 'DELETE' })
  },
}

// Remote Clusters CRUD
const remoteclustersAPI = {
  list: async (namespace: string = 'all'): Promise<RemoteCluster[]> => {
    return fetchJSON<RemoteCluster[]>(`${API_BASE}/remoteclusters?namespace=${namespace}`)
  },
  get: async (namespace: string, name: string): Promise<RemoteCluster> => {
    return fetchJSON<RemoteCluster>(`${API_BASE}/remoteclusters/${namespace}/${name}`)
  },
  create: async (resource: RemoteCluster): Promise<RemoteCluster> => {
    return fetchJSON<RemoteCluster>(`${API_BASE}/remoteclusters`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (namespace: string, name: string, resource: RemoteCluster): Promise<RemoteCluster> => {
    return fetchJSON<RemoteCluster>(`${API_BASE}/remoteclusters/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/remoteclusters/${namespace}/${name}`, { method: 'DELETE' })
  },
}

// Overload/Load Shedding
const overloadAPI = {
  status: async (): Promise<OverloadStatus> => {
    return fetchJSON<OverloadStatus>(`${API_BASE}/overload/status`)
  },
  config: async (): Promise<OverloadConfig> => {
    return fetchJSON<OverloadConfig>(`${API_BASE}/overload/config`)
  },
  updateConfig: async (config: OverloadConfig): Promise<OverloadConfig> => {
    return fetchJSON<OverloadConfig>(`${API_BASE}/overload/config`, {
      method: 'PUT',
      body: JSON.stringify(config),
    })
  },
}

// WASM Plugins
const wasmPluginsAPI = {
  list: async (): Promise<WASMPluginStatus[]> => {
    return fetchJSON<WASMPluginStatus[]>(`${API_BASE}/wasm/plugins`)
  },
  get: async (name: string): Promise<WASMPluginConfig> => {
    return fetchJSON<WASMPluginConfig>(`${API_BASE}/wasm/plugins/${name}`)
  },
}

export const api = {
  // Auth
  auth: {
    getSession: async (): Promise<AuthSession> => {
      return fetchJSON<AuthSession>(`${API_BASE}/auth/session`)
    },
    login: async (username: string, password: string): Promise<LoginResult> => {
      return fetchJSON<LoginResult>(`${API_BASE}/auth/login`, {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
    },
    logout: async (): Promise<void> => {
      await fetchJSON<void>(`${API_BASE}/auth/logout`, { method: 'POST' })
    },
  },

  // Mode
  getMode: async (): Promise<ModeInfo> => {
    return fetchJSON<ModeInfo>(`${API_BASE}/mode`)
  },

  // Namespaces
  namespaces: {
    list: async (): Promise<string[]> => {
      return fetchJSON<string[]>(`${API_BASE}/namespaces`)
    },
  },

  // Resources (with normalization for flat/nested format support)
  gateways: gatewaysAPI,
  routes: routesAPI,
  backends: backendsAPI,
  vips: vipsAPI,
  policies: policiesAPI,

  // New CRDs
  certificates: certificatesAPI,
  ippools: ippoolsAPI,
  clusters: clustersAPI,
  federations: federationsAPI,
  remoteclusters: remoteclustersAPI,
  overload: overloadAPI,
  wasmPlugins: wasmPluginsAPI,

  // Agents
  agents: {
    list: async (namespace: string = 'novaedge-system'): Promise<AgentInfo[]> => {
      return fetchJSON<AgentInfo[]>(`${API_BASE}/agents?namespace=${namespace}`)
    },
  },

  // Metrics
  metrics: {
    dashboard: async (): Promise<DashboardMetrics> => {
      return fetchJSON<DashboardMetrics>(`${API_BASE}/metrics/dashboard`)
    },

    query: async (query: string): Promise<unknown> => {
      return fetchJSON(`${API_BASE}/metrics/query?query=${encodeURIComponent(query)}`)
    },

    queryRange: async (
      query: string,
      start: Date,
      end: Date,
      step: string = '15s'
    ): Promise<unknown> => {
      const params = new URLSearchParams({
        query,
        start: start.toISOString(),
        end: end.toISOString(),
        step,
      })
      return fetchJSON(`${API_BASE}/metrics/query?${params}`)
    },
  },

  // Config management
  config: {
    validate: async (config: unknown): Promise<ValidationResult> => {
      return fetchJSON<ValidationResult>(`${API_BASE}/config/validate`, {
        method: 'POST',
        body: JSON.stringify(config),
      })
    },

    export: async (namespace: string = 'all'): Promise<Blob> => {
      const response = await fetch(
        `${API_BASE}/config/export?namespace=${namespace}`,
        { method: 'POST' }
      )
      if (!response.ok) {
        throw new Error('Export failed')
      }
      return response.blob()
    },

    import: async (data: string, dryRun: boolean = false): Promise<ImportResult> => {
      const response = await fetch(
        `${API_BASE}/config/import?dryRun=${dryRun}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/x-yaml' },
          body: data,
        }
      )
      if (!response.ok) {
        const error = await response.json().catch(() => ({ error: 'Import failed' }))
        throw new Error(error.error)
      }
      return response.json()
    },

    history: async (): Promise<HistoryEntry[]> => {
      return fetchJSON<HistoryEntry[]>(`${API_BASE}/config/history`)
    },

    restore: async (id: string): Promise<void> => {
      await fetchJSON(`${API_BASE}/config/history/${id}/restore`, {
        method: 'POST',
      })
    },
  },

  // Traces
  traces: {
    search: async (params: Record<string, string>): Promise<Trace[]> => {
      return fetchJSON<Trace[]>(`${API_BASE}/traces?${new URLSearchParams(params)}`)
    },
    get: async (traceID: string): Promise<Trace> => {
      return fetchJSON<Trace>(`${API_BASE}/traces/${traceID}`)
    },
    services: async (): Promise<string[]> => {
      return fetchJSON<string[]>(`${API_BASE}/traces/services`)
    },
    operations: async (service: string): Promise<string[]> => {
      return fetchJSON<string[]>(`${API_BASE}/traces/operations?service=${encodeURIComponent(service)}`)
    },
  },

  // Logs
  logs: {
    get: async (pod: string, namespace: string, tailLines: number = 100): Promise<string> => {
      return fetchText(
        `${API_BASE}/logs?pod=${encodeURIComponent(pod)}&namespace=${encodeURIComponent(namespace)}&tailLines=${tailLines}`
      )
    },
  },

  // Events
  events: {
    list: async (namespace?: string, involved?: string): Promise<KubeEvent[]> => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (involved) params.set('involved', involved)
      return fetchJSON<KubeEvent[]>(`${API_BASE}/events?${params}`)
    },
  },

  // WAF
  waf: {
    events: async (): Promise<WAFSummary> => {
      return fetchJSON<WAFSummary>(`${API_BASE}/waf/events`)
    },
  },

  // Mesh
  mesh: {
    status: async (): Promise<MeshStatus> => {
      return fetchJSON<MeshStatus>(`${API_BASE}/mesh/status`)
    },
    topology: async (): Promise<MeshTopology> => {
      return fetchJSON<MeshTopology>(`${API_BASE}/mesh/topology`)
    },
  },

  // Config Snapshots
  configSnapshots: {
    list: async (): Promise<ConfigSnapshot[]> => {
      return fetchJSON<ConfigSnapshot[]>(`${API_BASE}/config/snapshots`)
    },
    get: async (id: string): Promise<ConfigSnapshot> => {
      return fetchJSON<ConfigSnapshot>(`${API_BASE}/config/snapshots/${id}`)
    },
    create: async (comment?: string): Promise<ConfigSnapshot> => {
      return fetchJSON<ConfigSnapshot>(`${API_BASE}/config/snapshots`, {
        method: 'POST',
        body: JSON.stringify({ comment }),
      })
    },
    diff: async (fromId: string, toId: string): Promise<ConfigDiff> => {
      return fetchJSON<ConfigDiff>(`${API_BASE}/config/diff?from=${fromId}&to=${toId}`)
    },
    rollback: async (id: string): Promise<void> => {
      await fetchJSON<unknown>(`${API_BASE}/config/rollback/${id}`, { method: 'POST' })
    },
  },

  // Health
  health: async (): Promise<{ status: string }> => {
    return fetchJSON<{ status: string }>(`${API_BASE}/health`)
  },
}

export type ApiClient = typeof api
