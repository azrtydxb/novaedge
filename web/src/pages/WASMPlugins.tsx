import { useState } from 'react'
import { useWASMPlugins, useWASMPlugin } from '@/api/hooks'
import type { WASMPluginStatus } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  Puzzle,
  AlertCircle,
  Activity,
  Clock,
  MemoryStick,
  Timer,
} from 'lucide-react'
import { MetricCard } from '@/components/metrics/MetricCard'

export default function WASMPlugins() {
  const { data: plugins = [], isLoading, error } = useWASMPlugins()
  const [selectedPlugin, setSelectedPlugin] = useState<string>('')
  const [detailOpen, setDetailOpen] = useState(false)
  const { data: pluginDetail } = useWASMPlugin(selectedPlugin)

  const columns: Column<WASMPluginStatus>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.name,
      sortable: true,
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => (
        <Badge
          className={
            row.loaded
              ? 'bg-green-500 hover:bg-green-600'
              : 'bg-red-500 hover:bg-red-600'
          }
        >
          {row.loaded ? 'Loaded' : 'Error'}
        </Badge>
      ),
    },
    {
      key: 'invocations',
      header: 'Invocations',
      accessor: (row) => row.invocations.toLocaleString(),
      sortable: true,
    },
    {
      key: 'errors',
      header: 'Errors',
      accessor: (row) => (
        <span className={row.errors > 0 ? 'text-red-500 font-medium' : ''}>
          {row.errors.toLocaleString()}
        </span>
      ),
      sortable: true,
    },
    {
      key: 'avgLatency',
      header: 'Avg Latency',
      accessor: (row) => `${row.avgLatencyMs.toFixed(2)} ms`,
      sortable: true,
    },
  ]

  const handleRowClick = (row: WASMPluginStatus) => {
    setSelectedPlugin(row.name)
    setDetailOpen(true)
  }

  const getRowKey = (row: WASMPluginStatus) => row.name

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load WASM plugins: {error.message}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <DataTable
        data={plugins}
        columns={columns}
        getRowKey={getRowKey}
        onRowClick={handleRowClick}
        isLoading={isLoading}
        emptyMessage="No WASM plugins loaded"
        searchPlaceholder="Search plugins..."
        searchFilter={(row, query) =>
          row.name.toLowerCase().includes(query) || false
        }
      />

      {/* Plugin Detail Dialog */}
      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Puzzle className="h-5 w-5" />
              {selectedPlugin}
            </DialogTitle>
            <DialogDescription>WASM plugin details and metrics</DialogDescription>
          </DialogHeader>

          {pluginDetail ? (
            <div className="space-y-6">
              {/* Plugin Metrics */}
              <div className="grid gap-4 md:grid-cols-3">
                <MetricCard
                  title="Invocations"
                  value={
                    plugins
                      .find((p) => p.name === selectedPlugin)
                      ?.invocations.toLocaleString() ?? '-'
                  }
                  icon={<Activity className="h-4 w-4" />}
                />
                <MetricCard
                  title="Errors"
                  value={
                    plugins
                      .find((p) => p.name === selectedPlugin)
                      ?.errors.toLocaleString() ?? '-'
                  }
                  icon={<AlertCircle className="h-4 w-4" />}
                />
                <MetricCard
                  title="Avg Latency"
                  value={`${
                    plugins
                      .find((p) => p.name === selectedPlugin)
                      ?.avgLatencyMs.toFixed(2) ?? '-'
                  } ms`}
                  icon={<Clock className="h-4 w-4" />}
                />
              </div>

              {/* Resource Limits */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm font-medium">Resource Limits</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="flex items-center gap-2">
                      <MemoryStick className="h-4 w-4 text-muted-foreground" />
                      <span className="text-sm text-muted-foreground">Memory Limit:</span>
                      <span className="text-sm font-medium">
                        {pluginDetail.memoryLimitMB
                          ? `${pluginDetail.memoryLimitMB} MB`
                          : 'Unlimited'}
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <Timer className="h-4 w-4 text-muted-foreground" />
                      <span className="text-sm text-muted-foreground">Timeout:</span>
                      <span className="text-sm font-medium">
                        {pluginDetail.timeoutMs
                          ? `${pluginDetail.timeoutMs} ms`
                          : 'No limit'}
                      </span>
                    </div>
                  </div>
                </CardContent>
              </Card>

              {/* Plugin Configuration */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm font-medium">Configuration</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    <div className="grid gap-2 text-sm">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Path</span>
                        <span className="font-mono text-xs">{pluginDetail.path}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Phase</span>
                        <Badge variant="outline">{pluginDetail.phase ?? 'request'}</Badge>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Priority</span>
                        <span>{pluginDetail.priority ?? 0}</span>
                      </div>
                    </div>
                    {pluginDetail.config && Object.keys(pluginDetail.config).length > 0 && (
                      <div>
                        <p className="text-sm text-muted-foreground mb-2">Plugin Config:</p>
                        <pre className="rounded-md bg-muted p-3 text-xs overflow-x-auto">
                          {JSON.stringify(pluginDetail.config, null, 2)}
                        </pre>
                      </div>
                    )}
                  </div>
                </CardContent>
              </Card>
            </div>
          ) : (
            <div className="flex items-center justify-center h-40">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
