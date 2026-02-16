import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { AlertCircle, Clock, RotateCcw, Eye } from 'lucide-react'
import { api } from '@/api/client'
import { useApp } from '@/contexts/AppContext'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ResourceDialog } from '@/components/resources/ResourceDialog'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from '@/hooks/use-toast'

interface HistoryEntry {
  id: string
  timestamp: string
  type: 'create' | 'update' | 'delete'
  resourceType: string
  resourceName: string
  namespace: string
  snapshot?: string
}

interface HistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function HistoryDialog({ open, onOpenChange }: HistoryDialogProps) {
  const { mode } = useApp()
  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selectedEntry, setSelectedEntry] = useState<HistoryEntry | null>(null)
  const [viewDialogOpen, setViewDialogOpen] = useState(false)
  const [restoreDialogOpen, setRestoreDialogOpen] = useState(false)
  const queryClient = useQueryClient()

  useEffect(() => {
    if (open) {
      fetchHistory()
    }
  }, [open])

  const fetchHistory = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const data = await api.config.history()
      setHistory(data)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch history')
    } finally {
      setIsLoading(false)
    }
  }

  const handleView = (entry: HistoryEntry) => {
    setSelectedEntry(entry)
    setViewDialogOpen(true)
  }

  const handleRestoreClick = (entry: HistoryEntry) => {
    setSelectedEntry(entry)
    setRestoreDialogOpen(true)
  }

  const handleRestore = async () => {
    if (!selectedEntry) return

    try {
      await api.config.restore(selectedEntry.id)
      toast({
        title: 'Configuration restored successfully',
        variant: 'success' as const,
      })
      // Invalidate all resource queries
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      queryClient.invalidateQueries({ queryKey: ['vips'] })
      queryClient.invalidateQueries({ queryKey: ['policies'] })
      onOpenChange(false)
    } catch (e) {
      toast({
        title: 'Failed to restore configuration',
        description: e instanceof Error ? e.message : 'Unknown error',
        variant: 'destructive',
      })
    }
  }

  const getTypeColor = (type: string) => {
    switch (type) {
      case 'create':
        return 'bg-green-500'
      case 'update':
        return 'bg-blue-500'
      case 'delete':
        return 'bg-red-500'
      default:
        return 'bg-gray-500'
    }
  }

  if (mode === 'kubernetes') {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Configuration History</DialogTitle>
            <DialogDescription>
              Configuration history is only available in standalone mode
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-center">
              In Kubernetes mode, configuration history is managed through
              Kubernetes' native versioning and etcd.
            </p>
          </div>
          <DialogFooter>
            <Button onClick={() => onOpenChange(false)}>Close</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>Configuration History</DialogTitle>
            <DialogDescription>
              View and restore previous configurations
            </DialogDescription>
          </DialogHeader>

          <div className="flex-1 overflow-auto">
            {isLoading ? (
              <div className="flex items-center justify-center py-12">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
              </div>
            ) : error ? (
              <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                <AlertCircle className="h-12 w-12 mb-4 text-destructive" />
                <p>{error}</p>
                <Button variant="outline" className="mt-4" onClick={fetchHistory}>
                  Retry
                </Button>
              </div>
            ) : history.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                <Clock className="h-12 w-12 mb-4" />
                <p>No history entries found</p>
              </div>
            ) : (
              <div className="space-y-3">
                {history.map((entry) => (
                  <Card key={entry.id}>
                    <CardContent className="py-3">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-3">
                          <Badge
                            className={`${getTypeColor(entry.type)} text-white`}
                          >
                            {entry.type}
                          </Badge>
                          <div>
                            <p className="font-medium text-sm">
                              {entry.resourceType}/{entry.resourceName}
                            </p>
                            <p className="text-xs text-muted-foreground">
                              {entry.namespace} •{' '}
                              {new Date(entry.timestamp).toLocaleString()}
                            </p>
                          </div>
                        </div>
                        <div className="flex items-center gap-2">
                          {entry.snapshot && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleView(entry)}
                            >
                              <Eye className="h-4 w-4" />
                            </Button>
                          )}
                          {entry.snapshot && entry.type !== 'delete' && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleRestoreClick(entry)}
                            >
                              <RotateCcw className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {selectedEntry && (
        <>
          <ResourceDialog
            open={viewDialogOpen}
            onOpenChange={setViewDialogOpen}
            title={`${selectedEntry.resourceType}/${selectedEntry.resourceName}`}
            description={`Snapshot from ${new Date(
              selectedEntry.timestamp
            ).toLocaleString()}`}
            mode="view"
            resource={selectedEntry.snapshot ? JSON.parse(selectedEntry.snapshot) : {}}
            onSubmit={async () => {}}
            readOnly
          />

          <ConfirmDialog
            open={restoreDialogOpen}
            onOpenChange={setRestoreDialogOpen}
            title="Restore Configuration"
            description={`Are you sure you want to restore ${selectedEntry.resourceType}/${selectedEntry.resourceName} to the state from ${new Date(
              selectedEntry.timestamp
            ).toLocaleString()}?`}
            confirmLabel="Restore"
            onConfirm={handleRestore}
          />
        </>
      )}
    </>
  )
}
