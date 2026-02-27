import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useClusters,
  useCreateCluster,
  useUpdateCluster,
  useDeleteCluster,
} from '@/api/hooks'
import type { GenericResource } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { toast } from '@/hooks/use-toast'

const DEFAULT_CLUSTER_YAML = `apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: example-cluster
  namespace: nova-system
spec:
  version: "1.0.2"
  replicas:
    controller: 3
    agent: 5
`

function getClusterStatus(resource: GenericResource): { text: string; ready: boolean } {
  const phase = resource.status?.phase as string | undefined
  if (phase) {
    return { text: phase as string, ready: phase === 'Ready' || phase === 'Running' }
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

export default function Clusters() {
  const { namespace, readOnly } = useApp()
  const { data: clusters = [], isLoading, error } = useClusters(namespace)
  const createCluster = useCreateCluster()
  const updateCluster = useUpdateCluster()
  const deleteCluster = useDeleteCluster()

  const queryClient = useQueryClient()
  const bulkDelete = useMutation({
    mutationFn: async (items: { namespace: string; name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => api.clusters.delete(item.namespace, item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      toast({ title: 'Successfully deleted selected clusters', variant: 'success' as const })
    },
    onError: (error: Error) => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      toast({
        title: 'Some cluster deletions failed',
        description: error.message,
        variant: 'destructive',
      })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentCluster, setCurrentCluster] = useState<GenericResource | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [clusterToDelete, setClusterToDelete] = useState<GenericResource | null>(null)
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
      key: 'version',
      header: 'Version',
      accessor: (row) => (row.spec?.version as string) ?? 'N/A',
      sortable: true,
    },
    {
      key: 'imageRepository',
      header: 'Image Repository',
      accessor: (row) => (row.spec?.imageRepository as string) ?? 'default',
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => {
        const { text, ready } = getClusterStatus(row)
        return (
          <Badge
            variant={ready ? 'default' : 'secondary'}
            className={ready ? 'bg-green-500 hover:bg-green-600' : 'bg-yellow-500 hover:bg-yellow-600'}
          >
            {text}
          </Badge>
        )
      },
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
    setCurrentCluster(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (cluster: GenericResource) => {
    setCurrentCluster(cluster)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (cluster: GenericResource) => {
    setClusterToDelete(cluster)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (clusterToDelete) {
      deleteCluster.mutate({
        namespace: clusterToDelete.metadata?.namespace ?? '',
        name: clusterToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (cluster: GenericResource) => {
    if (dialogMode === 'create') {
      await createCluster.mutateAsync(cluster)
    } else {
      await updateCluster.mutateAsync({
        namespace: cluster.metadata?.namespace ?? '',
        name: cluster.metadata?.name ?? '',
        cluster,
      })
    }
  }

  const getRowKey = (row: GenericResource) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load clusters: {error.message}
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
            Create Cluster
          </Button>
        )}
      </div>

      <DataTable
        data={clusters}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No clusters found"
        searchPlaceholder="Search clusters..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          (row.spec?.version as string)?.toLowerCase().includes(query) ||
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
            ? 'Create Cluster'
            : dialogMode === 'edit'
            ? 'Edit Cluster'
            : 'View Cluster'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new NovaEdge cluster configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentCluster}
        onSubmit={handleSubmit}
        isLoading={createCluster.isPending || updateCluster.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_CLUSTER_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Cluster"
        description={`Are you sure you want to delete cluster "${clusterToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteCluster.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Clusters"
        description={`Are you sure you want to delete ${selectedRows.size} cluster(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
