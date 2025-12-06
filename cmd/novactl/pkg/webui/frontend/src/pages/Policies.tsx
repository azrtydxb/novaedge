import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  usePolicies,
  useCreatePolicy,
  useUpdatePolicy,
  useDeletePolicy,
  useBulkDelete,
} from '@/api/hooks'
import type { Policy } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { formatAge } from '@/lib/utils'

const DEFAULT_POLICY_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: my-policy
  namespace: default
spec:
  targetRef:
    group: novaedge.io
    kind: ProxyRoute
    name: my-route
  rateLimit:
    requestsPerSecond: 100
    burst: 50
  cors:
    allowOrigins:
      - "*"
    allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
    allowHeaders:
      - Content-Type
      - Authorization
    maxAge: 86400
`

export default function Policies() {
  const { namespace, readOnly } = useApp()
  const { data: policies = [], isLoading, error } = usePolicies(namespace)
  const createPolicy = useCreatePolicy()
  const updatePolicy = useUpdatePolicy()
  const deletePolicy = useDeletePolicy()
  const bulkDelete = useBulkDelete('policies')

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentPolicy, setCurrentPolicy] = useState<Policy | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [policyToDelete, setPolicyToDelete] = useState<Policy | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const getPolicyTypes = (policy: Policy) => {
    const types: string[] = []
    if (policy.spec?.rateLimit) types.push('Rate Limit')
    if (policy.spec?.cors) types.push('CORS')
    if (policy.spec?.ipFilter) types.push('IP Filter')
    if (policy.spec?.jwt) types.push('JWT')
    return types
  }

  const columns: Column<Policy>[] = [
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
      key: 'target',
      header: 'Target',
      accessor: (row) => {
        const ref = row.spec?.targetRef
        return ref ? `${ref.kind}/${ref.name}` : '-'
      },
    },
    {
      key: 'types',
      header: 'Policy Types',
      accessor: (row) => {
        const types = getPolicyTypes(row)
        return types.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {types.map((type) => (
              <Badge key={type} variant="secondary" className="text-xs">
                {type}
              </Badge>
            ))}
          </div>
        ) : (
          '-'
        )
      },
    },
    {
      key: 'rateLimit',
      header: 'Rate Limit',
      accessor: (row) =>
        row.spec?.rateLimit
          ? `${row.spec.rateLimit.requestsPerSecond} req/s`
          : '-',
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

  const handleEdit = (policy: Policy) => {
    setCurrentPolicy(policy)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (policy: Policy) => {
    setPolicyToDelete(policy)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (policyToDelete) {
      deletePolicy.mutate({
        namespace: policyToDelete.metadata?.namespace ?? '',
        name: policyToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (policy: Policy) => {
    if (dialogMode === 'create') {
      await createPolicy.mutateAsync(policy)
    } else {
      await updatePolicy.mutateAsync({
        namespace: policy.metadata?.namespace ?? '',
        name: policy.metadata?.name ?? '',
        policy,
      })
    }
  }

  const getRowKey = (row: Policy) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load policies: {error.message}
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
            Create Policy
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
        emptyMessage="No policies found"
        searchPlaceholder="Search policies..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.targetRef?.name?.toLowerCase().includes(query) ||
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
            ? 'Create Policy'
            : dialogMode === 'edit'
            ? 'Edit Policy'
            : 'View Policy'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new policy configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentPolicy}
        onSubmit={handleSubmit}
        isLoading={createPolicy.isPending || updatePolicy.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_POLICY_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Policy"
        description={`Are you sure you want to delete policy "${policyToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deletePolicy.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Policies"
        description={`Are you sure you want to delete ${selectedRows.size} policy(ies)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
