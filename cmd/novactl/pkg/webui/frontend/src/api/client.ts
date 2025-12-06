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

export const api = {
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

  // Health
  health: async (): Promise<{ status: string }> => {
    return fetchJSON<{ status: string }>(`${API_BASE}/health`)
  },
}

export type ApiClient = typeof api
