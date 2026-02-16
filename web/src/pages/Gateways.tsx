import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useGateways,
  useCreateGateway,
  useUpdateGateway,
  useDeleteGateway,
  useBulkDelete,
} from '@/api/hooks'
import type { Gateway } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'

const DEFAULT_GATEWAY_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
  namespace: default
spec:
  listeners:
    - name: http
      port: 80
      protocol: HTTP
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: TERMINATE
        certificateRefs:
          - name: my-tls-secret
`

export default function Gateways() {
  const { namespace, readOnly } = useApp()
  const { data: gateways = [], isLoading, error } = useGateways(namespace)
  const createGateway = useCreateGateway()
  const updateGateway = useUpdateGateway()
  const deleteGateway = useDeleteGateway()
  const bulkDelete = useBulkDelete('gateways')

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentGateway, setCurrentGateway] = useState<Gateway | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [gatewayToDelete, setGatewayToDelete] = useState<Gateway | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<Gateway>[] = [
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
      key: 'listeners',
      header: 'Listeners',
      accessor: (row) => {
        const listeners = row.spec?.listeners ?? []
        return listeners.length > 0
          ? listeners.map((l) => `${l.name}:${l.port}`).join(', ')
          : '-'
      },
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => (
        <Badge
          variant={row.status?.ready ? 'default' : 'secondary'}
          className={row.status?.ready ? 'bg-green-500 hover:bg-green-600' : ''}
        >
          {row.status?.ready ? 'Ready' : 'Not Ready'}
        </Badge>
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
    setCurrentGateway(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (gateway: Gateway) => {
    setCurrentGateway(gateway)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (gateway: Gateway) => {
    setGatewayToDelete(gateway)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (gatewayToDelete) {
      deleteGateway.mutate({
        namespace: gatewayToDelete.metadata?.namespace ?? '',
        name: gatewayToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (gateway: Gateway) => {
    if (dialogMode === 'create') {
      await createGateway.mutateAsync(gateway)
    } else {
      await updateGateway.mutateAsync({
        namespace: gateway.metadata?.namespace ?? '',
        name: gateway.metadata?.name ?? '',
        gateway,
      })
    }
  }

  const getRowKey = (row: Gateway) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load gateways: {error.message}
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
            Create Gateway
          </Button>
        )}
      </div>

      <DataTable
        data={gateways}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No gateways found"
        searchPlaceholder="Search gateways..."
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
            ? 'Create Gateway'
            : dialogMode === 'edit'
            ? 'Edit Gateway'
            : 'View Gateway'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new gateway configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentGateway}
        onSubmit={handleSubmit}
        isLoading={createGateway.isPending || updateGateway.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_GATEWAY_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Gateway"
        description={`Are you sure you want to delete gateway "${gatewayToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteGateway.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Gateways"
        description={`Are you sure you want to delete ${selectedRows.size} gateway(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
