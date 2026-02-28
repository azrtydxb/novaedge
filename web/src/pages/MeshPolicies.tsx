import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { GenericResource } from '@/api/types'
import { useApp } from '@/contexts/AppContext'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'
import { toast } from '@/hooks/use-toast'

const API_BASE = '/api/v1'

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(error.error || 'Request failed')
  }
  return response.json()
}

const meshPoliciesAPI = {
  list: async (namespace: string = 'all'): Promise<GenericResource[]> => {
    return fetchJSON<GenericResource[]>(`${API_BASE}/mesh/policies?namespace=${namespace}`)
  },
  get: async (namespace: string, name: string): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/mesh/policies/${namespace}/${name}`)
  },
  create: async (resource: GenericResource): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/mesh/policies`, {
      method: 'POST',
      body: JSON.stringify(resource),
    })
  },
  update: async (namespace: string, name: string, resource: GenericResource): Promise<GenericResource> => {
    return fetchJSON<GenericResource>(`${API_BASE}/mesh/policies/${namespace}/${name}`, {
      method: 'PUT',
      body: JSON.stringify(resource),
    })
  },
  delete: async (namespace: string, name: string): Promise<void> => {
    await fetchJSON<{ status: string }>(`${API_BASE}/mesh/policies/${namespace}/${name}`, { method: 'DELETE' })
  },
}

const DEFAULT_MESH_POLICY_YAML = `apiVersion: novaedge.io/v1alpha1
kind: MeshAuthorizationPolicy
metadata:
  name: example-policy
  namespace: default
spec:
  source:
    service: frontend
  destination:
    service: backend
  rules:
    - methods: ["GET", "POST"]
      paths: ["/api/*"]
`

export default function MeshPolicies() {
  const { namespace, readOnly } = useApp()
  const queryClient = useQueryClient()

  const { data: policies = [], isLoading, error } = useQuery<GenericResource[]>({
    queryKey: ['meshPolicies', namespace],
    queryFn: () => meshPoliciesAPI.list(namespace),
  })

  const createPolicy = useMutation({
    mutationFn: (policy: GenericResource) => meshPoliciesAPI.create(policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['meshPolicies'] })
      toast({ title: 'Mesh policy created successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to create mesh policy', description: error.message, variant: 'destructive' })
    },
  })

  const updatePolicy = useMutation({
    mutationFn: ({ ns, name, policy }: { ns: string; name: string; policy: GenericResource }) =>
      meshPoliciesAPI.update(ns, name, policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['meshPolicies'] })
      toast({ title: 'Mesh policy updated successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to update mesh policy', description: error.message, variant: 'destructive' })
    },
  })

  const deletePolicy = useMutation({
    mutationFn: ({ ns, name }: { ns: string; name: string }) =>
      meshPoliciesAPI.delete(ns, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['meshPolicies'] })
      toast({ title: 'Mesh policy deleted successfully', variant: 'success' as const })
    },
    onError: (error: Error) => {
      toast({ title: 'Failed to delete mesh policy', description: error.message, variant: 'destructive' })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentPolicy, setCurrentPolicy] = useState<GenericResource | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [policyToDelete, setPolicyToDelete] = useState<GenericResource | null>(null)
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
      key: 'source',
      header: 'Source Service',
      accessor: (row) => {
        const spec = row.spec as Record<string, unknown> | undefined
        const source = spec?.source as Record<string, unknown> | undefined
        return (source?.service as string) ?? '-'
      },
    },
    {
      key: 'destination',
      header: 'Destination Service',
      accessor: (row) => {
        const spec = row.spec as Record<string, unknown> | undefined
        const destination = spec?.destination as Record<string, unknown> | undefined
        return (destination?.service as string) ?? '-'
      },
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => {
        const status = row.status as Record<string, unknown> | undefined
        const conditions = status?.conditions as Array<Record<string, unknown>> | undefined
        const ready = conditions?.some((c) => c.type === 'Ready' && c.status === 'True')
        return (
          <Badge
            variant={ready ? 'default' : 'secondary'}
            className={ready ? 'bg-green-500 hover:bg-green-600' : ''}
          >
            {ready ? 'Active' : 'Inactive'}
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
          : '-',
      sortable: true,
    },
  ]

  const handleCreate = () => {
    setCurrentPolicy(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (policy: GenericResource) => {
    setCurrentPolicy(policy)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (policy: GenericResource) => {
    setPolicyToDelete(policy)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (policyToDelete) {
      deletePolicy.mutate({
        ns: policyToDelete.metadata?.namespace ?? '',
        name: policyToDelete.metadata?.name ?? '',
      })
    }
  }

  const confirmBulkDelete = async () => {
    const items = Array.from(selectedRows).map((key) => {
      const [ns, name] = key.split('/')
      return { ns, name }
    })
    const results = await Promise.allSettled(
      items.map((item) => meshPoliciesAPI.delete(item.ns, item.name))
    )
    const failures = results.filter((r) => r.status === 'rejected')
    if (failures.length > 0) {
      toast({
        title: `${failures.length} deletion(s) failed`,
        variant: 'destructive',
      })
    } else {
      toast({
        title: 'Selected mesh policies deleted',
        variant: 'success' as const,
      })
    }
    queryClient.invalidateQueries({ queryKey: ['meshPolicies'] })
    setSelectedRows(new Set())
  }

  const handleSubmit = async (policy: GenericResource) => {
    if (dialogMode === 'create') {
      await createPolicy.mutateAsync(policy)
    } else {
      await updatePolicy.mutateAsync({
        ns: policy.metadata?.namespace ?? '',
        name: policy.metadata?.name ?? '',
        policy,
      })
    }
  }

  const getRowKey = (row: GenericResource) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load mesh policies: {(error as Error).message}
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
              onClick={() => setBulkDeleteDialogOpen(true)}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete ({selectedRows.size})
            </Button>
          )}
        </div>
        {!readOnly && (
          <Button onClick={handleCreate}>
            <Plus className="h-4 w-4 mr-2" />
            Create Mesh Policy
          </Button>
        )}
      </div>

      <DataTable
        data={policies}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No mesh policies found"
        searchPlaceholder="Search mesh policies..."
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
            ? 'Create Mesh Policy'
            : dialogMode === 'edit'
            ? 'Edit Mesh Policy'
            : 'View Mesh Policy'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new mesh authorization policy'
            : undefined
        }
        mode={dialogMode}
        resource={currentPolicy}
        onSubmit={handleSubmit}
        isLoading={createPolicy.isPending || updatePolicy.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_MESH_POLICY_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Mesh Policy"
        description={`Are you sure you want to delete mesh policy "${policyToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deletePolicy.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Mesh Policies"
        description={`Are you sure you want to delete ${selectedRows.size} mesh policy/policies? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
      />
    </div>
  )
}
