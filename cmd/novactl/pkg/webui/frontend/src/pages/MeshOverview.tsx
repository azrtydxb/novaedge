import { useState } from 'react'
import { useMeshStatus, useMeshTopology } from '@/api/hooks'
import type { MeshService } from '@/api/types'
import { TopologyGraph } from '@/components/mesh/TopologyGraph'
import { MetricCard } from '@/components/metrics/MetricCard'
import { DataTable, Column } from '@/components/common/DataTable'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, Network, ShieldCheck, ShieldOff } from 'lucide-react'
import { Input } from '@/components/ui/input'

export default function MeshOverview() {
  const { data: meshStatus, isLoading: statusLoading, error: statusError } = useMeshStatus()
  const { data: topology, isLoading: topoLoading } = useMeshTopology()
  const [search, setSearch] = useState('')

  const notEnrolled = meshStatus
    ? meshStatus.totalServices - meshStatus.mtlsEnabled
    : 0

  const filteredServices = (meshStatus?.services ?? []).filter((svc) =>
    svc.name.toLowerCase().includes(search.toLowerCase()) ||
    svc.namespace.toLowerCase().includes(search.toLowerCase())
  )

  const columns: Column<MeshService>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.name,
      sortable: true,
    },
    {
      key: 'namespace',
      header: 'Namespace',
      accessor: (row) => (
        <Badge variant="outline">{row.namespace}</Badge>
      ),
      sortable: true,
    },
    {
      key: 'spiffeId',
      header: 'SPIFFE ID',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-xs">
          {row.spiffeId ?? '-'}
        </code>
      ),
    },
    {
      key: 'mtlsStatus',
      header: 'mTLS Status',
      accessor: (row) => (
        <Badge
          variant={row.mtlsStatus === 'enabled' ? 'default' : 'secondary'}
          className={row.mtlsStatus === 'enabled' ? 'bg-green-500 hover:bg-green-600' : 'bg-red-500 hover:bg-red-600'}
        >
          {row.mtlsStatus === 'enabled' ? 'Enabled' : 'Disabled'}
        </Badge>
      ),
    },
    {
      key: 'meshEnabled',
      header: 'Mesh Enabled',
      accessor: (row) => (
        <Badge
          variant={row.meshEnabled ? 'default' : 'secondary'}
          className={row.meshEnabled ? 'bg-blue-500 hover:bg-blue-600' : ''}
        >
          {row.meshEnabled ? 'Yes' : 'No'}
        </Badge>
      ),
    },
  ]

  if (statusError) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
          <AlertCircle className="h-12 w-12 mb-4" />
          <p className="text-lg font-medium">Mesh Status Unavailable</p>
          <p className="text-sm mt-1">{statusError.message}</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          title="Total Services"
          value={meshStatus?.totalServices ?? 0}
          icon={<Network className="h-4 w-4" />}
        />
        <MetricCard
          title="mTLS Enabled"
          value={meshStatus?.mtlsEnabled ?? 0}
          icon={<ShieldCheck className="h-4 w-4" />}
          className="border-green-500/30"
        />
        <MetricCard
          title="Not Enrolled"
          value={notEnrolled}
          icon={<ShieldOff className="h-4 w-4" />}
          className={notEnrolled > 0 ? 'border-yellow-500/30' : ''}
        />
      </div>

      {/* Topology graph */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Network className="h-4 w-4" />
            Service Mesh Topology
          </CardTitle>
        </CardHeader>
        <CardContent>
          {topoLoading ? (
            <div className="flex items-center justify-center h-[400px]">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
          ) : (
            <TopologyGraph
              nodes={topology?.nodes ?? []}
              edges={topology?.edges ?? []}
            />
          )}
        </CardContent>
      </Card>

      {/* Service list */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-medium">Mesh Services</CardTitle>
            <Input
              placeholder="Filter services..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-64"
            />
          </div>
        </CardHeader>
        <CardContent>
          <DataTable
            data={filteredServices}
            columns={columns}
            getRowKey={(row) => `${row.namespace}/${row.name}`}
            isLoading={statusLoading}
            emptyMessage="No mesh services found"
          />
        </CardContent>
      </Card>
    </div>
  )
}
