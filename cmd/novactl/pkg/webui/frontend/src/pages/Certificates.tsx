import { useState } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useCertificates,
  useCreateCertificate,
  useUpdateCertificate,
  useDeleteCertificate,
} from '@/api/hooks'
import type { Certificate } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2 } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { toast } from '@/hooks/use-toast'

const DEFAULT_CERTIFICATE_YAML = `apiVersion: novaedge.io/v1alpha1
kind: ProxyCertificate
metadata:
  name: example-cert
  namespace: default
spec:
  domains:
    - example.com
    - "*.example.com"
  issuer: acme
  secretName: example-cert-tls
  renewBefore: 720h
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    challenge: http-01
`

function getIssuerBadge(issuer: string | undefined) {
  switch (issuer) {
    case 'acme':
      return <Badge className="bg-blue-500 hover:bg-blue-600">acme</Badge>
    case 'cert-manager':
      return <Badge className="bg-green-500 hover:bg-green-600">cert-manager</Badge>
    case 'vault':
      return <Badge className="bg-purple-500 hover:bg-purple-600">vault</Badge>
    case 'manual':
      return <Badge variant="secondary">manual</Badge>
    default:
      return <Badge variant="secondary">{issuer ?? '-'}</Badge>
  }
}

function getDaysLeft(notAfter: string | undefined): { text: string; className: string } {
  if (!notAfter) return { text: 'N/A', className: '' }

  const expiry = new Date(notAfter)
  const now = new Date()
  const diffMs = expiry.getTime() - now.getTime()
  const days = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (days < 0) return { text: 'Expired', className: 'text-red-500 font-semibold' }
  if (days < 7) return { text: `${days}d`, className: 'text-red-500 font-semibold' }
  if (days < 30) return { text: `${days}d`, className: 'text-yellow-500 font-semibold' }
  return { text: `${days}d`, className: 'text-green-500' }
}

function formatExpiry(notAfter: string | undefined): string {
  if (!notAfter) return 'N/A'
  try {
    return new Date(notAfter).toLocaleDateString()
  } catch {
    return notAfter
  }
}

export default function Certificates() {
  const { namespace, readOnly } = useApp()
  const { data: certificates = [], isLoading, error } = useCertificates(namespace)
  const createCertificate = useCreateCertificate()
  const updateCertificate = useUpdateCertificate()
  const deleteCertificate = useDeleteCertificate()

  const queryClient = useQueryClient()
  const bulkDelete = useMutation({
    mutationFn: async (items: { namespace: string; name: string }[]) => {
      const results = await Promise.allSettled(
        items.map((item) => api.certificates.delete(item.namespace, item.name))
      )
      const failures = results.filter((r) => r.status === 'rejected')
      if (failures.length > 0) {
        throw new Error(`${failures.length} deletion(s) failed`)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast({ title: 'Successfully deleted selected certificates', variant: 'success' as const })
    },
    onError: (error: Error) => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast({
        title: 'Some certificate deletions failed',
        description: error.message,
        variant: 'destructive',
      })
    },
  })

  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<'create' | 'edit' | 'view'>('create')
  const [currentCertificate, setCurrentCertificate] = useState<Certificate | undefined>()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [certificateToDelete, setCertificateToDelete] = useState<Certificate | null>(null)
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false)

  const columns: Column<Certificate>[] = [
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
      key: 'domains',
      header: 'Domains',
      accessor: (row) => {
        const domains = row.spec?.domains ?? []
        return domains.length > 0 ? domains.join(', ') : '-'
      },
    },
    {
      key: 'issuer',
      header: 'Issuer',
      accessor: (row) => getIssuerBadge(row.spec?.issuer),
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
      key: 'expiry',
      header: 'Expiry',
      accessor: (row) => formatExpiry(row.status?.notAfter),
      sortable: true,
    },
    {
      key: 'daysLeft',
      header: 'Days Left',
      accessor: (row) => {
        const { text, className } = getDaysLeft(row.status?.notAfter)
        return <span className={className}>{text}</span>
      },
    },
  ]

  const handleCreate = () => {
    setCurrentCertificate(undefined)
    setDialogMode('create')
    setDialogOpen(true)
  }

  const handleEdit = (certificate: Certificate) => {
    setCurrentCertificate(certificate)
    setDialogMode(readOnly ? 'view' : 'edit')
    setDialogOpen(true)
  }

  const handleDelete = (certificate: Certificate) => {
    setCertificateToDelete(certificate)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = () => {
    if (certificateToDelete) {
      deleteCertificate.mutate({
        namespace: certificateToDelete.metadata?.namespace ?? '',
        name: certificateToDelete.metadata?.name ?? '',
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

  const handleSubmit = async (certificate: Certificate) => {
    if (dialogMode === 'create') {
      await createCertificate.mutateAsync(certificate)
    } else {
      await updateCertificate.mutateAsync({
        namespace: certificate.metadata?.namespace ?? '',
        name: certificate.metadata?.name ?? '',
        certificate,
      })
    }
  }

  const getRowKey = (row: Certificate) =>
    `${row.metadata?.namespace}/${row.metadata?.name}`

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load certificates: {error.message}
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
            Create Certificate
          </Button>
        )}
      </div>

      <DataTable
        data={certificates}
        columns={columns}
        getRowKey={getRowKey}
        selectable={!readOnly}
        selectedRows={selectedRows}
        onSelectionChange={setSelectedRows}
        onRowClick={handleEdit}
        isLoading={isLoading}
        emptyMessage="No certificates found"
        searchPlaceholder="Search certificates..."
        searchFilter={(row, query) =>
          row.metadata?.name?.toLowerCase().includes(query) ||
          row.metadata?.namespace?.toLowerCase().includes(query) ||
          row.spec?.domains?.some((d) => d.toLowerCase().includes(query)) ||
          row.spec?.issuer?.toLowerCase().includes(query) ||
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
            ? 'Create Certificate'
            : dialogMode === 'edit'
            ? 'Edit Certificate'
            : 'View Certificate'
        }
        description={
          dialogMode === 'create'
            ? 'Define a new certificate configuration'
            : undefined
        }
        mode={dialogMode}
        resource={currentCertificate}
        onSubmit={handleSubmit}
        isLoading={createCertificate.isPending || updateCertificate.isPending}
        readOnly={readOnly}
        defaultYaml={DEFAULT_CERTIFICATE_YAML}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Certificate"
        description={`Are you sure you want to delete certificate "${certificateToDelete?.metadata?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
        isLoading={deleteCertificate.isPending}
      />

      <ConfirmDialog
        open={bulkDeleteDialogOpen}
        onOpenChange={setBulkDeleteDialogOpen}
        title="Delete Selected Certificates"
        description={`Are you sure you want to delete ${selectedRows.size} certificate(s)? This action cannot be undone.`}
        confirmLabel="Delete All"
        variant="destructive"
        onConfirm={confirmBulkDelete}
        isLoading={bulkDelete.isPending}
      />
    </div>
  )
}
