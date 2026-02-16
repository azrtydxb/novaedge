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
import type { GenericResource } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Plus, Trash2 } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { toast } from '@/hooks/use-toast'

const DEFAULT_FEDERATION_YAML = `apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeFederation
metadata:
  name: example-federation
  namespace: novaedge-system
spec:
  mode: hub-spoke
  clusters: []
`

const DEFAULT_REMOTE_CLUSTER_YAML = `apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeRemoteCluster
metadata:
  name: remote-cluster-1
  namespace: novaedge-system
spec:
  endpoint: https://remote-cluster:6443
  secretRef:
    name: remote-kubeconfig
`

function getGenericStatus(resource: GenericResource): { text: string; ready: boolean } {
  const phase = resource.status?.phase as string | undefined
  if (phase) {
    return { text: phase, ready: phase === 'Ready' || phase === 'Running' || phase === 'Active' }
  }

  const conditions = resource.status?.conditions as Array<{ type: string; status: string }> | undefined
  if (conditions) {
    const readyCondition = conditions.find((c) => c.type === 'Ready')
    if (readyCondition) {
      return { text: readyCondition.status === 'True' ? 'Ready' : 'Not Ready', ready: readyCondition.status === 'True' }
    }
  }

  return { text: 'Unknown', ready: false }
}

function formatLastSync(lastSyncTime: unknown): string {
  if (!lastSyncTime || typeof lastSyncTime !== 'string') return 'N/A'
  try {
    return new Date(lastSyncTime).toLocaleString()
  } catch {
    return String(lastSyncTime)
  }
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
  const [currentFederation, setCurrentFederation] = useState<GenericResource | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [federationToDelete, setFederationToDelete] = useState<GenericResource | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<GenericResource>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.metadata?.name ?? '-',
      sortable: true,
    },
    {
      key: 'namespace',
      header: 'Namespace',
      accessor: (row) => (
        <Badge variant="outline">{row.metadata?.namespace ?? '-'}</Badge>
      ),
      sortable: true,
    },
    {
      key: 'mode',
      header: 'Mode',
      accessor: (row) => (row.spec?.mode as string) ?? '-',
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => {
        const { text, ready } = getGenericStatus(row)
        return (
          <Badge
            variant={ready ? 'default' : 'secondary'}
            className={ready ? 'bg-green-500 hover:bg-green-600' : ''}
          >
            {text}
          </Badge>
        )
      },
    },
    {
      key: 'clusters',
      header: 'Clusters',
      accessor: (row) => {
        const clusters = row.spec?.clusters as unknown[] | undefined
        const statusClusters = row.status?.clusters as unknown[] | undefined
        const count = clusters?.length ?? statusClusters?.length ?? 0
        return count
      },
    },
  ]

  const handleCreate = () => {
    setCurrentFederation(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (federation: GenericResource) => {
    setCurrentFederation(federation)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (federation: GenericResource) => {
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

  const handleSubmit = async (federation: GenericResource) => {
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

  const getRowKey = (row: GenericResource) =>
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
          (row.spec?.mode as string)?.toLowerCase().includes(query) ||
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
  const [currentRemoteCluster, setCurrentRemoteCluster] = useState<GenericResource | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [remoteClusterToDelete, setRemoteClusterToDelete] = useState<GenericResource | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<GenericResource>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.metadata?.name ?? '-',
      sortable: true,
    },
    {
      key: 'namespace',
      header: 'Namespace',
      accessor: (row) => (
        <Badge variant="outline">{row.metadata?.namespace ?? '-'}</Badge>
      ),
      sortable: true,
    },
    {
      key: 'endpoint',
      header: 'Endpoint',
      accessor: (row) => (row.spec?.endpoint as string) ?? '-',
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => {
        const { text, ready } = getGenericStatus(row)
        return (
          <Badge
            variant={ready ? 'default' : 'secondary'}
            className={ready ? 'bg-green-500 hover:bg-green-600' : ''}
          >
            {text}
          </Badge>
        )
      },
    },
    {
      key: 'lastSync',
      header: 'Last Sync',
      accessor: (row) => formatLastSync(row.status?.lastSyncTime),
      sortable: true,
    },
  ]

  const handleCreate = () => {
    setCurrentRemoteCluster(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (remoteCluster: GenericResource) => {
    setCurrentRemoteCluster(remoteCluster)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (remoteCluster: GenericResource) => {
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

  const handleSubmit = async (remoteCluster: GenericResource) => {
    if (dialogMode === 'create') {
      await createRemoteCluster.mutateAsync(remoteCluster)
    } else {
      await updateRemoteCluster.mutateAsync({
        namespace: remoteCluster.metadata?.namespace ?? '',
        name: remoteCluster.metadata?.name ?? '',
        remoteCluster,
      })
    }
  }

  const getRowKey = (row: GenericResource) =>
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
          (row.spec?.endpoint as string)?.toLowerCase().includes(query) ||
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
