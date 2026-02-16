import { useState, useRef, useCallback } from 'react'
import { useApp } from '@/contexts/AppContext'
import {
  useConfigSnapshots,
  useCreateConfigSnapshot,
  useRollbackConfig,
  useConfigDiff,
} from '@/api/hooks'
import { api } from '@/api/client'
import type { ConfigSnapshot, ImportResult, ValidationResult } from '@/api/types'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Download,
  Upload,
  Camera,
  RotateCcw,
  AlertCircle,
  CheckCircle2,
  FileText,
  GitCompare,
} from 'lucide-react'
import { toast } from '@/hooks/use-toast'
import { useQueryClient } from '@tanstack/react-query'
import ReactDiffViewer from 'react-diff-viewer-continued'

export default function Config() {
  return (
    <Tabs defaultValue="export-import" className="space-y-4">
      <TabsList>
        <TabsTrigger value="export-import" className="flex items-center gap-2">
          <FileText className="h-4 w-4" />
          Export / Import
        </TabsTrigger>
        <TabsTrigger value="snapshots" className="flex items-center gap-2">
          <Camera className="h-4 w-4" />
          Snapshots
        </TabsTrigger>
        <TabsTrigger value="diff" className="flex items-center gap-2">
          <GitCompare className="h-4 w-4" />
          Diff
        </TabsTrigger>
      </TabsList>

      <TabsContent value="export-import">
        <ExportImportTab />
      </TabsContent>
      <TabsContent value="snapshots">
        <SnapshotsTab />
      </TabsContent>
      <TabsContent value="diff">
        <DiffTab />
      </TabsContent>
    </Tabs>
  )
}

// ---- Export / Import Tab ----
function ExportImportTab() {
  const { namespace } = useApp()
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [yamlContent, setYamlContent] = useState('')
  const [dryRun, setDryRun] = useState(false)
  const [importResult, setImportResult] = useState<ImportResult | null>(null)
  const [validationResult, setValidationResult] = useState<ValidationResult | null>(null)
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [isValidating, setIsValidating] = useState(false)

  const handleExport = useCallback(async () => {
    setIsExporting(true)
    try {
      const blob = await api.config.export(namespace)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'novaedge-config.yaml'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
      toast({ title: 'Configuration exported', variant: 'success' as const })
    } catch (e) {
      toast({
        title: 'Export failed',
        description: e instanceof Error ? e.message : 'Unknown error',
        variant: 'destructive',
      })
    } finally {
      setIsExporting(false)
    }
  }, [namespace])

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) {
      const reader = new FileReader()
      reader.onload = (ev) => {
        const content = ev.target?.result as string
        setYamlContent(content)
        setImportResult(null)
        setValidationResult(null)
      }
      reader.readAsText(file)
    }
  }

  const handleValidate = async () => {
    if (!yamlContent.trim()) {
      toast({ title: 'No content to validate', variant: 'destructive' })
      return
    }
    setIsValidating(true)
    setValidationResult(null)
    try {
      const result = await api.config.validate(yamlContent)
      setValidationResult(result)
      if (result.valid) {
        toast({ title: 'Configuration is valid', variant: 'success' as const })
      } else {
        toast({ title: 'Validation found issues', variant: 'destructive' })
      }
    } catch (e) {
      toast({
        title: 'Validation failed',
        description: e instanceof Error ? e.message : 'Unknown error',
        variant: 'destructive',
      })
    } finally {
      setIsValidating(false)
    }
  }

  const handleImport = async () => {
    if (!yamlContent.trim()) {
      toast({ title: 'No content to import', variant: 'destructive' })
      return
    }
    setIsImporting(true)
    setImportResult(null)
    try {
      const result = await api.config.import(yamlContent, dryRun)
      setImportResult(result)
      if (!dryRun) {
        queryClient.invalidateQueries()
        toast({ title: 'Configuration imported successfully', variant: 'success' as const })
      } else {
        toast({ title: 'Dry run completed', variant: 'success' as const })
      }
    } catch (e) {
      toast({
        title: 'Import failed',
        description: e instanceof Error ? e.message : 'Unknown error',
        variant: 'destructive',
      })
    } finally {
      setIsImporting(false)
    }
  }

  return (
    <div className="space-y-6">
      {/* Export */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Download className="h-4 w-4" />
            Export Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground mb-4">
            Export the current configuration as a YAML file. Namespace: {namespace === 'all' ? 'All namespaces' : namespace}
          </p>
          <Button onClick={handleExport} disabled={isExporting}>
            <Download className="h-4 w-4 mr-2" />
            {isExporting ? 'Exporting...' : 'Export Configuration'}
          </Button>
        </CardContent>
      </Card>

      {/* Import */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Upload className="h-4 w-4" />
            Import Configuration
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* File upload drop zone */}
          <div className="border-2 border-dashed border-muted-foreground/25 rounded-lg p-6 text-center">
            <input
              ref={fileInputRef}
              type="file"
              accept=".yaml,.yml"
              onChange={handleFileChange}
              className="hidden"
            />
            <Upload className="h-10 w-10 mx-auto text-muted-foreground mb-3" />
            <p className="text-sm text-muted-foreground mb-2">
              Drop a YAML file here, or
            </p>
            <Button variant="outline" size="sm" onClick={() => fileInputRef.current?.click()}>
              Browse Files
            </Button>
          </div>

          {/* Or paste */}
          <div className="space-y-2">
            <Label>Or paste YAML content</Label>
            <Textarea
              className="h-[200px] font-mono text-sm"
              value={yamlContent}
              onChange={(e) => {
                setYamlContent(e.target.value)
                setImportResult(null)
                setValidationResult(null)
              }}
              placeholder="Paste YAML configuration here..."
            />
          </div>

          {/* Actions */}
          <div className="flex items-center gap-3">
            <Button variant="outline" onClick={handleValidate} disabled={isValidating || !yamlContent.trim()}>
              <CheckCircle2 className="h-4 w-4 mr-2" />
              {isValidating ? 'Validating...' : 'Validate'}
            </Button>
            <div className="flex items-center gap-2">
              <input
                id="dry-run"
                type="checkbox"
                checked={dryRun}
                onChange={(e) => setDryRun(e.target.checked)}
                className="rounded border-gray-300"
              />
              <Label htmlFor="dry-run" className="text-sm cursor-pointer">
                Dry Run
              </Label>
            </div>
            <Button onClick={handleImport} disabled={isImporting || !yamlContent.trim()}>
              <Upload className="h-4 w-4 mr-2" />
              {isImporting ? 'Importing...' : dryRun ? 'Dry Run Import' : 'Import'}
            </Button>
          </div>

          {/* Validation result */}
          {validationResult && (
            <div className={`rounded-lg p-4 ${validationResult.valid ? 'bg-green-500/10 border border-green-500/30' : 'bg-red-500/10 border border-red-500/30'}`}>
              <div className="flex items-center gap-2 mb-2">
                {validationResult.valid ? (
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                ) : (
                  <AlertCircle className="h-4 w-4 text-red-500" />
                )}
                <span className="font-medium text-sm">
                  {validationResult.valid ? 'Configuration is valid' : 'Validation errors found'}
                </span>
              </div>
              {validationResult.errors && validationResult.errors.length > 0 && (
                <ul className="space-y-1 text-sm">
                  {validationResult.errors.map((err, i) => (
                    <li key={i} className="text-red-400">
                      <strong>{err.field}:</strong> {err.message}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* Import result */}
          {importResult && (
            <div className="rounded-lg p-4 bg-muted border">
              <p className="font-medium text-sm mb-2">
                Import Result {importResult.dryRun ? '(Dry Run)' : ''}
              </p>
              <div className="grid gap-2 text-sm">
                {importResult.created && importResult.created.length > 0 && (
                  <div className="flex items-center gap-2">
                    <Badge className="bg-green-500">Created</Badge>
                    <span>{importResult.created.length} resource(s)</span>
                  </div>
                )}
                {importResult.updated && importResult.updated.length > 0 && (
                  <div className="flex items-center gap-2">
                    <Badge className="bg-blue-500">Updated</Badge>
                    <span>{importResult.updated.length} resource(s)</span>
                  </div>
                )}
                {importResult.skipped && importResult.skipped.length > 0 && (
                  <div className="flex items-center gap-2">
                    <Badge variant="secondary">Skipped</Badge>
                    <span>{importResult.skipped.length} resource(s)</span>
                  </div>
                )}
                {importResult.errors && importResult.errors.length > 0 && (
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <Badge variant="destructive">Errors</Badge>
                      <span>{importResult.errors.length} error(s)</span>
                    </div>
                    {importResult.errors.map((err, i) => (
                      <p key={i} className="text-red-400 text-xs ml-4">
                        {err.resource.kind}/{err.resource.name}: {err.error}
                      </p>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// ---- Snapshots Tab ----
function SnapshotsTab() {
  const { data: snapshots = [], isLoading } = useConfigSnapshots()
  const createSnapshot = useCreateConfigSnapshot()
  const rollbackConfig = useRollbackConfig()
  const [comment, setComment] = useState('')
  const [rollbackDialogOpen, setRollbackDialogOpen] = useState(false)
  const [snapshotToRollback, setSnapshotToRollback] = useState<ConfigSnapshot | null>(null)

  const handleCreate = () => {
    createSnapshot.mutate(comment || undefined, {
      onSuccess: () => setComment(''),
    })
  }

  const handleRollback = (snapshot: ConfigSnapshot) => {
    setSnapshotToRollback(snapshot)
    setRollbackDialogOpen(true)
  }

  const confirmRollback = () => {
    if (snapshotToRollback) {
      rollbackConfig.mutate(snapshotToRollback.id)
    }
  }

  const formatTimestamp = (ts: string) => {
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }

  return (
    <div className="space-y-6">
      {/* Take snapshot */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Camera className="h-4 w-4" />
            Take Snapshot
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-3">
            <Input
              placeholder="Optional comment..."
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              className="max-w-md"
            />
            <Button onClick={handleCreate} disabled={createSnapshot.isPending}>
              <Camera className="h-4 w-4 mr-2" />
              {createSnapshot.isPending ? 'Creating...' : 'Take Snapshot'}
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Snapshot list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Snapshots</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center h-32">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
          ) : snapshots.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">
              No snapshots found. Take a snapshot to get started.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Timestamp</TableHead>
                  <TableHead>Comment</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {snapshots.map((snapshot) => (
                  <TableRow key={snapshot.id}>
                    <TableCell>
                      <code className="px-2 py-1 bg-muted rounded text-xs">
                        {snapshot.id.slice(0, 12)}...
                      </code>
                    </TableCell>
                    <TableCell>{formatTimestamp(snapshot.timestamp)}</TableCell>
                    <TableCell>{snapshot.comment || '-'}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleRollback(snapshot)}
                      >
                        <RotateCcw className="h-3 w-3 mr-1" />
                        Restore
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={rollbackDialogOpen}
        onOpenChange={setRollbackDialogOpen}
        title="Restore Snapshot"
        description={`Are you sure you want to restore to snapshot "${snapshotToRollback?.id.slice(0, 12)}..."? This will overwrite the current configuration.`}
        confirmLabel="Restore"
        variant="destructive"
        onConfirm={confirmRollback}
        isLoading={rollbackConfig.isPending}
      />
    </div>
  )
}

// ---- Diff Tab ----
function DiffTab() {
  const { data: snapshots = [] } = useConfigSnapshots()
  const [fromId, setFromId] = useState('')
  const [toId, setToId] = useState('')

  const { data: diffResult, isLoading: diffLoading, error: diffError } = useConfigDiff(fromId, toId)

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <GitCompare className="h-4 w-4" />
            Compare Snapshots
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4">
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground mb-1 block">From</Label>
              <Select value={fromId} onValueChange={setFromId}>
                <SelectTrigger>
                  <SelectValue placeholder="Select snapshot..." />
                </SelectTrigger>
                <SelectContent>
                  {snapshots.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.id.slice(0, 12)}... {s.comment ? `(${s.comment})` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground mb-1 block">To</Label>
              <Select value={toId} onValueChange={setToId}>
                <SelectTrigger>
                  <SelectValue placeholder="Select snapshot..." />
                </SelectTrigger>
                <SelectContent>
                  {snapshots.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.id.slice(0, 12)}... {s.comment ? `(${s.comment})` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Diff result */}
      {fromId && toId && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Diff Result</CardTitle>
          </CardHeader>
          <CardContent>
            {diffLoading ? (
              <div className="flex items-center justify-center h-32">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
              </div>
            ) : diffError ? (
              <div className="flex items-center gap-2 text-destructive">
                <AlertCircle className="h-4 w-4" />
                <span className="text-sm">Failed to load diff: {(diffError as Error).message}</span>
              </div>
            ) : diffResult ? (
              <div className="overflow-auto rounded border" style={{ maxHeight: '600px' }}>
                <ReactDiffViewer
                  oldValue={diffResult.from.config || ''}
                  newValue={diffResult.to.config || ''}
                  splitView={true}
                  useDarkTheme={true}
                  leftTitle={`From: ${diffResult.from.id.slice(0, 12)}...`}
                  rightTitle={`To: ${diffResult.to.id.slice(0, 12)}...`}
                />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground text-center py-8">
                Select two snapshots above to compare them.
              </p>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
