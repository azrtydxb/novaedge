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
  Certificate,
  IPPool,
  GenericResource,
  Trace,
  KubeEvent,
  WAFSummary,
  MeshStatus,
  MeshTopology,
  ConfigSnapshot,
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

// Certificates
export function useCertificates(namespace: string) {
  return useQuery<Certificate[]>({
    queryKey: ['certificates', namespace],
    queryFn: () => api.certificates.list(namespace),
  })
}

export function useCertificate(namespace: string, name: string) {
  return useQuery<Certificate>({
    queryKey: ['certificates', namespace, name],
    queryFn: () => api.certificates.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (certificate: Certificate) => api.certificates.create(certificate),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast({ title: 'Certificate created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create certificate', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, certificate }: { namespace: string; name: string; certificate: Certificate }) =>
      api.certificates.update(namespace, name, certificate),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast({ title: 'Certificate updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update certificate', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.certificates.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast({ title: 'Certificate deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete certificate', description: error.message, variant: 'destructive' })
    },
  })
}

// IP Pools
export function useIPPools() {
  return useQuery<IPPool[]>({
    queryKey: ['ippools'],
    queryFn: () => api.ippools.list(),
  })
}

export function useIPPool(name: string) {
  return useQuery<IPPool>({
    queryKey: ['ippools', name],
    queryFn: () => api.ippools.get(name),
    enabled: !!name,
  })
}

export function useCreateIPPool() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (ippool: IPPool) => api.ippools.create(ippool),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ippools'] })
      toast({ title: 'IP Pool created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create IP Pool', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateIPPool() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name, ippool }: { name: string; ippool: IPPool }) =>
      api.ippools.update(name, ippool),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ippools'] })
      toast({ title: 'IP Pool updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update IP Pool', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteIPPool() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name }: { name: string }) => api.ippools.delete(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ippools'] })
      toast({ title: 'IP Pool deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete IP Pool', description: error.message, variant: 'destructive' })
    },
  })
}

// Clusters (NovaEdgeCluster)
export function useClusters(namespace: string) {
  return useQuery<GenericResource[]>({
    queryKey: ['clusters', namespace],
    queryFn: () => api.clusters.list(namespace),
  })
}

export function useCluster(namespace: string, name: string) {
  return useQuery<GenericResource>({
    queryKey: ['clusters', namespace, name],
    queryFn: () => api.clusters.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (cluster: GenericResource) => api.clusters.create(cluster),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      toast({ title: 'Cluster created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create cluster', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, cluster }: { namespace: string; name: string; cluster: GenericResource }) =>
      api.clusters.update(namespace, name, cluster),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      toast({ title: 'Cluster updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update cluster', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.clusters.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      toast({ title: 'Cluster deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete cluster', description: error.message, variant: 'destructive' })
    },
  })
}

// Federations
export function useFederations(namespace: string) {
  return useQuery<GenericResource[]>({
    queryKey: ['federations', namespace],
    queryFn: () => api.federations.list(namespace),
  })
}

export function useFederation(namespace: string, name: string) {
  return useQuery<GenericResource>({
    queryKey: ['federations', namespace, name],
    queryFn: () => api.federations.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateFederation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (federation: GenericResource) => api.federations.create(federation),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['federations'] })
      toast({ title: 'Federation created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create federation', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateFederation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, federation }: { namespace: string; name: string; federation: GenericResource }) =>
      api.federations.update(namespace, name, federation),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['federations'] })
      toast({ title: 'Federation updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update federation', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteFederation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.federations.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['federations'] })
      toast({ title: 'Federation deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete federation', description: error.message, variant: 'destructive' })
    },
  })
}

// Remote Clusters
export function useRemoteClusters(namespace: string) {
  return useQuery<GenericResource[]>({
    queryKey: ['remoteclusters', namespace],
    queryFn: () => api.remoteclusters.list(namespace),
  })
}

export function useRemoteCluster(namespace: string, name: string) {
  return useQuery<GenericResource>({
    queryKey: ['remoteclusters', namespace, name],
    queryFn: () => api.remoteclusters.get(namespace, name),
    enabled: !!namespace && !!name,
  })
}

export function useCreateRemoteCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (remoteCluster: GenericResource) => api.remoteclusters.create(remoteCluster),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['remoteclusters'] })
      toast({ title: 'Remote cluster created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create remote cluster', description: error.message, variant: 'destructive' })
    },
  })
}

export function useUpdateRemoteCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name, remoteCluster }: { namespace: string; name: string; remoteCluster: GenericResource }) =>
      api.remoteclusters.update(namespace, name, remoteCluster),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['remoteclusters'] })
      toast({ title: 'Remote cluster updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update remote cluster', description: error.message, variant: 'destructive' })
    },
  })
}

export function useDeleteRemoteCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      api.remoteclusters.delete(namespace, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['remoteclusters'] })
      toast({ title: 'Remote cluster deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete remote cluster', description: error.message, variant: 'destructive' })
    },
  })
}

// Traces (read-only observability)
export function useTraceSearch(params: Record<string, string>) {
  return useQuery<Trace[]>({
    queryKey: ['traces', 'search', params],
    queryFn: () => api.traces.search(params),
    enabled: Object.keys(params).length > 0,
  })
}

export function useTrace(traceId: string) {
  return useQuery<Trace>({
    queryKey: ['traces', traceId],
    queryFn: () => api.traces.get(traceId),
    enabled: !!traceId,
  })
}

export function useTraceServices() {
  return useQuery<string[]>({
    queryKey: ['traces', 'services'],
    queryFn: () => api.traces.services(),
  })
}

export function useTraceOperations(service: string) {
  return useQuery<string[]>({
    queryKey: ['traces', 'operations', service],
    queryFn: () => api.traces.operations(service),
    enabled: !!service,
  })
}

// Logs
export function useLogs(pod: string, namespace: string, tailLines: number = 100) {
  return useQuery<string>({
    queryKey: ['logs', pod, namespace, tailLines],
    queryFn: () => api.logs.get(pod, namespace, tailLines),
    enabled: !!pod && !!namespace,
  })
}

// Events
export function useEvents(namespace?: string) {
  return useQuery<KubeEvent[]>({
    queryKey: ['events', namespace],
    queryFn: () => api.events.list(namespace),
  })
}

// WAF
export function useWAFEvents() {
  return useQuery<WAFSummary>({
    queryKey: ['waf', 'events'],
    queryFn: () => api.waf.events(),
    refetchInterval: 30000,
  })
}

// Mesh
export function useMeshStatus() {
  return useQuery<MeshStatus>({
    queryKey: ['mesh', 'status'],
    queryFn: () => api.mesh.status(),
    refetchInterval: 30000,
  })
}

export function useMeshTopology() {
  return useQuery<MeshTopology>({
    queryKey: ['mesh', 'topology'],
    queryFn: () => api.mesh.topology(),
    refetchInterval: 30000,
  })
}

// Config Snapshots
export function useConfigSnapshots() {
  return useQuery<ConfigSnapshot[]>({
    queryKey: ['configSnapshots'],
    queryFn: () => api.configSnapshots.list(),
  })
}

export function useConfigSnapshot(id: string) {
  return useQuery<ConfigSnapshot>({
    queryKey: ['configSnapshots', id],
    queryFn: () => api.configSnapshots.get(id),
    enabled: !!id,
  })
}

export function useCreateConfigSnapshot() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (comment?: string) => api.configSnapshots.create(comment),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configSnapshots'] })
      toast({ title: 'Config snapshot created', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create snapshot', description: error.message, variant: 'destructive' })
    },
  })
}

export function useConfigDiff(fromId: string, toId: string) {
  return useQuery({
    queryKey: ['configSnapshots', 'diff', fromId, toId],
    queryFn: () => api.configSnapshots.diff(fromId, toId),
    enabled: !!fromId && !!toId,
  })
}

export function useRollbackConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.configSnapshots.rollback(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configSnapshots'] })
      toast({ title: 'Config rolled back successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to rollback config', description: error.message, variant: 'destructive' })
    },
  })
}
