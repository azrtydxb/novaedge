import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useRoutes,
  useCreateRoute,
  useUpdateRoute,
  useDeleteRoute,
  useBulkDelete,
} from '@/api/hooks'
import type { Route } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'

const DEFAULT_ROUTE_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: my-route
  namespace: default
spec:
  parentRefs:
    - name: my-gateway
      namespace: default
  hostnames:
    - example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: my-backend
          namespace: default
          weight: 100
`

export default function Routes() {
  const { namespace, readOnly } = useApp()
  const { data: routes = [], isLoading, error } = useRoutes(namespace)
  const createRoute = useCreateRoute()
  const updateRoute = useUpdateRoute()
  const deleteRoute = useDeleteRoute()
  const bulkDelete = useBulkDelete('routes')

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentRoute, setCurrentRoute] = useState<Route | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [routeToDelete, setRouteToDelete] = useState<Route | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<Route>[] = [
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
      key: 'hostnames',
      header: 'Hostnames',
      accessor: (row) => {
        const hostnames = row.spec?.hostnames ?? []
        return hostnames.length > 0 ? hostnames.join(', ') : '*'
      },
    },
    {
      key: 'parentRef',
      header: 'Gateway',
      accessor: (row) => {
        const refs = row.spec?.parentRefs ?? []
        return refs.length > 0 ? refs.map((r) => r.name).join(', ') : '-'
      },
    },
    {
      key: 'rules',
      header: 'Rules',
      accessor: (row) => row.spec?.rules?.length ?? 0,
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
    setCurrentRoute(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (route: Route) => {
    setCurrentRoute(route)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (route: Route) => {
    setRouteToDelete(route)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (routeToDelete) {
      deleteRoute.mutate({
        namespace: routeToDelete.metadata?.namespace ?? '',
        name: routeToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (route: Route) => {
    if (dialogMode === 'create') {
      await createRoute.mutateAsync(route)
    } else {
      await updateRoute.mutateAsync({
        namespace: route.metadata?.namespace ?? '',
        name: route.metadata?.name ?? '',
        route,
      })
    }
  }

  const getRowKey = (row: Route) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load routes: {error.message}
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
            Create Route
          </Button>
        )}
      </div>

      <DataTable
        data={routes}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No routes found"
        searchPlaceholder="Search routes..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.hostnames?.some((h) => h.toLowerCase().includes(query)) ||
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
            ? 'Create Route'
            : dialogMode === 'edit'
            ? 'Edit Route'
            : 'View Route'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new route configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentRoute}
        onSubmit={handleSubmit}
        isLoading={createRoute.isPending || updateRoute.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_ROUTE_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Route"
        description={`Are you sure you want to delete route "${routeToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteRoute.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Routes"
        description={`Are you sure you want to delete ${selectedRows.size} route(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
