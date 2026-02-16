import { useState, useRef } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AlertCircle, Upload, FileText } from 'lucide-react'
import { api } from '@/api/client'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from '@/hooks/use-toast'

interface ImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ImportDialog({ open, onOpenChange }: ImportDialogProps) {
  const [yamlContent, setYamlContent] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<'paste' | 'file'>('paste')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const queryClient = useQueryClient()

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) {
      const reader = new FileReader()
      reader.onload = (e) => {
        const content = e.target?.result as string
        setYamlContent(content)
        setError(null)
      }
      reader.onerror = () => {
        setError('Failed to read file')
      }
      reader.readAsText(file)
    }
  }

  const handleImport = async () => {
    if (!yamlContent.trim()) {
      setError('Please provide YAML content to import')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      await api.config.import(yamlContent)
      toast({
        title: 'Configuration imported successfully',
        variant: 'success' as const,
      })
      // Invalidate all resource queries to refresh data
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      queryClient.invalidateQueries({ queryKey: ['vips'] })
      queryClient.invalidateQueries({ queryKey: ['policies'] })
      onOpenChange(false)
      setYamlContent('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to import configuration')
    } finally {
      setIsLoading(false)
    }
  }

  const handleClose = () => {
    onOpenChange(false)
    setYamlContent('')
    setError(null)
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Import Configuration</DialogTitle>
          <DialogDescription>
            Import NovaEdge resources from YAML. This will create or update resources.
          </DialogDescription>
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'paste' | 'file')}>
          <TabsList>
            <TabsTrigger value="paste" className="flex items-center gap-2">
              <FileText className="h-4 w-4" />
              Paste YAML
            </TabsTrigger>
            <TabsTrigger value="file" className="flex items-center gap-2">
              <Upload className="h-4 w-4" />
              Upload File
            </TabsTrigger>
          </TabsList>

          <TabsContent value="paste" className="mt-4">
            <div className="space-y-2">
              <Label htmlFor="yaml-content">YAML Content</Label>
              <Textarea
                id="yaml-content"
                className="h-[300px] font-mono text-sm"
                value={yamlContent}
                onChange={(e) => {
                  setYamlContent(e.target.value)
                  setError(null)
                }}
                placeholder={`apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
  namespace: default
spec:
  listeners:
    - name: http
      port: 80
      protocol: HTTP
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
...`}
              />
            </div>
          </TabsContent>

          <TabsContent value="file" className="mt-4">
            <div className="space-y-4">
              <div className="border-2 border-dashed border-muted-foreground/25 rounded-lg p-8 text-center">
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".yaml,.yml"
                  onChange={handleFileChange}
                  className="hidden"
                />
                <Upload className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
                <p className="text-muted-foreground mb-2">
                  Drag and drop a YAML file, or
                </p>
                <Button
                  variant="outline"
                  onClick={() => fileInputRef.current?.click()}
                >
                  Browse Files
                </Button>
              </div>
              {yamlContent && (
                <div className="space-y-2">
                  <Label>Preview</Label>
                  <Textarea
                    className="h-[200px] font-mono text-sm"
                    value={yamlContent}
                    readOnly
                  />
                </div>
              )}
            </div>
          </TabsContent>
        </Tabs>

        {error && (
          <div className="flex items-center gap-2 text-destructive text-sm mt-2">
            <AlertCircle className="h-4 w-4" />
            <span>{error}</span>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleImport} disabled={isLoading || !yamlContent.trim()}>
            {isLoading ? 'Importing...' : 'Import'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
