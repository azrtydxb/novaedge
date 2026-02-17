import { useApp } from '@/contexts/AppContext'
import {
  useDashboardMetrics,
  useGateways,
  useRoutes,
  useBackends,
  useVIPs,
  usePolicies,
  useCertificates,
  useIPPools,
  useClusters,
  useFederations,
  useRemoteClusters,
  useAgents,
  useEvents,
  useOverloadStatus,
  useWASMPlugins,
} from '@/api/hooks'
import { MetricCard } from '@/components/metrics/MetricCard'
import { MetricsChart } from '@/components/metrics/MetricsChart'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Activity,
  Server,
  GitBranch,
  Database,
  Star,
  Shield,
  AlertCircle,
  Cpu,
  MemoryStick,
  KeyRound,
  Network,
  Boxes,
  Globe,
  MonitorCheck,
  CalendarClock,
  Zap,
  Puzzle,
  ShieldAlert,
} from 'lucide-react'
import { formatBytes, formatUptime } from '@/lib/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Link } from 'react-router-dom'

export default function Dashboard() {
  const { namespace, mode } = useApp()
  const { data: metrics, isLoading: metricsLoading, error: metricsError } = useDashboardMetrics()
  const { data: gateways = [] } = useGateways(namespace)
  const { data: routes = [] } = useRoutes(namespace)
  const { data: backends = [] } = useBackends(namespace)
  const { data: vips = [] } = useVIPs(namespace)
  const { data: policies = [] } = usePolicies(namespace)
  const { data: certificates = [] } = useCertificates(namespace)
  const { data: ippools = [] } = useIPPools()
  const { data: clusters = [] } = useClusters(namespace)
  const { data: federations = [] } = useFederations(namespace)
  const { data: remoteClusters = [] } = useRemoteClusters(namespace)
  const { data: agents = [] } = useAgents()
  const { data: events = [] } = useEvents()
  const { data: overloadStatus } = useOverloadStatus()
  const { data: wasmPlugins = [] } = useWASMPlugins()

  const readyAgents = agents.filter((a) => a.ready).length
  const totalAgents = agents.length

  const resourceCounts = [
    { title: 'Gateways', value: gateways.length, icon: <Server className="h-4 w-4" />, link: '/gateways' },
    { title: 'Routes', value: routes.length, icon: <GitBranch className="h-4 w-4" />, link: '/routes' },
    { title: 'Backends', value: backends.length, icon: <Database className="h-4 w-4" />, link: '/backends' },
    { title: 'VIPs', value: vips.length, icon: <Star className="h-4 w-4" />, link: '/vips' },
    { title: 'Policies', value: policies.length, icon: <Shield className="h-4 w-4" />, link: '/policies' },
    { title: 'Certificates', value: certificates.length, icon: <KeyRound className="h-4 w-4" />, link: '/certificates' },
    { title: 'IP Pools', value: ippools.length, icon: <Network className="h-4 w-4" />, link: '/ippools' },
    { title: 'Clusters', value: clusters.length, icon: <Boxes className="h-4 w-4" />, link: '/clusters' },
    { title: 'Federations', value: federations.length, icon: <Globe className="h-4 w-4" />, link: '/federation' },
    { title: 'Remote Clusters', value: remoteClusters.length, icon: <MonitorCheck className="h-4 w-4" />, link: '/federation' },
  ]

  const hasMetrics = metrics && !metricsError

  // Federation derived data
  const connectedClusters = remoteClusters.filter(
    (rc) => rc.status?.connected === true
  ).length

  // Fault injection: count routes that have faultInjection configured
  const faultInjectionCount = routes.filter(
    (r) => r.spec?.faultInjection
  ).length

  // Slow start: count backends that have slowStart configured
  const slowStartCount = backends.filter(
    (b) => b.spec?.slowStart
  ).length

  // Outlier detection ejected: count backends with outlierDetection
  const outlierDetectionCount = backends.filter(
    (b) => b.spec?.outlierDetection
  ).length

  // WASM plugins loaded
  const loadedPlugins = wasmPlugins.filter((p) => p.loaded).length

  // Recent events (last 10)
  const recentEvents = events.slice(0, 10)

  return (
    <div className="space-y-6">
      {/* Cluster Health Bar */}
      <Card>
        <CardContent className="py-3">
          <div className="flex items-center gap-6 flex-wrap">
            <div className="flex items-center gap-2">
              <Badge variant={mode === 'kubernetes' ? 'default' : 'secondary'}>
                {mode === 'kubernetes' ? 'Kubernetes Mode' : 'Standalone Mode'}
              </Badge>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">Nodes:</span>
              <Badge variant="outline">{totalAgents}</Badge>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">Agents:</span>
              <Badge className={readyAgents === totalAgents && totalAgents > 0 ? 'bg-green-500' : totalAgents > 0 ? 'bg-yellow-500' : ''}>
                {readyAgents}/{totalAgents} Ready
              </Badge>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">Controller:</span>
              <Badge className={hasMetrics ? 'bg-green-500' : 'bg-red-500'}>
                {hasMetrics ? 'Healthy' : 'Unknown'}
              </Badge>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Resource Summary Grid - all 10 CRDs */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
        {resourceCounts.map((item) => (
          <Link key={item.title} to={item.link} className="block">
            <MetricCard
              title={item.title}
              value={item.value}
              icon={item.icon}
              className="hover:border-primary/50 transition-colors cursor-pointer"
            />
          </Link>
        ))}
      </div>

      {/* Federation Status, Load Shedding, Active Features */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Federation Status */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Globe className="h-4 w-4" />
              Federation Status
            </CardTitle>
          </CardHeader>
          <CardContent>
            {federations.length === 0 ? (
              <p className="text-sm text-muted-foreground">No federations configured</p>
            ) : (
              <div className="space-y-2">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Federations</span>
                  <span className="font-medium">{federations.length}</span>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Connected Clusters</span>
                  <Badge variant="outline">{connectedClusters}/{remoteClusters.length}</Badge>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Sync Status</span>
                  <Badge className={connectedClusters === remoteClusters.length && remoteClusters.length > 0 ? 'bg-green-500' : remoteClusters.length > 0 ? 'bg-yellow-500' : ''}>
                    {connectedClusters === remoteClusters.length && remoteClusters.length > 0 ? 'Synced' : remoteClusters.length > 0 ? 'Partial' : 'N/A'}
                  </Badge>
                </div>
                <Link to="/federation" className="text-xs text-primary hover:underline block pt-1">
                  View Federation Details
                </Link>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Load Shedding Status */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <ShieldAlert className="h-4 w-4" />
              Load Shedding
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">State</span>
                <Badge className={overloadStatus?.state === 'overloaded' ? 'bg-red-500' : 'bg-green-500'}>
                  {overloadStatus?.state === 'overloaded' ? 'Overloaded' : 'Normal'}
                </Badge>
              </div>
              {overloadStatus?.state === 'overloaded' && (
                <div className="flex items-center gap-2 text-sm text-yellow-600 dark:text-yellow-400">
                  <Zap className="h-3 w-3" />
                  <span>Shedding active</span>
                </div>
              )}
              {overloadStatus && (
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Total Shed</span>
                  <span className="font-medium">{overloadStatus.totalShed}</span>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Active Features Summary */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Puzzle className="h-4 w-4" />
              Active Features
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">Fault Injection Rules</span>
                <span className="font-medium">{faultInjectionCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">Endpoints in Slow Start</span>
                <span className="font-medium">{slowStartCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">Outlier Detection</span>
                <span className="font-medium">{outlierDetectionCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">WASM Plugins Loaded</span>
                <span className="font-medium">{loadedPlugins}</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Recent Events */}
      {recentEvents.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <CalendarClock className="h-4 w-4" />
              Recent Events
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {recentEvents.map((event, i) => (
                <div
                  key={`${event.timestamp}-${event.reason}-${i}`}
                  className={`flex items-start gap-3 p-2 rounded text-sm ${
                    event.type === 'Warning' ? 'bg-yellow-500/10' : ''
                  }`}
                >
                  <span className="text-xs text-muted-foreground whitespace-nowrap mt-0.5">
                    {new Date(event.timestamp).toLocaleTimeString()}
                  </span>
                  <Badge
                    variant={event.type === 'Warning' ? 'destructive' : 'secondary'}
                    className="text-xs shrink-0"
                  >
                    {event.type}
                  </Badge>
                  <span className="font-medium shrink-0">{event.reason}</span>
                  <span className="text-muted-foreground truncate">{event.message}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Metrics section */}
      {metricsLoading ? (
        <Card>
          <CardContent className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
          </CardContent>
        </Card>
      ) : metricsError ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Metrics Unavailable</p>
            <p className="text-sm mt-1">Prometheus may not be configured or reachable</p>
          </CardContent>
        </Card>
      ) : hasMetrics ? (
        <>
          {/* Traffic metrics */}
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <MetricCard
              title="Requests/sec"
              value={metrics.requestsPerSecond?.toFixed(2) ?? '0'}
              icon={<Activity className="h-4 w-4" />}
            />
            <MetricCard
              title="Avg Latency"
              value={`${metrics.avgLatencyMs?.toFixed(1) ?? '0'} ms`}
              icon={<Activity className="h-4 w-4" />}
            />
            <MetricCard
              title="Error Rate"
              value={`${((metrics.errorRate ?? 0) * 100).toFixed(2)}%`}
              icon={<AlertCircle className="h-4 w-4" />}
            />
            <MetricCard
              title="Active Connections"
              value={metrics.activeConnections ?? 0}
              icon={<Server className="h-4 w-4" />}
            />
          </div>

          {/* Bandwidth */}
          <div className="grid gap-4 md:grid-cols-2">
            <MetricCard
              title="Bandwidth In"
              value={formatBytes(metrics.bandwidthIn ?? 0)}
              subtitle="Total inbound traffic"
            />
            <MetricCard
              title="Bandwidth Out"
              value={formatBytes(metrics.bandwidthOut ?? 0)}
              subtitle="Total outbound traffic"
            />
          </div>

          {/* Resource metrics (CPU, Memory, Goroutines) */}
          <div className="grid gap-4 md:grid-cols-3">
            <MetricCard
              title="Total CPU Usage"
              value={`${(metrics.totalCpuUsage ?? 0).toFixed(2)}%`}
              icon={<Cpu className="h-4 w-4" />}
              subtitle="Aggregate CPU across workers"
            />
            <MetricCard
              title="Total Memory"
              value={formatBytes(metrics.totalMemoryUsage ?? 0)}
              icon={<MemoryStick className="h-4 w-4" />}
              subtitle="Aggregate memory across workers"
            />
            <MetricCard
              title="Total Goroutines"
              value={metrics.totalGoroutines ?? 0}
              icon={<Activity className="h-4 w-4" />}
              subtitle="Active goroutines across workers"
            />
          </div>

          {/* Per-worker resource table */}
          {metrics.workers && metrics.workers.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium flex items-center gap-2">
                  <Server className="h-4 w-4" />
                  Worker Resources
                </CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Instance</TableHead>
                      <TableHead className="text-right">CPU</TableHead>
                      <TableHead className="text-right">Memory</TableHead>
                      <TableHead className="text-right">Goroutines</TableHead>
                      <TableHead className="text-right">Uptime</TableHead>
                      <TableHead className="text-right">Req/s</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {metrics.workers.map((worker) => (
                      <TableRow key={worker.instance}>
                        <TableCell className="font-medium">{worker.instance}</TableCell>
                        <TableCell className="text-right">{worker.cpuUsage.toFixed(2)}%</TableCell>
                        <TableCell className="text-right">{formatBytes(worker.memoryUsage)}</TableCell>
                        <TableCell className="text-right">{Math.floor(worker.goroutines)}</TableCell>
                        <TableCell className="text-right">{formatUptime(worker.uptime)}</TableCell>
                        <TableCell className="text-right">{worker.requestsRate.toFixed(2)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {/* Request rate chart */}
          {metrics.requestRateHistory && metrics.requestRateHistory.length > 0 && (
            <MetricsChart
              title="Request Rate Over Time"
              data={metrics.requestRateHistory.map((point) => ({
                timestamp: new Date(point.timestamp).toLocaleTimeString(),
                requests: point.value,
              }))}
              lines={[
                { key: 'requests', label: 'Requests/sec', color: 'hsl(var(--primary))' },
              ]}
              yAxisLabel="req/s"
            />
          )}

          {/* Latency chart */}
          {metrics.latencyHistory && metrics.latencyHistory.length > 0 && (
            <MetricsChart
              title="Latency Over Time"
              data={metrics.latencyHistory.map((point) => ({
                timestamp: new Date(point.timestamp).toLocaleTimeString(),
                latency: point.value,
              }))}
              lines={[
                { key: 'latency', label: 'Avg Latency (ms)', color: 'hsl(var(--chart-2))' },
              ]}
              yAxisLabel="ms"
            />
          )}
        </>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Metrics</CardTitle>
          </CardHeader>
          <CardContent className="text-muted-foreground">
            No metrics data available
          </CardContent>
        </Card>
      )}

      {/* Recent activity */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Resource Status</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {gateways.slice(0, 5).map((gw) => (
              <div key={`${gw.metadata?.namespace}-${gw.metadata?.name}`} className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Server className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm">{gw.metadata?.name}</span>
                  <Badge variant="outline" className="text-xs">
                    {gw.metadata?.namespace}
                  </Badge>
                </div>
                <Badge
                  variant={gw.status?.ready ? 'default' : 'secondary'}
                  className={gw.status?.ready ? 'bg-green-500' : ''}
                >
                  {gw.status?.ready ? 'Ready' : 'Not Ready'}
                </Badge>
              </div>
            ))}
            {gateways.length === 0 && (
              <p className="text-sm text-muted-foreground">No gateways configured</p>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
