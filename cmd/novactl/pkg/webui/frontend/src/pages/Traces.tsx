import { useState, useMemo } from 'react'
import { useTraceSearch, useTrace, useTraceServices, useTraceOperations } from '@/api/hooks'
import { TraceTimeline } from '@/components/traces/TraceTimeline'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Search, ChevronDown, ChevronRight, Activity, AlertCircle } from 'lucide-react'
import { formatDuration } from '@/lib/utils'
import { format } from 'date-fns'

const LOOKBACK_OPTIONS = [
  { label: 'Last 15m', value: '15m' },
  { label: 'Last 1h', value: '1h' },
  { label: 'Last 6h', value: '6h' },
  { label: 'Last 24h', value: '24h' },
]

export default function Traces() {
  const [service, setService] = useState<string>('')
  const [operation, setOperation] = useState<string>('')
  const [lookback, setLookback] = useState<string>('1h')
  const [minDuration, setMinDuration] = useState<string>('')
  const [searchParams, setSearchParams] = useState<Record<string, string>>({})
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null)

  const { data: services = [], isLoading: servicesLoading } = useTraceServices()
  const { data: operations = [] } = useTraceOperations(service)
  const { data: traces = [], isLoading: tracesLoading, error: tracesError } = useTraceSearch(searchParams)
  const { data: expandedTrace } = useTrace(expandedTraceId ?? '')

  const handleSearch = () => {
    const params: Record<string, string> = {}
    if (service) params.service = service
    if (operation) params.operation = operation
    if (lookback) params.lookback = lookback
    if (minDuration) params.minDuration = minDuration
    setSearchParams(params)
    setExpandedTraceId(null)
  }

  const handleServiceChange = (value: string) => {
    setService(value === '_all' ? '' : value)
    setOperation('')
  }

  const sortedTraces = useMemo(() => {
    return [...traces].sort((a, b) => (b.startTime ?? 0) - (a.startTime ?? 0))
  }, [traces])

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Activity className="h-6 w-6" />
        <h1 className="text-2xl font-bold">Distributed Traces</h1>
      </div>

      {/* Search Section */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Search Traces</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
            <div className="space-y-2">
              <Label>Service</Label>
              <Select value={service || '_all'} onValueChange={handleServiceChange}>
                <SelectTrigger>
                  <SelectValue placeholder={servicesLoading ? 'Loading...' : 'All Services'} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_all">All Services</SelectItem>
                  {services.map((svc) => (
                    <SelectItem key={svc} value={svc}>{svc}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>Operation</Label>
              <Select
                value={operation || '_all'}
                onValueChange={(v) => setOperation(v === '_all' ? '' : v)}
                disabled={!service}
              >
                <SelectTrigger>
                  <SelectValue placeholder={!service ? 'Select service first' : 'All Operations'} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_all">All Operations</SelectItem>
                  {operations.map((op) => (
                    <SelectItem key={op} value={op}>{op}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>Time Range</Label>
              <Select value={lookback} onValueChange={setLookback}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LOOKBACK_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>Min Duration</Label>
              <Input
                placeholder="e.g., 100ms, 1s"
                value={minDuration}
                onChange={(e) => setMinDuration(e.target.value)}
              />
            </div>

            <div className="flex items-end">
              <Button onClick={handleSearch} className="w-full">
                <Search className="h-4 w-4 mr-2" />
                Search
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Results */}
      {tracesLoading ? (
        <Card>
          <CardContent className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
          </CardContent>
        </Card>
      ) : tracesError ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Failed to Load Traces</p>
            <p className="text-sm mt-1">Tracing backend may not be configured or reachable</p>
          </CardContent>
        </Card>
      ) : Object.keys(searchParams).length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
            <Search className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Search for Traces</p>
            <p className="text-sm mt-1">Select search criteria and click Search to find traces</p>
          </CardContent>
        </Card>
      ) : sortedTraces.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
            <p className="text-lg font-medium">No Traces Found</p>
            <p className="text-sm mt-1">Try adjusting your search criteria</p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              Results
              <Badge variant="secondary" className="ml-2">{sortedTraces.length} traces</Badge>
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead>Trace ID</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Operation</TableHead>
                  <TableHead className="text-right">Duration</TableHead>
                  <TableHead className="text-right">Spans</TableHead>
                  <TableHead>Timestamp</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedTraces.map((trace) => {
                  const isExpanded = expandedTraceId === trace.traceID
                  const timestamp = trace.startTime
                    ? format(new Date(trace.startTime / 1000), 'yyyy-MM-dd HH:mm:ss')
                    : 'N/A'

                  return (
                    <TableRow
                      key={trace.traceID}
                      className="cursor-pointer"
                      onClick={() => setExpandedTraceId(isExpanded ? null : trace.traceID)}
                    >
                      <TableCell className="w-8">
                        {isExpanded ? (
                          <ChevronDown className="h-4 w-4" />
                        ) : (
                          <ChevronRight className="h-4 w-4" />
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {trace.traceID.slice(0, 12)}...
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">{trace.services?.[0] ?? 'unknown'}</Badge>
                      </TableCell>
                      <TableCell className="text-sm">
                        {trace.operationName ?? trace.spans?.[0]?.operationName ?? '-'}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm">
                        {trace.duration != null
                          ? formatDuration(trace.duration / 1000)
                          : '-'}
                      </TableCell>
                      <TableCell className="text-right">
                        {trace.spans?.length ?? 0}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {timestamp}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>

            {/* Expanded trace detail */}
            {expandedTraceId && expandedTrace && (
              <div className="border-t border-border p-4 bg-muted/20">
                <div className="mb-4 space-y-1">
                  <div className="text-sm">
                    <span className="text-muted-foreground">Trace ID: </span>
                    <span className="font-mono">{expandedTrace.traceID}</span>
                  </div>
                  <div className="text-sm">
                    <span className="text-muted-foreground">Services: </span>
                    <span className="flex gap-1 mt-1 flex-wrap">
                      {expandedTrace.services?.map((svc) => (
                        <Badge key={svc} variant="outline">{svc}</Badge>
                      ))}
                    </span>
                  </div>
                  <div className="text-sm">
                    <span className="text-muted-foreground">Total Spans: </span>
                    <span>{expandedTrace.spans?.length ?? 0}</span>
                  </div>
                </div>
                <Card>
                  <CardHeader>
                    <CardTitle className="text-sm font-medium">Span Timeline</CardTitle>
                  </CardHeader>
                  <CardContent className="p-0 overflow-hidden">
                    <TraceTimeline spans={expandedTrace.spans ?? []} />
                  </CardContent>
                </Card>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
