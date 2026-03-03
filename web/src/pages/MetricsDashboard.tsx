import { useState, useMemo, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import { MetricsChart } from '@/components/metrics/MetricsChart'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  AreaChart,
  Area,
} from 'recharts'
import { BarChart3, AlertCircle, Play, TableIcon, LineChart } from 'lucide-react'

const TIME_RANGES = [
  { label: '5m', minutes: 5, step: '15s' },
  { label: '15m', minutes: 15, step: '15s' },
  { label: '1h', minutes: 60, step: '30s' },
  { label: '6h', minutes: 360, step: '120s' },
  { label: '24h', minutes: 1440, step: '300s' },
]

interface PrometheusResult {
  data?: {
    resultType?: string
    result?: Array<{
      metric?: Record<string, string>
      value?: [number, string]
      values?: [number, string][]
    }>
  }
}

function parseRangeResult(
  data: PrometheusResult | undefined,
  label: string
): { timestamp: string; [key: string]: string | number }[] {
  if (!data?.data?.result?.[0]?.values) return []
  return data.data.result[0].values.map(([ts, val]) => ({
    timestamp: new Date(ts * 1000).toLocaleTimeString(),
    [label]: parseFloat(val) || 0,
  }))
}

function mergeRangeResults(
  datasets: { data: PrometheusResult | undefined; label: string }[]
): { timestamp: string; [key: string]: string | number }[] {
  const timeMap = new Map<string, { timestamp: string; [key: string]: string | number }>()

  for (const { data, label } of datasets) {
    if (!data?.data?.result?.[0]?.values) continue
    for (const [ts, val] of data.data.result[0].values) {
      const timeStr = new Date(ts * 1000).toLocaleTimeString()
      const existing = timeMap.get(timeStr) ?? { timestamp: timeStr }
      existing[label] = parseFloat(val) || 0
      timeMap.set(timeStr, existing)
    }
  }

  return Array.from(timeMap.values())
}

function parseInstantResult(
  data: PrometheusResult | undefined,
  labelKey: string = 'listener'
): { name: string; value: number }[] {
  if (!data?.data?.result) return []
  return data.data.result.map((r) => ({
    name: r.metric?.[labelKey] ?? r.metric?.instance ?? 'unknown',
    value: parseFloat(r.value?.[1] ?? '0'),
  }))
}

export default function MetricsDashboard() {
  const [selectedRange, setSelectedRange] = useState(2) // default 1h
  const [customQuery, setCustomQuery] = useState('')
  const [customResultMode, setCustomResultMode] = useState<'table' | 'chart'>('chart')
  const [executedQuery, setExecutedQuery] = useState('')

  const range = TIME_RANGES[selectedRange]
  const end = useMemo(() => new Date(), [selectedRange]) // eslint-disable-line react-hooks/exhaustive-deps
  const start = useMemo(() => new Date(end.getTime() - range.minutes * 60 * 1000), [end, range.minutes])

  // Request Rate
  const { data: requestRateData, error: requestRateError } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'requestRate', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'sum(rate(novaedge_http_requests_total[5m]))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // Error Rate
  const { data: errorRateData } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'errorRate', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'sum(rate(novaedge_http_requests_total{status_class="5xx"}[5m]))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // P50 Latency
  const { data: p50Data } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'p50', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'histogram_quantile(0.5, sum(rate(novaedge_http_request_duration_seconds_bucket[5m])) by (le))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // P95 Latency
  const { data: p95Data } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'p95', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'histogram_quantile(0.95, sum(rate(novaedge_http_request_duration_seconds_bucket[5m])) by (le))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // P99 Latency
  const { data: p99Data } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'p99', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'histogram_quantile(0.99, sum(rate(novaedge_http_request_duration_seconds_bucket[5m])) by (le))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // Upstream RTT
  const { data: rttData } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'rtt', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'avg(rate(novaedge_upstream_rtt_seconds_sum[5m])/rate(novaedge_upstream_rtt_seconds_count[5m]))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // Active Connections
  const { data: connectionsData } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'connections', selectedRange],
    queryFn: () => api.metrics.query('novaedge_active_connections') as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // Cache Hit Rate
  const { data: cacheData } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'cache', selectedRange],
    queryFn: () => api.metrics.queryRange(
      'sum(rate(novaedge_cache_hits_total[5m])) / sum(rate(novaedge_cache_requests_total[5m]))',
      start, end, range.step
    ) as Promise<PrometheusResult>,
    refetchInterval: 30000,
  })

  // Custom Query
  const { data: customData, isLoading: customLoading, error: customError } = useQuery<PrometheusResult>({
    queryKey: ['metrics', 'custom', executedQuery],
    queryFn: () => api.metrics.query(executedQuery) as Promise<PrometheusResult>,
    enabled: !!executedQuery,
  })

  // Processed chart data
  const requestRateChart = useMemo(
    () => parseRangeResult(requestRateData, 'requests'),
    [requestRateData]
  )

  const errorRateChart = useMemo(
    () => parseRangeResult(errorRateData, 'errors'),
    [errorRateData]
  )

  const latencyChart = useMemo(
    () => mergeRangeResults([
      { data: p50Data, label: 'p50' },
      { data: p95Data, label: 'p95' },
      { data: p99Data, label: 'p99' },
    ]),
    [p50Data, p95Data, p99Data]
  )

  const rttChart = useMemo(
    () => parseRangeResult(rttData, 'rtt'),
    [rttData]
  )

  const connectionsChart = useMemo(
    () => parseInstantResult(connectionsData),
    [connectionsData]
  )

  const cacheChart = useMemo(
    () => parseRangeResult(cacheData, 'hitRate').map((d) => ({
      ...d,
      hitRate: typeof d.hitRate === 'number' ? d.hitRate * 100 : 0,
    })),
    [cacheData]
  )

  const customResults = useMemo(() => {
    if (!customData?.data?.result) return []
    return customData.data.result.map((r) => ({
      metric: JSON.stringify(r.metric ?? {}),
      value: r.value?.[1] ?? '-',
      timestamp: r.value?.[0] ? new Date(r.value[0] * 1000).toLocaleTimeString() : '-',
    }))
  }, [customData])

  const executeCustomQuery = useCallback(() => {
    if (customQuery.trim()) {
      setExecutedQuery(customQuery.trim())
    }
  }, [customQuery])

  const hasNoMetrics = requestRateError && !requestRateData

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="h-6 w-6" />
          <h1 className="text-2xl font-bold">Metrics Dashboard</h1>
        </div>

        {/* Time Range Selector */}
        <div className="flex gap-1 bg-muted rounded-lg p-1">
          {TIME_RANGES.map((r, i) => (
            <Button
              key={r.label}
              variant={selectedRange === i ? 'default' : 'ghost'}
              size="sm"
              className="text-xs h-7 px-3"
              onClick={() => setSelectedRange(i)}
            >
              {r.label}
            </Button>
          ))}
        </div>
      </div>

      {hasNoMetrics ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Metrics Unavailable</p>
            <p className="text-sm mt-1">Prometheus may not be configured or reachable</p>
          </CardContent>
        </Card>
      ) : (
        <>
          {/* Section 1: Traffic */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide">Traffic</h2>
            <div className="grid gap-4 md:grid-cols-2">
              <MetricsChart
                title="Request Rate"
                data={requestRateChart}
                lines={[{ key: 'requests', label: 'Requests/sec', color: 'hsl(var(--primary))' }]}
                yAxisLabel="req/s"
              />
              <MetricsChart
                title="Error Rate (5xx)"
                data={errorRateChart}
                lines={[{ key: 'errors', label: 'Errors/sec', color: 'hsl(var(--destructive))' }]}
                yAxisLabel="err/s"
              />
            </div>
          </div>

          {/* Section 2: Latency */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide">Latency</h2>
            <div className="grid gap-4 md:grid-cols-2">
              <MetricsChart
                title="Request Latency Percentiles"
                data={latencyChart}
                lines={[
                  { key: 'p50', label: 'P50', color: 'hsl(var(--chart-1))' },
                  { key: 'p95', label: 'P95', color: 'hsl(var(--chart-2))' },
                  { key: 'p99', label: 'P99', color: 'hsl(var(--chart-3))' },
                ]}
                yAxisLabel="seconds"
              />
              <MetricsChart
                title="Upstream RTT"
                data={rttChart}
                lines={[{ key: 'rtt', label: 'Avg RTT', color: 'hsl(var(--chart-4))' }]}
                yAxisLabel="seconds"
              />
            </div>
          </div>

          {/* Section 3: Backend Health */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide">Backend Health</h2>
            {connectionsChart.length > 0 ? (
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm font-medium">Active Connections by Listener</CardTitle>
                </CardHeader>
                <CardContent>
                  <div style={{ height: 300 }}>
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={connectionsChart} margin={{ top: 5, right: 30, left: 20, bottom: 5 }}>
                        <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                        <XAxis
                          dataKey="name"
                          tick={{ fill: 'hsl(var(--muted-foreground))' }}
                          className="text-xs"
                        />
                        <YAxis
                          tick={{ fill: 'hsl(var(--muted-foreground))' }}
                          className="text-xs"
                        />
                        <Tooltip
                          contentStyle={{
                            backgroundColor: 'hsl(var(--card))',
                            border: '1px solid hsl(var(--border))',
                            borderRadius: '6px',
                          }}
                        />
                        <Bar
                          dataKey="value"
                          name="Connections"
                          fill="hsl(var(--primary))"
                          radius={[4, 4, 0, 0]}
                        />
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                </CardContent>
              </Card>
            ) : (
              <Card>
                <CardContent className="flex items-center justify-center h-32 text-muted-foreground">
                  No active connections data available
                </CardContent>
              </Card>
            )}
          </div>

          {/* Section 4: Cache & Compression */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide">Cache</h2>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium">Cache Hit Rate</CardTitle>
              </CardHeader>
              <CardContent>
                <div style={{ height: 300 }}>
                  {cacheChart.length > 0 ? (
                    <ResponsiveContainer width="100%" height="100%">
                      <AreaChart data={cacheChart} margin={{ top: 5, right: 30, left: 20, bottom: 5 }}>
                        <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                        <XAxis
                          dataKey="timestamp"
                          tick={{ fill: 'hsl(var(--muted-foreground))' }}
                          className="text-xs"
                        />
                        <YAxis
                          tick={{ fill: 'hsl(var(--muted-foreground))' }}
                          className="text-xs"
                          label={{
                            value: '%',
                            angle: -90,
                            position: 'insideLeft',
                            fill: 'hsl(var(--muted-foreground))',
                          }}
                        />
                        <Tooltip
                          contentStyle={{
                            backgroundColor: 'hsl(var(--card))',
                            border: '1px solid hsl(var(--border))',
                            borderRadius: '6px',
                          }}
                          formatter={(value) => [`${Number(value ?? 0).toFixed(2)}%`, 'Hit Rate']}
                        />
                        <Area
                          type="monotone"
                          dataKey="hitRate"
                          name="Hit Rate"
                          stroke="hsl(var(--chart-1))"
                          fill="hsl(var(--chart-1))"
                          fillOpacity={0.2}
                        />
                      </AreaChart>
                    </ResponsiveContainer>
                  ) : (
                    <div className="flex items-center justify-center h-full text-muted-foreground">
                      No cache data available
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Section 5: Custom Query */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide">Custom Query</h2>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium">PromQL Query</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex gap-2">
                  <div className="flex-1">
                    <Label className="sr-only">PromQL Query</Label>
                    <Input
                      value={customQuery}
                      onChange={(e) => setCustomQuery(e.target.value)}
                      placeholder="Enter a PromQL query, e.g. up{job='novaedge'}"
                      className="font-mono text-sm"
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') executeCustomQuery()
                      }}
                    />
                  </div>
                  <Button onClick={executeCustomQuery} disabled={!customQuery.trim()}>
                    <Play className="h-4 w-4 mr-2" />
                    Execute
                  </Button>
                  <div className="flex gap-1 bg-muted rounded-md p-0.5">
                    <Button
                      variant={customResultMode === 'chart' ? 'default' : 'ghost'}
                      size="icon"
                      className="h-8 w-8"
                      onClick={() => setCustomResultMode('chart')}
                      title="Chart view"
                    >
                      <LineChart className="h-4 w-4" />
                    </Button>
                    <Button
                      variant={customResultMode === 'table' ? 'default' : 'ghost'}
                      size="icon"
                      className="h-8 w-8"
                      onClick={() => setCustomResultMode('table')}
                      title="Table view"
                    >
                      <TableIcon className="h-4 w-4" />
                    </Button>
                  </div>
                </div>

                {customLoading && (
                  <div className="flex items-center justify-center h-32">
                    <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary" />
                  </div>
                )}

                {customError && (
                  <div className="flex flex-col items-center justify-center h-32 text-muted-foreground">
                    <AlertCircle className="h-8 w-8 mb-2 text-destructive" />
                    <p className="text-sm">Query failed. Check your PromQL syntax.</p>
                  </div>
                )}

                {customResults.length > 0 && !customLoading && !customError && (
                  customResultMode === 'table' ? (
                    <div className="border rounded-md overflow-hidden">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>Metric</TableHead>
                            <TableHead className="text-right">Value</TableHead>
                            <TableHead>Timestamp</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {customResults.map((result, i) => (
                            <TableRow key={i}>
                              <TableCell className="font-mono text-xs max-w-md truncate">
                                {result.metric}
                              </TableCell>
                              <TableCell className="text-right font-mono">
                                {typeof result.value === 'string' ? result.value : String(result.value)}
                              </TableCell>
                              <TableCell className="text-sm text-muted-foreground">
                                {result.timestamp}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  ) : (
                    <div style={{ height: 250 }}>
                      <ResponsiveContainer width="100%" height="100%">
                        <BarChart
                          data={customResults.map((r, i) => ({
                            name: `[${i}]`,
                            value: parseFloat(r.value) || 0,
                          }))}
                          margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
                        >
                          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                          <XAxis
                            dataKey="name"
                            tick={{ fill: 'hsl(var(--muted-foreground))' }}
                            className="text-xs"
                          />
                          <YAxis
                            tick={{ fill: 'hsl(var(--muted-foreground))' }}
                            className="text-xs"
                          />
                          <Tooltip
                            contentStyle={{
                              backgroundColor: 'hsl(var(--card))',
                              border: '1px solid hsl(var(--border))',
                              borderRadius: '6px',
                            }}
                          />
                          <Bar dataKey="value" fill="hsl(var(--primary))" radius={[4, 4, 0, 0]} />
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  )
                )}

                {executedQuery && customResults.length === 0 && !customLoading && !customError && (
                  <div className="flex items-center justify-center h-32 text-muted-foreground">
                    No results returned for this query
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  )
}
