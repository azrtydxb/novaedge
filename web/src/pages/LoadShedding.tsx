import { useState, useEffect } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useOverloadStatus,
  useOverloadConfig,
  useUpdateOverloadConfig,
} from '@/api/hooks'
import type { OverloadConfig } from '@/api/types'
import { MetricCard } from '@/components/metrics/MetricCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  ShieldAlert,
  MemoryStick,
  Activity,
  Server,
  AlertTriangle,
  CheckCircle,
  Save,
  Info,
} from 'lucide-react'

export default function LoadShedding() {
  const { readOnly } = useApp()
  const { data: status, isLoading: statusLoading, error: statusError } = useOverloadStatus()
  const { data: config, isLoading: configLoading, error: configError } = useOverloadConfig()
  const updateConfig = useUpdateOverloadConfig()

  const [formConfig, setFormConfig] = useState<OverloadConfig>({
    enabled: false,
    heapMemoryTriggerPercent: 90,
    heapMemoryRecoverPercent: 80,
    goroutineTriggerCount: 10000,
    goroutineRecoverCount: 8000,
    activeConnectionTriggerCount: 50000,
    activeConnectionRecoverCount: 40000,
    checkInterval: '1s',
  })

  useEffect(() => {
    if (config) {
      setFormConfig(config)
    }
  }, [config])

  const handleSave = () => {
    updateConfig.mutate(formConfig)
  }

  const isOverloaded = status?.state === 'overloaded'

  return (
    <div className="space-y-6">
      {/* Status Header */}
      <Card className={isOverloaded ? 'border-red-500/50' : 'border-green-500/50'}>
        <CardContent className="py-4">
          <div className="flex items-center gap-4">
            <div
              className={`flex h-12 w-12 items-center justify-center rounded-full ${
                isOverloaded
                  ? 'bg-red-500/20 text-red-500'
                  : 'bg-green-500/20 text-green-500'
              }`}
            >
              {isOverloaded ? (
                <AlertTriangle className="h-6 w-6" />
              ) : (
                <CheckCircle className="h-6 w-6" />
              )}
            </div>
            <div>
              <h2 className="text-xl font-semibold">Load Shedding Status</h2>
              <div className="flex items-center gap-2 mt-1">
                {statusLoading ? (
                  <Badge variant="secondary">Loading...</Badge>
                ) : statusError ? (
                  <Badge variant="destructive">Error</Badge>
                ) : (
                  <Badge
                    className={
                      isOverloaded
                        ? 'bg-red-500 hover:bg-red-600'
                        : 'bg-green-500 hover:bg-green-600'
                    }
                  >
                    {isOverloaded ? 'Overloaded' : 'Normal'}
                  </Badge>
                )}
                <span className="text-sm text-muted-foreground">
                  Auto-refreshes every 5 seconds
                </span>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Status Metrics Grid */}
      {statusError ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-40 text-muted-foreground">
            <ShieldAlert className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Status Unavailable</p>
            <p className="text-sm mt-1">{statusError.message}</p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <MetricCard
            title="Heap Memory Usage"
            value={status ? `${(status.heapUsageRatio * 100).toFixed(1)}%` : '-'}
            icon={<MemoryStick className="h-4 w-4" />}
            subtitle="Current heap utilization"
          />
          <MetricCard
            title="Goroutine Count"
            value={status?.goroutineCount ?? '-'}
            icon={<Activity className="h-4 w-4" />}
            subtitle="Active goroutines"
          />
          <MetricCard
            title="Active Connections"
            value={status?.activeConnections ?? '-'}
            icon={<Server className="h-4 w-4" />}
            subtitle="Current open connections"
          />
          <MetricCard
            title="Total Requests Shed"
            value={status?.totalShed ?? '-'}
            icon={<ShieldAlert className="h-4 w-4" />}
            subtitle="Requests rejected due to overload"
          />
        </div>
      )}

      {/* Configuration Section */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <ShieldAlert className="h-4 w-4" />
            Load Shedding Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          {configLoading ? (
            <div className="flex items-center justify-center h-40">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
          ) : configError ? (
            <div className="text-center py-8 text-muted-foreground">
              Failed to load configuration: {configError.message}
            </div>
          ) : (
            <div className="space-y-6">
              {/* Enable/Disable Toggle */}
              <div className="flex items-center gap-3">
                <Label htmlFor="enabled" className="font-medium">
                  Enable Load Shedding
                </Label>
                <Button
                  id="enabled"
                  variant={formConfig.enabled ? 'default' : 'outline'}
                  size="sm"
                  onClick={() =>
                    setFormConfig((prev) => ({ ...prev, enabled: !prev.enabled }))
                  }
                  disabled={readOnly}
                >
                  {formConfig.enabled ? 'Enabled' : 'Disabled'}
                </Button>
              </div>

              {/* Hysteresis Info */}
              <div className="rounded-md bg-muted/50 p-3 flex items-start gap-2">
                <Info className="h-4 w-4 mt-0.5 text-muted-foreground shrink-0" />
                <p className="text-sm text-muted-foreground">
                  Load shedding uses hysteresis to prevent flapping. The trigger threshold activates shedding,
                  and the recover threshold (which must be lower) deactivates it.
                  This prevents rapid on/off cycling when values hover near a single threshold.
                </p>
              </div>

              {/* Threshold Forms */}
              <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
                {/* Heap Memory */}
                <div className="space-y-3">
                  <h4 className="font-medium text-sm flex items-center gap-2">
                    <MemoryStick className="h-4 w-4" />
                    Heap Memory
                  </h4>
                  <div className="space-y-2">
                    <div>
                      <Label htmlFor="heapTrigger" className="text-xs text-muted-foreground">
                        Trigger (%)
                      </Label>
                      <Input
                        id="heapTrigger"
                        type="number"
                        min={0}
                        max={100}
                        value={formConfig.heapMemoryTriggerPercent ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            heapMemoryTriggerPercent: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                    <div>
                      <Label htmlFor="heapRecover" className="text-xs text-muted-foreground">
                        Recover (%)
                      </Label>
                      <Input
                        id="heapRecover"
                        type="number"
                        min={0}
                        max={100}
                        value={formConfig.heapMemoryRecoverPercent ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            heapMemoryRecoverPercent: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                  </div>
                </div>

                {/* Goroutines */}
                <div className="space-y-3">
                  <h4 className="font-medium text-sm flex items-center gap-2">
                    <Activity className="h-4 w-4" />
                    Goroutines
                  </h4>
                  <div className="space-y-2">
                    <div>
                      <Label htmlFor="goroutineTrigger" className="text-xs text-muted-foreground">
                        Trigger Count
                      </Label>
                      <Input
                        id="goroutineTrigger"
                        type="number"
                        min={0}
                        value={formConfig.goroutineTriggerCount ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            goroutineTriggerCount: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                    <div>
                      <Label htmlFor="goroutineRecover" className="text-xs text-muted-foreground">
                        Recover Count
                      </Label>
                      <Input
                        id="goroutineRecover"
                        type="number"
                        min={0}
                        value={formConfig.goroutineRecoverCount ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            goroutineRecoverCount: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                  </div>
                </div>

                {/* Active Connections */}
                <div className="space-y-3">
                  <h4 className="font-medium text-sm flex items-center gap-2">
                    <Server className="h-4 w-4" />
                    Active Connections
                  </h4>
                  <div className="space-y-2">
                    <div>
                      <Label htmlFor="connTrigger" className="text-xs text-muted-foreground">
                        Trigger Count
                      </Label>
                      <Input
                        id="connTrigger"
                        type="number"
                        min={0}
                        value={formConfig.activeConnectionTriggerCount ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            activeConnectionTriggerCount: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                    <div>
                      <Label htmlFor="connRecover" className="text-xs text-muted-foreground">
                        Recover Count
                      </Label>
                      <Input
                        id="connRecover"
                        type="number"
                        min={0}
                        value={formConfig.activeConnectionRecoverCount ?? ''}
                        onChange={(e) =>
                          setFormConfig((prev) => ({
                            ...prev,
                            activeConnectionRecoverCount: Number(e.target.value),
                          }))
                        }
                        disabled={readOnly}
                      />
                    </div>
                  </div>
                </div>
              </div>

              {/* Check Interval */}
              <div className="max-w-xs">
                <Label htmlFor="checkInterval" className="text-xs text-muted-foreground">
                  Check Interval (e.g. "1s", "500ms")
                </Label>
                <Input
                  id="checkInterval"
                  type="text"
                  value={formConfig.checkInterval ?? ''}
                  onChange={(e) =>
                    setFormConfig((prev) => ({
                      ...prev,
                      checkInterval: e.target.value,
                    }))
                  }
                  disabled={readOnly}
                />
              </div>

              {/* Save Button */}
              {!readOnly && (
                <div className="flex justify-end">
                  <Button
                    onClick={handleSave}
                    disabled={updateConfig.isPending}
                  >
                    <Save className="h-4 w-4 mr-2" />
                    {updateConfig.isPending ? 'Saving...' : 'Save Configuration'}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
