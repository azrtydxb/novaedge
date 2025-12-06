import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useVIPs,
  useCreateVIP,
  useUpdateVIP,
  useDeleteVIP,
  useBulkDelete,
} from '@/api/hooks'
import type { VIP } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'

const DEFAULT_VIP_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: my-vip
  namespace: default
spec:
  address: 192.168.1.100
  mode: L2
  interface: eth0
  gatewayRef:
    name: my-gateway
    namespace: default
`

export default function VIPs() {
  const { namespace, readOnly } = useApp()
  const { data: vips = [], isLoading, error } = useVIPs(namespace)
  const createVIP = useCreateVIP()
  const updateVIP = useUpdateVIP()
  const deleteVIP = useDeleteVIP()
  const bulkDelete = useBulkDelete('vips')

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentVIP, setCurrentVIP] = useState<VIP | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [vipToDelete, setVIPToDelete] = useState<VIP | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const getModeVariant = (mode: string | undefined) => {
    switch (mode) {
      case 'L2':
        return 'default'
      case 'BGP':
        return 'secondary'
      case 'OSPF':
        return 'outline'
      default:
        return 'secondary'
    }
  }

  const columns: Column<VIP>[] = [
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
      key: 'address',
      header: 'Address',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-sm">
          {row.spec?.address ?? '-'}
        </code>
      ),
    },
    {
      key: 'mode',
      header: 'Mode',
      accessor: (row) => (
        <Badge variant={getModeVariant(row.spec?.mode)}>
          {row.spec?.mode ?? 'L2'}
        </Badge>
      ),
    },
    {
      key: 'interface',
      header: 'Interface',
      accessor: (row) => row.spec?.interface ?? '-',
    },
    {
      key: 'gateway',
      header: 'Gateway',
      accessor: (row) => row.spec?.gatewayRef?.name ?? '-',
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => (
        <Badge
          variant={row.status?.bound ? 'default' : 'secondary'}
          className={row.status?.bound ? 'bg-green-500 hover:bg-green-600' : ''}
        >
          {row.status?.bound ? 'Bound' : 'Unbound'}
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
    setCurrentVIP(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (vip: VIP) => {
    setCurrentVIP(vip)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (vip: VIP) => {
    setVIPToDelete(vip)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (vipToDelete) {
      deleteVIP.mutate({
        namespace: vipToDelete.metadata?.namespace ?? '',
        name: vipToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (vip: VIP) => {
    if (dialogMode === 'create') {
      await createVIP.mutateAsync(vip)
    } else {
      await updateVIP.mutateAsync({
        namespace: vip.metadata?.namespace ?? '',
        name: vip.metadata?.name ?? '',
        vip,
      })
    }
  }

  const getRowKey = (row: VIP) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load VIPs: {error.message}
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
            Create VIP
          </Button>
        )}
      </div>

      <DataTable
        data={vips}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No VIPs found"
        searchPlaceholder="Search VIPs..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.address?.toLowerCase().includes(query) ||
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
            ? 'Create VIP'
            : dialogMode === 'edit'
            ? 'Edit VIP'
            : 'View VIP'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new Virtual IP configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentVIP}
        onSubmit={handleSubmit}
        isLoading={createVIP.isPending || updateVIP.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_VIP_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete VIP"
        description={`Are you sure you want to delete VIP "${vipToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteVIP.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected VIPs"
        description={`Are you sure you want to delete ${selectedRows.size} VIP(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
