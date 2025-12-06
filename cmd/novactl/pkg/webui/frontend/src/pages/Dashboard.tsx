import { useApp } from '@/contexts/AppContext'
import { useDashboardMetrics, useGateways, useRoutes, useBackends, useVIPs, usePolicies } from '@/api/hooks'
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

export default function Dashboard() {
  const { namespace, mode } = useApp()
  const { data: metrics, isLoading: metricsLoading, error: metricsError } = useDashboardMetrics()
  const { data: gateways = [] } = useGateways(namespace)
  const { data: routes = [] } = useRoutes(namespace)
  const { data: backends = [] } = useBackends(namespace)
  const { data: vips = [] } = useVIPs(namespace)
  const { data: policies = [] } = usePolicies(namespace)

  const resourceCounts = [
    { title: 'Gateways', value: gateways.length, icon: <Server className="h-4 w-4" /> },
    { title: 'Routes', value: routes.length, icon: <GitBranch className="h-4 w-4" /> },
    { title: 'Backends', value: backends.length, icon: <Database className="h-4 w-4" /> },
    { title: 'VIPs', value: vips.length, icon: <Star className="h-4 w-4" /> },
    { title: 'Policies', value: policies.length, icon: <Shield className="h-4 w-4" /> },
  ]

  const hasMetrics = metrics && !metricsError

  return (
    <div className="space-y-6">
      {/* Mode indicator */}
      <div className="flex items-center gap-2">
        <Badge variant={mode === 'kubernetes' ? 'default' : 'secondary'}>
          {mode === 'kubernetes' ? 'Kubernetes Mode' : 'Standalone Mode'}
        </Badge>
      </div>

      {/* Resource counts */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
        {resourceCounts.map((item) => (
          <MetricCard
            key={item.title}
            title={item.title}
            value={item.value}
            icon={item.icon}
          />
        ))}
      </div>

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
