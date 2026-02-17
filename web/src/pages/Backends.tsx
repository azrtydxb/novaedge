import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useBackends,
  useCreateBackend,
  useUpdateBackend,
  useDeleteBackend,
  useBulkDelete,
} from '@/api/hooks'
import type { Backend } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'

const DEFAULT_BACKEND_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: my-backend
  namespace: default
spec:
  endpoints:
    - address: 10.0.0.1
      port: 8080
    - address: 10.0.0.2
      port: 8080
  loadBalancer:
    algorithm: RoundRobin
  healthCheck:
    enabled: true
    path: /health
    interval: 10s
    timeout: 5s
    healthyThreshold: 2
    unhealthyThreshold: 3
`

export default function Backends() {
  const { namespace, readOnly } = useApp()
  const { data: backends = [], isLoading, error } = useBackends(namespace)
  const createBackend = useCreateBackend()
  const updateBackend = useUpdateBackend()
  const deleteBackend = useDeleteBackend()
  const bulkDelete = useBulkDelete('backends')

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentBackend, setCurrentBackend] = useState<Backend | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [backendToDelete, setBackendToDelete] = useState<Backend | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<Backend>[] = [
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
      key: 'endpoints',
      header: 'Endpoints',
      accessor: (row) => {
        const endpoints = row.spec?.endpoints ?? []
        const healthy = endpoints.filter((e) => e.healthy !== false).length
        return (
          <span>
            {healthy}/{endpoints.length} healthy
          </span>
        )
      },
    },
    {
      key: 'algorithm',
      header: 'LB Algorithm',
      accessor: (row) => (
        <Badge variant="secondary">
          {row.spec?.loadBalancer?.algorithm ?? 'RoundRobin'}
        </Badge>
      ),
    },
    {
      key: 'healthCheck',
      header: 'Health Check',
      accessor: (row) => (
        <Badge
          variant={row.spec?.healthCheck?.enabled ? 'default' : 'secondary'}
          className={row.spec?.healthCheck?.enabled ? 'bg-green-500 hover:bg-green-600' : ''}
        >
          {row.spec?.healthCheck?.enabled ? 'Enabled' : 'Disabled'}
        </Badge>
      ),
    },
    {
      key: 'slowStart',
      header: 'Slow Start',
      accessor: (row) => row.spec?.slowStart?.window ?? '-',
    },
    {
      key: 'outlierDetection',
      header: 'Outlier Detection',
      accessor: (row) =>
        row.spec?.outlierDetection ? (
          <Badge className="bg-blue-500 hover:bg-blue-600">Enabled</Badge>
        ) : (
          '-'
        ),
    },
    {
      key: 'age',
      header: 'Age',
      accessor: (row) =>
        row.metadata?.creationTimestamp
          ? formatAge(row.metadata.creationTimestamp)
          : '-',
      sortable: true,
    },
  ]

  const handleCreate = () => {
    setCurrentBackend(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (backend: Backend) => {
    setCurrentBackend(backend)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (backend: Backend) => {
    setBackendToDelete(backend)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (backendToDelete) {
      deleteBackend.mutate({
        namespace: backendToDelete.metadata?.namespace ?? '',
        name: backendToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (backend: Backend) => {
    if (dialogMode === 'create') {
      await createBackend.mutateAsync(backend)
    } else {
      await updateBackend.mutateAsync({
        namespace: backend.metadata?.namespace ?? '',
        name: backend.metadata?.name ?? '',
        backend,
      })
    }
  }

  const getRowKey = (row: Backend) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load backends: {error.message}
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
            Create Backend
          </Button>
        )}
      </div>

      <DataTable
        data={backends}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No backends found"
        searchPlaceholder="Search backends..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
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
            ? 'Create Backend'
            : dialogMode === 'edit'
            ? 'Edit Backend'
            : 'View Backend'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new backend configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentBackend}
        onSubmit={handleSubmit}
        isLoading={createBackend.isPending || updateBackend.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_BACKEND_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Backend"
        description={`Are you sure you want to delete backend "${backendToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteBackend.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Backends"
        description={`Are you sure you want to delete ${selectedRows.size} backend(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
