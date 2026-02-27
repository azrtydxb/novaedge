import { useApp } from '@/contexts/AppContext'
import { useAgents } from '@/api/hooks'
import type { AgentInfo } from '@/api/types'
import { DataTable, Column } from '@/components/common/DataTable'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { AlertCircle, Server, Cpu, HardDrive, FileText } from 'lucide-react'
import { formatAge, formatBytes } from '@/lib/utils'
import { useNavigate } from 'react-router-dom'

export default function Agents() {
  const { mode } = useApp()
  const { data: agents = [], isLoading, error } = useAgents()
  const navigate = useNavigate()

  const columns: Column<AgentInfo>[] = [
    {
      key: 'name',
      header: 'Name',
      accessor: (row) => row.podName ?? '-',
      sortable: true,
    },
    {
      key: 'node',
      header: 'Node',
      accessor: (row) => row.nodeName ?? '-',
      sortable: true,
    },
    {
      key: 'ip',
      header: 'IP Address',
      accessor: (row) => (
        <code className="px-2 py-1 bg-muted rounded text-sm">
          {row.podIP ?? '-'}
        </code>
      ),
    },
    {
      key: 'status',
      header: 'Status',
      accessor: (row) => (
        <Badge
          variant={row.ready ? 'default' : 'secondary'}
          className={row.ready ? 'bg-green-500 hover:bg-green-600' : ''}
        >
          {row.ready ? 'Ready' : 'Not Ready'}
        </Badge>
      ),
    },
    {
      key: 'mesh',
      header: 'Mesh Status',
      accessor: (row) => {
        // Mesh status is inferred from annotations or metadata if available
        const meshEnabled = row.podName && row.ready
        return (
          <Badge
            variant={meshEnabled ? 'default' : 'secondary'}
            className={meshEnabled ? 'bg-blue-500 hover:bg-blue-600' : ''}
          >
            {meshEnabled ? 'Enrolled' : 'Not Enrolled'}
          </Badge>
        )
      },
    },
    {
      key: 'version',
      header: 'Version',
      accessor: (row) => row.version ?? '-',
    },
    {
      key: 'uptime',
      header: 'Uptime',
      accessor: (row) =>
        row.startTime ? formatAge(row.startTime) : '-',
      sortable: true,
    },
  ]

  const getRowKey = (row: AgentInfo) => row.podName ?? row.nodeName ?? ''

  if (mode === 'standalone') {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
          <AlertCircle className="h-12 w-12 mb-4" />
          <p className="text-lg font-medium">Agents Not Available</p>
          <p className="text-sm mt-1">
            Agent information is only available in Kubernetes mode
          </p>
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <div className="text-center py-12 text-destructive">
        Failed to load agents: {error.message}
      </div>
    )
  }

  const readyAgents = agents.filter((a) => a.ready).length
  const totalAgents = agents.length

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total Agents
            </CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalAgents}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Ready Agents
            </CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">
              {readyAgents}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              {totalAgents > 0
                ? `${((readyAgents / totalAgents) * 100).toFixed(0)}% healthy`
                : 'No agents'}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Not Ready
            </CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div
              className={`text-2xl font-bold ${
                totalAgents - readyAgents > 0 ? 'text-red-600' : ''
              }`}
            >
              {totalAgents - readyAgents}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Agents table */}
      <DataTable
        data={agents}
        columns={columns}
        getRowKey={getRowKey}
        isLoading={isLoading}
        emptyMessage="No agents found"
        searchPlaceholder="Search agents..."
        searchFilter={(row, query) =>
          row.podName?.toLowerCase().includes(query) ||
          row.nodeName?.toLowerCase().includes(query) ||
          row.podIP?.toLowerCase().includes(query) ||
          false
        }
        actions={(row) => [
          {
            label: 'View Logs',
            onClick: () => {
              const podName = row.podName ?? ''
              const ns = row.namespace ?? 'nova-system'
              navigate(`/logs?pod=${encodeURIComponent(podName)}&namespace=${encodeURIComponent(ns)}`)
            },
          },
        ]}
      />

      {/* Agent details */}
      {agents.length > 0 && (
        <div className="grid gap-4 md:grid-cols-2">
          {agents.map((agent) => (
            <Card key={getRowKey(agent)}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm font-medium">
                    {agent.podName}
                  </CardTitle>
                  <div className="flex items-center gap-2">
                    <Badge
                      variant={agent.ready ? 'default' : 'secondary'}
                      className={agent.ready ? 'bg-green-500' : ''}
                    >
                      {agent.ready ? 'Ready' : 'Not Ready'}
                    </Badge>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        const podName = agent.podName ?? ''
                        const ns = agent.namespace ?? 'nova-system'
                        navigate(`/logs?pod=${encodeURIComponent(podName)}&namespace=${encodeURIComponent(ns)}`)
                      }}
                    >
                      <FileText className="h-3 w-3 mr-1" />
                      Logs
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Node</span>
                  <span>{agent.nodeName ?? '-'}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">IP</span>
                  <code className="px-2 py-0.5 bg-muted rounded text-xs">
                    {agent.podIP ?? '-'}
                  </code>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Version</span>
                  <span>{agent.version ?? '-'}</span>
                </div>
                {agent.configVersion && (
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Config Version</span>
                    <span className="font-mono text-xs">
                      {agent.configVersion}
                    </span>
                  </div>
                )}
                {agent.metrics && (
                  <>
                    <div className="border-t pt-2 mt-2">
                      <span className="text-muted-foreground font-medium">
                        Metrics
                      </span>
                    </div>
                    {agent.metrics.activeConnections !== undefined && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">
                          Active Connections
                        </span>
                        <span>{agent.metrics.activeConnections}</span>
                      </div>
                    )}
                    {agent.metrics.requestsPerSecond !== undefined && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Requests/sec</span>
                        <span>{agent.metrics.requestsPerSecond.toFixed(2)}</span>
                      </div>
                    )}
                    {agent.metrics.memoryUsage !== undefined && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Memory</span>
                        <span>{formatBytes(agent.metrics.memoryUsage)}</span>
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
