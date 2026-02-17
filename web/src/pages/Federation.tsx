import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useFederations,
  useCreateFederation,
  useUpdateFederation,
  useDeleteFederation,
  useRemoteClusters,
  useCreateRemoteCluster,
  useUpdateRemoteCluster,
  useDeleteRemoteCluster,
} from '@/api/hooks'
import type { Federation as FederationType, RemoteCluster } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { toast } from '@/hooks/use-toast'

const DEFAULT_FEDERATION_YAML = `apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeFederation
metadata:
  name: my-federation
  namespace: novaedge-system
spec:
  mode: mesh
  syncInterval: "30s"
  antiEntropy:
    enabled: true
    interval: "60s"
    repairMode: bidirectional
  splitBrain:
    enabled: true
    quorum: 2
    partitionTimeout: "30s"
    autoHeal: true
`

const DEFAULT_REMOTE_CLUSTER_YAML = `apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeRemoteCluster
metadata:
  name: remote-cluster-1
  namespace: novaedge-system
spec:
  endpoint: "https://remote-cluster:6443"
  tunnel:
    type: wireguard
  tls:
    secretName: remote-cluster-tls
`

function formatLastSync(lastSyncTime: string | undefined): string {
  if (!lastSyncTime) return 'N/A'
  try {
    return new Date(lastSyncTime).toLocaleString()
  } catch {
    return String(lastSyncTime)
  }
}

function getModeBadge(mode: string | undefined) {
  const modeStr = (mode ?? '').toLowerCase()
  switch (modeStr) {
    case 'hub-spoke':
      return (
        <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
          hub-spoke
        </Badge>
      )
    case 'mesh':
      return (
        <Badge className="bg-purple-500 hover:bg-purple-600 text-white">
          mesh
        </Badge>
      )
    case 'unified':
      return (
        <Badge className="bg-green-500 hover:bg-green-600 text-white">
          unified
        </Badge>
      )
    default:
      return <Badge variant="secondary">{mode ?? '-'}</Badge>
  }
}

function getSplitBrainBadge(state: string | undefined) {
  const stateStr = (state ?? '').toLowerCase()
  switch (stateStr) {
    case 'healthy':
      return (
        <Badge className="bg-green-500 hover:bg-green-600 text-white">
          healthy
        </Badge>
      )
    case 'detected':
      return (
        <Badge className="bg-red-500 hover:bg-red-600 text-white">
          detected
        </Badge>
      )
    case 'healing':
      return (
        <Badge className="bg-yellow-500 hover:bg-yellow-600 text-white">
          healing
        </Badge>
      )
    default:
      return <Badge variant="outline">{state ?? 'N/A'}</Badge>
  }
}

function getSyncStatusBadge(phase: string | undefined) {
  const phaseStr = (phase ?? '').toLowerCase()
  if (phaseStr === 'synced' || phaseStr === 'insync' || phaseStr === 'in-sync' || phaseStr === 'ready' || phaseStr === 'active') {
    return (
      <Badge className="bg-green-500 hover:bg-green-600 text-white">
        {phase}
      </Badge>
    )
  }
  if (phaseStr === 'syncing' || phaseStr === 'pending' || phaseStr === 'reconciling') {
    return (
      <Badge className="bg-yellow-500 hover:bg-yellow-600 text-white">
        {phase}
      </Badge>
    )
  }
  if (phaseStr === 'error' || phaseStr === 'failed' || phaseStr === 'degraded') {
    return (
      <Badge className="bg-red-500 hover:bg-red-600 text-white">
        {phase}
      </Badge>
    )
  }
  return <Badge variant="outline">{phase ?? 'N/A'}</Badge>
}

function getTunnelTypeBadge(tunnelType: string | undefined) {
  const typeStr = (tunnelType ?? '').toLowerCase()
  switch (typeStr) {
    case 'wireguard':
      return (
        <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
          wireguard
        </Badge>
      )
    case 'ssh':
      return (
        <Badge className="bg-yellow-500 hover:bg-yellow-600 text-white">
          ssh
        </Badge>
      )
    case 'websocket':
      return (
        <Badge className="bg-purple-500 hover:bg-purple-600 text-white">
          websocket
        </Badge>
      )
    case 'none':
    case '':
      return <Badge variant="secondary">none</Badge>
    default:
      return <Badge variant="outline">{tunnelType ?? 'none'}</Badge>
  }
}

function getConnectedIndicator(connected: boolean | undefined) {
  if (connected) {
    return (
      <span className="flex items-center gap-2">
        <span className="inline-block h-2.5 w-2.5 rounded-full bg-green-500" />
        Yes
      </span>
    )
  }
  return (
    <span className="flex items-center gap-2">
      <span className="inline-block h-2.5 w-2.5 rounded-full bg-red-500" />
      No
    </span>
  )
}

// --- Federations Sub-Component ---

function FederationsTab() {
  const { namespace, readOnly } = useApp()
  const { data: federations = [], isLoading, error } = useFederations(namespace)
  const createFederation = useCreateFederation()
  const updateFederation = useUpdateFederation()
  const deleteFederation = useDeleteFederation()

  const queryClient = useQueryClient()
  const bulkDelete = useMutation({
    mutationFn: async (items: { namespace: string; name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => api.federations.delete(item.namespace, item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['federations'] })
      toast({ title: 'Successfully deleted selected federations', variant: 'success' as const })
    },
    onError: (err: Error) => {
      queryClient.invalidateQueries({ queryKey: ['federations'] })
      toast({
        title: 'Some federation deletions failed',
        description: err.message,
        variant: 'destructive',
      })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentFederation, setCurrentFederation] = useState<FederationType | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [federationToDelete, setFederationToDelete] = useState<FederationType | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<FederationType>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.metadata?.name ?? '-',
      sortable: true,
    },
    {
      key: 'mode',
      header: 'Mode',
      accessor: (row) => getModeBadge(row.spec?.mode),
    },
    {
      key: 'members',
      header: 'Members',
      accessor: (row) => {
        const specMembers = row.spec?.members
        const statusMembers = row.status?.members
        const count = specMembers?.length ?? statusMembers?.length ?? 0
        return count
      },
      sortable: true,
    },
    {
      key: 'syncStatus',
      header: 'Sync Status',
      accessor: (row) => getSyncStatusBadge(row.status?.phase),
    },
    {
      key: 'splitBrain',
      header: 'Split-Brain State',
      accessor: (row) => getSplitBrainBadge(row.status?.splitBrainState),
    },
    {
      key: 'lastSync',
      header: 'Last Sync',
      accessor: (row) => formatLastSync(row.status?.lastSyncTime),
      sortable: true,
    },
    {
      key: 'age',
      header: 'Age',
      accessor: (row) =>
        row.metadata?.creationTimestamp
          ? formatAge(row.metadata.creationTimestamp)
          : 'N/A',
      sortable: true,
    },
  ]

  const handleCreate = () => {
    setCurrentFederation(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (federation: FederationType) => {
    setCurrentFederation(federation)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (federation: FederationType) => {
    setFederationToDelete(federation)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (federationToDelete) {
      deleteFederation.mutate({
        namespace: federationToDelete.metadata?.namespace ?? '',
        name: federationToDelete.metadata?.name ?? '',
      })
    }
  }

  const handleBulkDelete = () => {
    setBulkDeleteDialogOpen(true)
  }

  const confirmBulkDelete = () => {
    const items = Array.from(selectedRows).map((key) => {
      const [ns, name] = key.split('/')
      return { namespace: ns, name }
    })
    bulkDelete.mutate(items, {
      onSuccess: () => setSelectedRows(new Set()),
    })
  }

  const handleSubmit = async (federation: FederationType) => {
    if (dialogMode === 'create') {
      await createFederation.mutateAsync(federation)
    } else {
      await updateFederation.mutateAsync({
        namespace: federation.metadata?.namespace ?? '',
        name: federation.metadata?.name ?? '',
        federation,
      })
    }
  }

  const getRowKey = (row: FederationType) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load federations: {error.message}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {!readOnly && selectedRows.size > 0 && (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleBulkDelete}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete ({selectedRows.size})
            </Button>
          )}
        </div>
        {!readOnly && (
          <Button onClick={handleCreate}>
            <Plus className="h-4 w-4 mr-2" />
            Create Federation
          </Button>
        )}
      </div>

      <DataTable
        data={federations}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No federations found"
        searchPlaceholder="Search federations..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.mode?.toLowerCase().includes(query) ||
          false
        }
        actions={(row) =>
          readOnly
            ? [{ label: 'View', onClick: () => handleEdit(row) }]
            : [
                { label: 'Edit', onClick: () => handleEdit(row) },
                {
                  label: 'Delete',
                  onClick: () => handleDelete(row),
                  variant: 'destructive',
                },
              ]
        }
      />

      <ResourceDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title={
          dialogMode === 'create'
            ? 'Create Federation'
            : dialogMode === 'edit'
            ? 'Edit Federation'
            : 'View Federation'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new multi-cluster federation'
            : undefined
        }
        mode={dialogMode}
        resource={currentFederation}
        onSubmit={handleSubmit}
        isLoading={createFederation.isPending || updateFederation.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_FEDERATION_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Federation"
        description={`Are you sure you want to delete federation "${federationToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteFederation.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Federations"
        description={`Are you sure you want to delete ${selectedRows.size} federation(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}

// --- Remote Clusters Sub-Component ---

function RemoteClustersTab() {
  const { namespace, readOnly } = useApp()
  const { data: remoteClusters = [], isLoading, error } = useRemoteClusters(namespace)
  const createRemoteCluster = useCreateRemoteCluster()
  const updateRemoteCluster = useUpdateRemoteCluster()
  const deleteRemoteCluster = useDeleteRemoteCluster()

  const queryClient = useQueryClient()
  const bulkDelete = useMutation({
    mutationFn: async (items: { namespace: string; name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => api.remoteclusters.delete(item.namespace, item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['remoteclusters'] })
      toast({ title: 'Successfully deleted selected remote clusters', variant: 'success' as const })
    },
    onError: (err: Error) => {
      queryClient.invalidateQueries({ queryKey: ['remoteclusters'] })
      toast({
        title: 'Some remote cluster deletions failed',
        description: err.message,
        variant: 'destructive',
      })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentRemoteCluster, setCurrentRemoteCluster] = useState<RemoteCluster | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [remoteClusterToDelete, setRemoteClusterToDelete] = useState<RemoteCluster | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<RemoteCluster>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.metadata?.name ?? '-',
      sortable: true,
    },
    {
      key: 'endpoint',
      header: 'Endpoint',
      accessor: (row) => row.spec?.endpoint ?? '-',
    },
    {
      key: 'tunnelType',
      header: 'Tunnel Type',
      accessor: (row) => getTunnelTypeBadge(row.spec?.tunnel?.type),
    },
    {
      key: 'connected',
      header: 'Connected',
      accessor: (row) => getConnectedIndicator(row.status?.connected),
    },
    {
      key: 'lastSeen',
      header: 'Last Seen',
      accessor: (row) => formatLastSync(row.status?.lastSeen),
      sortable: true,
    },
    {
      key: 'age',
      header: 'Age',
      accessor: (row) =>
        row.metadata?.creationTimestamp
          ? formatAge(row.metadata.creationTimestamp)
          : 'N/A',
      sortable: true,
    },
  ]

  const handleCreate = () => {
    setCurrentRemoteCluster(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (remoteCluster: RemoteCluster) => {
    setCurrentRemoteCluster(remoteCluster)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (remoteCluster: RemoteCluster) => {
    setRemoteClusterToDelete(remoteCluster)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (remoteClusterToDelete) {
      deleteRemoteCluster.mutate({
        namespace: remoteClusterToDelete.metadata?.namespace ?? '',
        name: remoteClusterToDelete.metadata?.name ?? '',
      })
    }
  }

  const handleBulkDelete = () => {
    setBulkDeleteDialogOpen(true)
  }

  const confirmBulkDelete = () => {
    const items = Array.from(selectedRows).map((key) => {
      const [ns, name] = key.split('/')
      return { namespace: ns, name }
    })
    bulkDelete.mutate(items, {
      onSuccess: () => setSelectedRows(new Set()),
    })
  }

  const handleSubmit = async (remoteCluster: RemoteCluster) => {
    if (dialogMode === 'create') {
      await createRemoteCluster.mutateAsync(remoteCluster)
    } else {
      await updateRemoteCluster.mutateAsync({
        namespace: remoteCluster.metadata?.namespace ?? '',
        name: remoteCluster.metadata?.name ?? '',
        rc: remoteCluster,
      })
    }
  }

  const getRowKey = (row: RemoteCluster) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load remote clusters: {error.message}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {!readOnly && selectedRows.size > 0 && (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleBulkDelete}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete ({selectedRows.size})
            </Button>
          )}
        </div>
        {!readOnly && (
          <Button onClick={handleCreate}>
            <Plus className="h-4 w-4 mr-2" />
            Create Remote Cluster
          </Button>
        )}
      </div>

      <DataTable
        data={remoteClusters}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No remote clusters found"
        searchPlaceholder="Search remote clusters..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.endpoint?.toLowerCase().includes(query) ||
          false
        }
        actions={(row) =>
          readOnly
            ? [{ label: 'View', onClick: () => handleEdit(row) }]
            : [
                { label: 'Edit', onClick: () => handleEdit(row) },
                {
                  label: 'Delete',
                  onClick: () => handleDelete(row),
                  variant: 'destructive',
                },
              ]
        }
      />

      <ResourceDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title={
          dialogMode === 'create'
            ? 'Create Remote Cluster'
            : dialogMode === 'edit'
            ? 'Edit Remote Cluster'
            : 'View Remote Cluster'
        }
        description={
          dialogMode === 'create'
            ? 'Register a new remote cluster for federation'
            : undefined
        }
        mode={dialogMode}
        resource={currentRemoteCluster}
        onSubmit={handleSubmit}
        isLoading={createRemoteCluster.isPending || updateRemoteCluster.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_REMOTE_CLUSTER_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Remote Cluster"
        description={`Are you sure you want to delete remote cluster "${remoteClusterToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteRemoteCluster.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Remote Clusters"
        description={`Are you sure you want to delete ${selectedRows.size} remote cluster(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}

// --- Main Federation Page ---

export default function Federation() {
  return (
    <div className="space-y-4">
      <Tabs defaultValue="federations">
        <TabsList>
          <TabsTrigger value="federations">Federations</TabsTrigger>
          <TabsTrigger value="remoteclusters">Remote Clusters</TabsTrigger>
        </TabsList>
        <TabsContent value="federations">
          <FederationsTab />
        </TabsContent>
        <TabsContent value="remoteclusters">
          <RemoteClustersTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
