import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useIPPools,
  useCreateIPPool,
  useUpdateIPPool,
  useDeleteIPPool,
} from '@/api/hooks'
import type { IPPool } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { toast } from '@/hooks/use-toast'

const DEFAULT_IPPOOL_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyIPPool
metadata:
  name: example-pool
spec:
  cidrs:
    - 192.168.100.0/24
  protocol: IPv4
  autoAssign: true
`

function getUtilizationColor(percent: number): string {
  if (percent >= 90) return 'bg-red-500'
  if (percent >= 70) return 'bg-yellow-500'
  return 'bg-green-500'
}

export default function IPPools() {
  const { readOnly } = useApp()
  const { data: ippools = [], isLoading, error } = useIPPools()
  const createIPPool = useCreateIPPool()
  const updateIPPool = useUpdateIPPool()
  const deleteIPPool = useDeleteIPPool()

  const queryClient = useQueryClient()
  const bulkDelete = useMutation({
    mutationFn: async (items: { name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => api.ippools.delete(item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ippools'] })
      toast({ title: 'Successfully deleted selected IP pools', variant: 'success' as const })
    },
    onError: (error: Error) => {
      queryClient.invalidateQueries({ queryKey: ['ippools'] })
      toast({
        title: 'Some IP pool deletions failed',
        description: error.message,
        variant: 'destructive',
      })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentIPPool, setCurrentIPPool] = useState<IPPool | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [ippoolToDelete, setIppoolToDelete] = useState<IPPool | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<IPPool>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.metadata?.name ?? '-',
      sortable: true,
    },
    {
      key: 'cidr',
      header: 'CIDR',
      accessor: (row) => {
        const cidrs = row.spec?.cidrs ?? []
        const addresses = row.spec?.addresses ?? []
        if (cidrs.length > 0) return cidrs.join(', ')
        if (addresses.length > 0) return addresses.join(', ')
        return '-'
      },
    },
    {
      key: 'protocol',
      header: 'Protocol',
      accessor: (row) => {
        const protocol = row.spec?.protocol ?? 'IPv4'
        return (
          <Badge className={protocol === 'IPv6' ? 'bg-purple-500 hover:bg-purple-600' : 'bg-blue-500 hover:bg-blue-600'}>
            {protocol}
          </Badge>
        )
      },
    },
    {
      key: 'allocated',
      header: 'Allocated',
      accessor: (row) => row.status?.allocated ?? 0,
      sortable: true,
    },
    {
      key: 'total',
      header: 'Total',
      accessor: (row) => row.status?.total ?? 0,
      sortable: true,
    },
    {
      key: 'utilization',
      header: 'Utilization',
      accessor: (row) => {
        const allocated = row.status?.allocated ?? 0
        const total = row.status?.total ?? 0
        if (total === 0) return <span className="text-muted-foreground">N/A</span>
        const percent = Math.round((allocated / total) * 100)
        return (
          <div className="flex items-center gap-2">
            <div className="w-24 h-2 rounded-full bg-muted overflow-hidden">
              <div
                className={`h-full rounded-full ${getUtilizationColor(percent)}`}
                style={{ width: `${Math.min(percent, 100)}%` }}
              />
            </div>
            <span className="text-sm text-muted-foreground">{percent}%</span>
          </div>
        )
      },
    },
  ]

  const handleCreate = () => {
    setCurrentIPPool(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (ippool: IPPool) => {
    setCurrentIPPool(ippool)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (ippool: IPPool) => {
    setIppoolToDelete(ippool)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (ippoolToDelete) {
      deleteIPPool.mutate({
        name: ippoolToDelete.metadata?.name ?? '',
      })
    }
  }

  const handleBulkDelete = () => {
    setBulkDeleteDialogOpen(true)
  }

  const confirmBulkDelete = () => {
    const items = Array.from(selectedRows).map((key) => ({ name: key }))
    bulkDelete.mutate(items, {
      onSuccess: () => setSelectedRows(new Set()),
    })
  }

  const handleSubmit = async (ippool: IPPool) => {
    if (dialogMode === 'create') {
      await createIPPool.mutateAsync(ippool)
    } else {
      await updateIPPool.mutateAsync({
        name: ippool.metadata?.name ?? '',
        ippool,
      })
    }
  }

  const getRowKey = (row: IPPool) => row.metadata?.name ?? ''

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load IP pools: {error.message}
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
            Create IP Pool
          </Button>
        )}
      </div>

      <DataTable
        data={ippools}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No IP pools found"
        searchPlaceholder="Search IP pools..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.spec?.cidrs?.some((c) => c.toLowerCase().includes(query)) ||
          row.spec?.addresses?.some((a) => a.toLowerCase().includes(query)) ||
          row.spec?.protocol?.toLowerCase().includes(query) ||
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
            ? 'Create IP Pool'
            : dialogMode === 'edit'
            ? 'Edit IP Pool'
            : 'View IP Pool'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new IP address pool'
            : undefined
        }
        mode={dialogMode}
        resource={currentIPPool}
        onSubmit={handleSubmit}
        isLoading={createIPPool.isPending || updateIPPool.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_IPPOOL_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete IP Pool"
        description={`Are you sure you want to delete IP pool "${ippoolToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteIPPool.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected IP Pools"
        description={`Are you sure you want to delete ${selectedRows.size} IP pool(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
