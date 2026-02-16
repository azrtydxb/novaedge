import { useMemo } from 'react'
import { useWAFEvents } from '@/api/hooks'
import { MetricCard } from '@/components/metrics/MetricCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
  Cell,
} from 'recharts'
import { Shield, ShieldAlert, ShieldCheck, AlertCircle, Activity } from 'lucide-react'
import { formatNumber } from '@/lib/utils'

export default function WAFEvents() {
  const { data: wafSummary, isLoading, error } = useWAFEvents()

  const blockRate = useMemo(() => {
    if (!wafSummary || wafSummary.totalRequests === 0) return 0
    return (wafSummary.blockedRequests / wafSummary.totalRequests) * 100
  }, [wafSummary])

  const topRulesChartData = useMemo(() => {
    if (!wafSummary?.topRules) return []
    return [...wafSummary.topRules]
      .sort((a, b) => b.count - a.count)
      .slice(0, 10)
      .map((rule) => ({
        name: rule.ruleId,
        count: rule.count,
        msg: rule.ruleMsg,
      }))
  }, [wafSummary])

  const sortedRules = useMemo(() => {
    if (!wafSummary?.topRules) return []
    return [...wafSummary.topRules].sort((a, b) => b.count - a.count)
  }, [wafSummary])

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <Shield className="h-6 w-6" />
          <h1 className="text-2xl font-bold">WAF Events</h1>
        </div>
        <Card>
          <CardContent className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
          </CardContent>
        </Card>
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <Shield className="h-6 w-6" />
          <h1 className="text-2xl font-bold">WAF Events</h1>
        </div>
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">WAF Data Unavailable</p>
            <p className="text-sm mt-1">WAF may not be configured or the backend is unreachable</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Shield className="h-6 w-6" />
        <h1 className="text-2xl font-bold">WAF Events</h1>
      </div>

      {/* Summary Cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          title="Total Requests"
          value={formatNumber(wafSummary?.totalRequests ?? 0)}
          icon={<Activity className="h-4 w-4" />}
        />
        <MetricCard
          title="Blocked"
          value={formatNumber(wafSummary?.blockedRequests ?? 0)}
          icon={<ShieldAlert className="h-4 w-4" />}
          className="border-red-500/30 bg-red-500/5"
        />
        <MetricCard
          title="Logged"
          value={formatNumber(wafSummary?.loggedRequests ?? 0)}
          icon={<ShieldCheck className="h-4 w-4" />}
          className="border-yellow-500/30 bg-yellow-500/5"
        />
        <MetricCard
          title="Block Rate"
          value={`${blockRate.toFixed(2)}%`}
          icon={<Shield className="h-4 w-4" />}
          className={blockRate > 10 ? 'border-red-500/30' : ''}
        />
      </div>

      {/* Top Blocked Rules Chart */}
      {topRulesChartData.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Top 10 Triggered Rules</CardTitle>
          </CardHeader>
          <CardContent>
            <div style={{ height: 350 }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={topRulesChartData}
                  layout="vertical"
                  margin={{ top: 5, right: 30, left: 100, bottom: 5 }}
                >
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis
                    type="number"
                    tick={{ fill: 'hsl(var(--muted-foreground))' }}
                    className="text-xs"
                  />
                  <YAxis
                    type="category"
                    dataKey="name"
                    tick={{ fill: 'hsl(var(--muted-foreground))' }}
                    className="text-xs"
                    width={90}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: 'hsl(var(--card))',
                      border: '1px solid hsl(var(--border))',
                      borderRadius: '6px',
                    }}
                    formatter={(value: number) => [`${value} hits`, 'Count']}
                  />
                  <Bar dataKey="count" radius={[0, 4, 4, 0]}>
                    {topRulesChartData.map((_, index) => (
                      <Cell
                        key={index}
                        fill={index < 3 ? 'hsl(var(--destructive))' : 'hsl(var(--primary))'}
                        opacity={1 - index * 0.06}
                      />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Rules Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Rule Details</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {sortedRules.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-muted-foreground">
              <p>No WAF rules have been triggered</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Rule ID</TableHead>
                  <TableHead>Rule Message</TableHead>
                  <TableHead className="text-right">Count</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRules.map((rule) => (
                  <TableRow key={rule.ruleId}>
                    <TableCell className="font-mono text-sm">{rule.ruleId}</TableCell>
                    <TableCell className="text-sm">{rule.ruleMsg || '-'}</TableCell>
                    <TableCell className="text-right font-mono font-medium">
                      {rule.count.toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
