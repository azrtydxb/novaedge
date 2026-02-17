import { useState } from 'react'
import {
  useSDWANLinks,
  useSDWANTopology,
  useSDWANPolicies,
  useSDWANEvents,
} from '@/api/hooks'
import type { WANLink, SDWANEvent, WANPolicy } from '@/api/sdwan-types'
import { MetricCard } from '@/components/metrics/MetricCard'
import { DataTable, Column } from '@/components/common/DataTable'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  AlertCircle,
  Network,
  Activity,
  Shield,
  ArrowRightLeft,
  CheckCircle2,
  XCircle,
  MapPin,
} from 'lucide-react'

function getHealthBadge(healthy: boolean) {
  if (healthy) {
    return (
      <Badge className="bg-green-500 hover:bg-green-600 text-white">
        <CheckCircle2 className="h-3 w-3 mr-1" />
        Healthy
      </Badge>
    )
  }
  return (
    <Badge className="bg-red-500 hover:bg-red-600 text-white">
      <XCircle className="h-3 w-3 mr-1" />
      Unhealthy
    </Badge>
  )
}

function getScoreBadge(score: number) {
  if (score >= 80) {
    return (
      <Badge className="bg-green-500 hover:bg-green-600 text-white">
        {score}
      </Badge>
    )
  }
  if (score >= 50) {
    return (
      <Badge className="bg-yellow-500 hover:bg-yellow-600 text-white">
        {score}
      </Badge>
    )
  }
  return (
    <Badge className="bg-red-500 hover:bg-red-600 text-white">
      {score}
    </Badge>
  )
}

function getRoleBadge(role: string) {
  const roleStr = role.toLowerCase()
  switch (roleStr) {
    case 'primary':
      return (
        <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
          primary
        </Badge>
      )
    case 'backup':
      return (
        <Badge className="bg-slate-500 hover:bg-slate-600 text-white">
          backup
        </Badge>
      )
    case 'active':
      return (
        <Badge className="bg-green-500 hover:bg-green-600 text-white">
          active
        </Badge>
      )
    default:
      return <Badge variant="outline">{role}</Badge>
  }
}

function getEventTypeBadge(type: string) {
  const typeStr = type.toLowerCase()
  if (typeStr === 'failover') {
    return (
      <Badge className="bg-red-500 hover:bg-red-600 text-white">
        failover
      </Badge>
    )
  }
  if (typeStr === 'path-selection' || typeStr === 'selection') {
    return (
      <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
        path-selection
      </Badge>
    )
  }
  if (typeStr === 'recovery') {
    return (
      <Badge className="bg-green-500 hover:bg-green-600 text-white">
        recovery
      </Badge>
    )
  }
  if (typeStr === 'degradation' || typeStr === 'degraded') {
    return (
      <Badge className="bg-yellow-500 hover:bg-yellow-600 text-white">
        degradation
      </Badge>
    )
  }
  return <Badge variant="outline">{type}</Badge>
}

function getStrategyBadge(strategy: string) {
  const stratStr = strategy.toLowerCase()
  switch (stratStr) {
    case 'performance':
      return (
        <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
          performance
        </Badge>
      )
    case 'cost':
      return (
        <Badge className="bg-green-500 hover:bg-green-600 text-white">
          cost
        </Badge>
      )
    case 'reliability':
      return (
        <Badge className="bg-purple-500 hover:bg-purple-600 text-white">
          reliability
        </Badge>
      )
    case 'latency':
      return (
        <Badge className="bg-orange-500 hover:bg-orange-600 text-white">
          latency
        </Badge>
      )
    default:
      return <Badge variant="outline">{strategy}</Badge>
  }
}

function getLinkQualityColor(latencyMs: number, healthy: boolean): string {
  if (!healthy) return 'border-red-500/50 bg-red-500/10'
  if (latencyMs < 30) return 'border-green-500/50 bg-green-500/10'
  if (latencyMs < 100) return 'border-yellow-500/50 bg-yellow-500/10'
  return 'border-red-500/50 bg-red-500/10'
}

function formatTimestamp(ts: string): string {
  try {
    return new Date(ts).toLocaleString()
  } catch {
    return String(ts)
  }
}

// --- Topology Section ---

function TopologySection() {
  const { data: topology, isLoading, error } = useSDWANTopology()

  if (error) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
          <AlertCircle className="h-8 w-8 mb-2" />
          <p className="text-sm">Failed to load topology: {error.message}</p>
        </CardContent>
      </Card>
    )
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Network className="h-4 w-4" />
            SD-WAN Topology
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center h-48">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
          </div>
        </CardContent>
      </Card>
    )
  }

  const sites = topology?.sites ?? []
  const links = topology?.links ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          <Network className="h-4 w-4" />
          SD-WAN Topology
          <Badge variant="outline" className="ml-2">
            {sites.length} sites
          </Badge>
          <Badge variant="outline">
            {links.length} links
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {sites.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-48 text-muted-foreground">
            <MapPin className="h-8 w-8 mb-2" />
            <p className="text-sm">No SD-WAN sites configured</p>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Sites grid */}
            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {sites.map((site) => (
                <div
                  key={site.name}
                  className="rounded-lg border border-border bg-card p-4 space-y-2"
                >
                  <div className="flex items-center gap-2">
                    <MapPin className="h-4 w-4 text-blue-400" />
                    <span className="font-medium text-sm">{site.name}</span>
                  </div>
                  <div className="text-xs text-muted-foreground space-y-1">
                    <div className="flex justify-between">
                      <span>Region</span>
                      <Badge variant="outline" className="text-xs">{site.region}</Badge>
                    </div>
                    <div className="flex justify-between">
                      <span>Overlay</span>
                      <code className="text-xs bg-muted px-1.5 py-0.5 rounded">{site.overlayAddr}</code>
                    </div>
                  </div>
                </div>
              ))}
            </div>

            {/* Connections list */}
            {links.length > 0 && (
              <div className="space-y-2">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Connections
                </h4>
                <div className="grid gap-2 md:grid-cols-2 lg:grid-cols-3">
                  {links.map((link, idx) => (
                    <div
                      key={`${link.from}-${link.to}-${idx}`}
                      className={`rounded-lg border p-3 text-xs ${getLinkQualityColor(link.latencyMs, link.healthy)}`}
                    >
                      <div className="flex items-center justify-between mb-1">
                        <div className="flex items-center gap-1.5">
                          <span className="font-medium">{link.from}</span>
                          <ArrowRightLeft className="h-3 w-3 text-muted-foreground" />
                          <span className="font-medium">{link.to}</span>
                        </div>
                        <span className={`inline-block h-2 w-2 rounded-full ${link.healthy ? 'bg-green-500' : 'bg-red-500'}`} />
                      </div>
                      <div className="flex items-center justify-between text-muted-foreground">
                        <span>{link.linkName}</span>
                        <span>{link.latencyMs}ms</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// --- WAN Links Table ---

function WANLinksSection() {
  const { data: links = [], isLoading, error } = useSDWANLinks()

  const columns: Column<WANLink>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => (
        <span className="font-medium">{row.name}</span>
      ),
      sortable: true,
    },
    {
      key: 'site',
      header: 'Site',
      accessor: (row) => (
        <Badge variant="outline">{row.site}</Badge>
      ),
      sortable: true,
    },
    {
      key: 'provider',
      header: 'Provider',
      accessor: (row) => row.provider,
      sortable: true,
    },
    {
      key: 'role',
      header: 'Role',
      accessor: (row) => getRoleBadge(row.role),
    },
    {
      key: 'bandwidth',
      header: 'Bandwidth',
      accessor: (row) => row.bandwidth,
    },
    {
      key: 'latencyMs',
      header: 'Latency',
      accessor: (row) => `${row.latencyMs}ms`,
      sortable: true,
    },
    {
      key: 'jitterMs',
      header: 'Jitter',
      accessor: (row) => `${row.jitterMs}ms`,
      sortable: true,
    },
    {
      key: 'packetLossPercent',
      header: 'Packet Loss',
      accessor: (row) => {
        const pct = row.packetLossPercent
        const color = pct === 0 ? 'text-green-400' : pct < 1 ? 'text-yellow-400' : 'text-red-400'
        return <span className={color}>{pct}%</span>
      },
      sortable: true,
    },
    {
      key: 'score',
      header: 'Score',
      accessor: (row) => getScoreBadge(row.score),
      sortable: true,
    },
    {
      key: 'healthy',
      header: 'Health',
      accessor: (row) => getHealthBadge(row.healthy),
    },
  ]

  if (error) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
          <AlertCircle className="h-8 w-8 mb-2" />
          <p className="text-sm">Failed to load WAN links: {error.message}</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          <Activity className="h-4 w-4" />
          WAN Links
        </CardTitle>
      </CardHeader>
      <CardContent>
        <DataTable
          data={links}
          columns={columns}
          getRowKey={(row) => `${row.site}/${row.name}`}
          isLoading={isLoading}
          emptyMessage="No WAN links found"
          searchPlaceholder="Search WAN links..."
          searchFilter={(row, query) =>
            row.name.toLowerCase().includes(query) ||
            row.site.toLowerCase().includes(query) ||
            row.provider.toLowerCase().includes(query) ||
            false
          }
        />
      </CardContent>
    </Card>
  )
}

// --- Policies Section ---

function PoliciesSection() {
  const { data: policies = [], isLoading, error } = useSDWANPolicies()

  const columns: Column<WANPolicy>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => (
        <span className="font-medium">{row.name}</span>
      ),
      sortable: true,
    },
    {
      key: 'strategy',
      header: 'Strategy',
      accessor: (row) => getStrategyBadge(row.strategy),
    },
    {
      key: 'matchHosts',
      header: 'Match Hosts',
      accessor: (row) => (
        <div className="flex flex-wrap gap-1">
          {(row.matchHosts ?? []).length === 0
            ? <span className="text-muted-foreground text-xs">*</span>
            : row.matchHosts.map((host) => (
                <Badge key={host} variant="outline" className="text-xs">
                  {host}
                </Badge>
              ))
          }
        </div>
      ),
    },
    {
      key: 'dscpClass',
      header: 'DSCP Class',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-xs">
          {row.dscpClass || '-'}
        </code>
      ),
    },
    {
      key: 'selections',
      header: 'Selections',
      accessor: (row) => row.selections,
      sortable: true,
    },
  ]

  if (error) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
          <AlertCircle className="h-8 w-8 mb-2" />
          <p className="text-sm">Failed to load policies: {error.message}</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          <Shield className="h-4 w-4" />
          Active Policies
        </CardTitle>
      </CardHeader>
      <CardContent>
        <DataTable
          data={policies}
          columns={columns}
          getRowKey={(row) => row.name}
          isLoading={isLoading}
          emptyMessage="No WAN policies found"
          searchPlaceholder="Search policies..."
          searchFilter={(row, query) =>
            row.name.toLowerCase().includes(query) ||
            row.strategy.toLowerCase().includes(query) ||
            row.matchHosts.some((h) => h.toLowerCase().includes(query)) ||
            false
          }
        />
      </CardContent>
    </Card>
  )
}

// --- Events Section ---

function EventsSection() {
  const { data: events = [], isLoading, error } = useSDWANEvents()

  const columns: Column<SDWANEvent>[] = [
    {
      key: 'timestamp',
      header: 'Time',
      accessor: (row) => (
        <span className="text-xs text-muted-foreground">
          {formatTimestamp(row.timestamp)}
        </span>
      ),
      sortable: true,
    },
    {
      key: 'type',
      header: 'Type',
      accessor: (row) => getEventTypeBadge(row.type),
    },
    {
      key: 'fromLink',
      header: 'From Link',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-xs">
          {row.fromLink || '-'}
        </code>
      ),
    },
    {
      key: 'toLink',
      header: 'To Link',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-xs">
          {row.toLink || '-'}
        </code>
      ),
    },
    {
      key: 'policy',
      header: 'Policy',
      accessor: (row) => (
        <Badge variant="outline" className="text-xs">
          {row.policy || '-'}
        </Badge>
      ),
    },
    {
      key: 'reason',
      header: 'Reason',
      accessor: (row) => (
        <span className="text-xs">{row.reason}</span>
      ),
    },
  ]

  if (error) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
          <AlertCircle className="h-8 w-8 mb-2" />
          <p className="text-sm">Failed to load events: {error.message}</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <ArrowRightLeft className="h-4 w-4" />
            Recent Events
          </CardTitle>
          <Badge variant="outline" className="text-xs">
            Auto-refresh: 5s
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <DataTable
          data={events}
          columns={columns}
          getRowKey={(row) => `${row.timestamp}-${row.type}-${row.fromLink}-${row.toLink}`}
          isLoading={isLoading}
          emptyMessage="No recent SD-WAN events"
          searchPlaceholder="Search events..."
          searchFilter={(row, query) =>
            row.type.toLowerCase().includes(query) ||
            row.fromLink.toLowerCase().includes(query) ||
            row.toLink.toLowerCase().includes(query) ||
            row.policy.toLowerCase().includes(query) ||
            row.reason.toLowerCase().includes(query) ||
            false
          }
        />
      </CardContent>
    </Card>
  )
}

// --- Main Page ---

export default function SDWANOverview() {
  const { data: links = [] } = useSDWANLinks()
  const { data: topology } = useSDWANTopology()
  const { data: policies = [] } = useSDWANPolicies()
  const [activeTab, setActiveTab] = useState('overview')

  const healthyLinks = links.filter((l) => l.healthy).length
  const totalLinks = links.length
  const totalSites = topology?.sites?.length ?? 0
  const avgLatency = totalLinks > 0
    ? Math.round(links.reduce((sum, l) => sum + l.latencyMs, 0) / totalLinks)
    : 0

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <MetricCard
          title="Total Sites"
          value={totalSites}
          icon={<MapPin className="h-4 w-4" />}
        />
        <MetricCard
          title="WAN Links"
          value={`${healthyLinks}/${totalLinks}`}
          subtitle={totalLinks > 0 ? `${Math.round((healthyLinks / totalLinks) * 100)}% healthy` : 'No links'}
          icon={<Network className="h-4 w-4" />}
          className={healthyLinks < totalLinks ? 'border-yellow-500/30' : 'border-green-500/30'}
        />
        <MetricCard
          title="Avg Latency"
          value={`${avgLatency}ms`}
          icon={<Activity className="h-4 w-4" />}
          className={avgLatency > 100 ? 'border-red-500/30' : avgLatency > 50 ? 'border-yellow-500/30' : ''}
        />
        <MetricCard
          title="Active Policies"
          value={policies.length}
          icon={<Shield className="h-4 w-4" />}
        />
      </div>

      {/* Tabs for sections */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview">Topology</TabsTrigger>
          <TabsTrigger value="links">WAN Links</TabsTrigger>
          <TabsTrigger value="policies">Policies</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="space-y-4">
          <TopologySection />
        </TabsContent>
        <TabsContent value="links">
          <WANLinksSection />
        </TabsContent>
        <TabsContent value="policies">
          <PoliciesSection />
        </TabsContent>
        <TabsContent value="events">
          <EventsSection />
        </TabsContent>
      </Tabs>
    </div>
  )
}
