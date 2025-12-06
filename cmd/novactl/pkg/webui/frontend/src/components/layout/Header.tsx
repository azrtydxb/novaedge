import { RefreshCw, Plus, FileCode } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useApp } from '@/contexts/AppContext'

interface HeaderProps {
  title: string
  showCreateButton?: boolean
  showTemplateButton?: boolean
  onRefresh?: () => void
  onCreate?: () => void
  onShowTemplates?: () => void
  isLoading?: boolean
}

export function Header({
  title,
  showCreateButton = false,
  showTemplateButton = false,
  onRefresh,
  onCreate,
  onShowTemplates,
  isLoading = false,
}: HeaderProps) {
  const { namespace, setNamespace, namespaces, readOnly } = useApp()

  return (
    <header className="flex items-center justify-between border-b bg-white px-6 py-4">
      <h1 className="text-2xl font-semibold text-slate-900">{title}</h1>

      <div className="flex items-center gap-4">
        {/* Namespace Selector */}
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Namespace:</span>
          <Select value={namespace} onValueChange={setNamespace}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="Select namespace" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Namespaces</SelectItem>
              {namespaces.map((ns) => (
                <SelectItem key={ns} value={ns}>
                  {ns}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Template Button */}
        {showTemplateButton && !readOnly && (
          <Button variant="outline" onClick={onShowTemplates}>
            <FileCode className="mr-2 h-4 w-4" />
            Templates
          </Button>
        )}

        {/* Create Button */}
        {showCreateButton && !readOnly && (
          <Button onClick={onCreate}>
            <Plus className="mr-2 h-4 w-4" />
            Create
          </Button>
        )}

        {/* Refresh Button */}
        {onRefresh && (
          <Button
            variant="outline"
            size="icon"
            onClick={onRefresh}
            disabled={isLoading}
          >
            <RefreshCw
              className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`}
            />
          </Button>
        )}
      </div>
    </header>
  )
}
