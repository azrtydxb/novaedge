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
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Label } from '@/components/ui/label'
import { AlertCircle } from 'lucide-react'
import yaml from 'js-yaml'

interface ResourceDialogProps<T> {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  mode: 'create' | 'edit' | 'view'
  resource?: T
  onSubmit: (resource: T) => Promise<void>
  isLoading?: boolean
  readOnly?: boolean
  defaultYaml?: string
  formContent?: React.ReactNode
}

export function ResourceDialog<T extends object>({
  open,
  onOpenChange,
  title,
  description,
  mode,
  resource,
  onSubmit,
  isLoading = false,
  readOnly = false,
  defaultYaml = '',
  formContent,
}: ResourceDialogProps<T>) {
  const [yamlContent, setYamlContent] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'form' | 'yaml'>('yaml')

  useEffect(() => {
    if (open) {
      if (resource) {
        try {
          setYamlContent(yaml.dump(resource, { indent: 2 }))
        } catch {
          setYamlContent('')
        }
      } else if (defaultYaml) {
        setYamlContent(defaultYaml)
      } else {
        setYamlContent('')
      }
      setError(null)
    }
  }, [open, resource, defaultYaml])

  const handleSubmit = async () => {
    setError(null)
    try {
      const parsed = yaml.load(yamlContent) as T
      if (!parsed || typeof parsed !== 'object') {
        setError('Invalid YAML: must be an object')
        return
      }
      await onSubmit(parsed)
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to parse YAML')
    }
  }

  const isViewMode = mode === 'view' || readOnly

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'form' | 'yaml')} className="flex-1 overflow-hidden flex flex-col">
          {formContent && (
            <TabsList className="w-fit">
              <TabsTrigger value="form">Form</TabsTrigger>
              <TabsTrigger value="yaml">YAML</TabsTrigger>
            </TabsList>
          )}

          {formContent && (
            <TabsContent value="form" className="flex-1 overflow-auto mt-4">
              {formContent}
            </TabsContent>
          )}

          <TabsContent value="yaml" className={`flex-1 overflow-hidden flex flex-col ${!formContent ? 'mt-0' : ''}`}>
            <div className="flex-1 overflow-hidden">
              <Label htmlFor="yaml-editor" className="sr-only">
                YAML Configuration
              </Label>
              <Textarea
                id="yaml-editor"
                className="h-full min-h-[400px] font-mono text-sm resize-none"
                value={yamlContent}
                onChange={(e) => setYamlContent(e.target.value)}
                readOnly={isViewMode}
                placeholder="Enter YAML configuration..."
              />
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
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {isViewMode ? 'Close' : 'Cancel'}
          </Button>
          {!isViewMode && (
            <Button onClick={handleSubmit} disabled={isLoading}>
              {isLoading ? 'Saving...' : mode === 'create' ? 'Create' : 'Update'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
