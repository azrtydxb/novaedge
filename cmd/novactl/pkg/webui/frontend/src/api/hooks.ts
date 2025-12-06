import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type {
  Gateway,
  Route,
  Backend,
  VIP,
  Policy,
  AgentInfo,
  DashboardMetrics,
} from './types'
import { toast } from '@/hooks/use-toast'

// Gateways
export function useGateways(namespace: string) {
  return useQuery({
    queryKey: ['gateways', namespace],
    queryFn: () => api.gateways.list(namespace),
  })
}

export function useGateway(namespace: string, name: string) {
  return useQuery({
    queryKey: ['gateways', namespace, name],
    queryFn: () => api.gateways.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateGateway() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (gateway: Gateway) => api.gateways.create(gateway),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
      toast({ title: 'Gateway created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create gateway', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateGateway() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, gateway }: { namespace: string; name: string; gateway: Gateway }) =>
      api.gateways.update(namespace, name, gateway),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
      toast({ title: 'Gateway updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update gateway', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteGateway() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.gateways.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
      toast({ title: 'Gateway deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete gateway', description: error.message, variant: 'destructive' })
    },
  })
}

// Routes
export function useRoutes(namespace: string) {
  return useQuery({
    queryKey: ['routes', namespace],
    queryFn: () => api.routes.list(namespace),
  })
}

export function useRoute(namespace: string, name: string) {
  return useQuery({
    queryKey: ['routes', namespace, name],
    queryFn: () => api.routes.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (route: Route) => api.routes.create(route),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      toast({ title: 'Route created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create route', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, route }: { namespace: string; name: string; route: Route }) =>
      api.routes.update(namespace, name, route),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      toast({ title: 'Route updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update route', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.routes.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      toast({ title: 'Route deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete route', description: error.message, variant: 'destructive' })
    },
  })
}

// Backends
export function useBackends(namespace: string) {
  return useQuery({
    queryKey: ['backends', namespace],
    queryFn: () => api.backends.list(namespace),
  })
}

export function useBackend(namespace: string, name: string) {
  return useQuery({
    queryKey: ['backends', namespace, name],
    queryFn: () => api.backends.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateBackend() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (backend: Backend) => api.backends.create(backend),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast({ title: 'Backend created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create backend', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateBackend() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, backend }: { namespace: string; name: string; backend: Backend }) =>
      api.backends.update(namespace, name, backend),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast({ title: 'Backend updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update backend', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteBackend() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.backends.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast({ title: 'Backend deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete backend', description: error.message, variant: 'destructive' })
    },
  })
}

// VIPs
export function useVIPs(namespace: string) {
  return useQuery({
    queryKey: ['vips', namespace],
    queryFn: () => api.vips.list(namespace),
  })
}

export function useVIP(namespace: string, name: string) {
  return useQuery({
    queryKey: ['vips', namespace, name],
    queryFn: () => api.vips.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateVIP() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vip: VIP) => api.vips.create(vip),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vips'] })
      toast({ title: 'VIP created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create VIP', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateVIP() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, vip }: { namespace: string; name: string; vip: VIP }) =>
      api.vips.update(namespace, name, vip),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vips'] })
      toast({ title: 'VIP updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update VIP', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteVIP() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.vips.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vips'] })
      toast({ title: 'VIP deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete VIP', description: error.message, variant: 'destructive' })
    },
  })
}

// Policies
export function usePolicies(namespace: string) {
  return useQuery({
    queryKey: ['policies', namespace],
    queryFn: () => api.policies.list(namespace),
  })
}

export function usePolicy(namespace: string, name: string) {
  return useQuery({
    queryKey: ['policies', namespace, name],
    queryFn: () => api.policies.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreatePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (policy: Policy) => api.policies.create(policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] })
      toast({ title: 'Policy created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create policy', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdatePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, policy }: { namespace: string; name: string; policy: Policy }) =>
      api.policies.update(namespace, name, policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] })
      toast({ title: 'Policy updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update policy', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeletePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.policies.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] })
      toast({ title: 'Policy deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete policy', description: error.message, variant: 'destructive' })
    },
  })
}

// Agents
export function useAgents(namespace: string = 'novaedge-system') {
  return useQuery<AgentInfo[]>({
    queryKey: ['agents', namespace],
    queryFn: () => api.agents.list(namespace),
  })
}

// Metrics
export function useDashboardMetrics() {
  return useQuery<DashboardMetrics>({
    queryKey: ['metrics', 'dashboard'],
    queryFn: () => api.metrics.dashboard(),
    refetchInterval: 30000, // Refresh every 30 seconds
  })
}

// Bulk delete mutation
export function useBulkDelete(resourceType: 'gateways' | 'routes' | 'backends' | 'vips' | 'policies') {
  const queryClient = useQueryClient()
  const apiResource = api[resourceType]

  return useMutation({
    mutationFn: async (items: { namespace: string; name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => apiResource.delete(item.namespace, item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [resourceType] })
      toast({ title: `Successfully deleted selected ${resourceType}`, variant: 'success' as const })
    },
    onError: (error: Error) => {
      queryClient.invalidateQueries({ queryKey: [resourceType] })
      toast({
        title: `Some ${resourceType} deletions failed`,
        description: error.message,
        variant: 'destructive',
      })
    },
  })
}
