import { useState, useMemo, useRef, useEffect, useCallback } from 'react'
import { useAgents, useLogs } from '@/api/hooks'
import { Card, CardContent } from '@/components/ui/card'
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
import { ScrollText, RefreshCw, ArrowDownToLine, AlertCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'UNKNOWN'

const LEVEL_COLORS: Record<LogLevel, string> = {
  DEBUG: 'text-slate-400',
  INFO: 'text-blue-400',
  WARN: 'text-yellow-400',
  ERROR: 'text-red-400',
  UNKNOWN: 'text-slate-300',
}

const LEVEL_OPTIONS: LogLevel[] = ['DEBUG', 'INFO', 'WARN', 'ERROR']

const TAIL_OPTIONS = [
  { label: '100 lines', value: '100' },
  { label: '500 lines', value: '500' },
  { label: '1000 lines', value: '1000' },
]

function detectLevel(line: string): LogLevel {
  const lower = line.toLowerCase()
  if (/"level"\s*:\s*"debug"/i.test(line) || /level=debug/i.test(lower) || /\bDEBUG\b/.test(line)) {
    return 'DEBUG'
  }
  if (/"level"\s*:\s*"info"/i.test(line) || /level=info/i.test(lower) || /\bINFO\b/.test(line)) {
    return 'INFO'
  }
  if (/"level"\s*:\s*"warn"/i.test(line) || /level=warn/i.test(lower) || /\bWARN\b/.test(line)) {
    return 'WARN'
  }
  if (/"level"\s*:\s*"error"/i.test(line) || /level=error/i.test(lower) || /\bERROR\b/.test(line)) {
    return 'ERROR'
  }
  return 'UNKNOWN'
}

function extractTimestamp(line: string): string | null {
  const isoMatch = line.match(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/)
  if (isoMatch) return isoMatch[0].replace('T', ' ')

  const tsMatch = line.match(/"ts"\s*:\s*"([^"]+)"/)
  if (tsMatch) return tsMatch[1]

  const timeMatch = line.match(/"time"\s*:\s*"([^"]+)"/)
  if (timeMatch) return timeMatch[1]

  return null
}

interface ParsedLine {
  raw: string
  level: LogLevel
  timestamp: string | null
}

export default function Logs() {
  const [namespace, setNamespace] = useState('novaedge-system')
  const [selectedPod, setSelectedPod] = useState('')
  const [tailLines, setTailLines] = useState(100)
  const [levelFilter, setLevelFilter] = useState<Set<LogLevel>>(new Set(LEVEL_OPTIONS))
  const [keyword, setKeyword] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const logContainerRef = useRef<HTMLDivElement>(null)

  const { data: agents = [] } = useAgents(namespace)
  const {
    data: logData,
    isLoading,
    error,
    refetch,
  } = useLogs(selectedPod, namespace, tailLines)

  const podOptions = useMemo(() => {
    return agents.map((agent) => ({
      label: agent.podName ?? agent.name ?? 'unknown',
      value: agent.podName ?? agent.name ?? '',
    }))
  }, [agents])

  // Auto-select first pod
  useEffect(() => {
    if (!selectedPod && podOptions.length > 0) {
      setSelectedPod(podOptions[0].value)
    }
  }, [podOptions, selectedPod])

  const parsedLines: ParsedLine[] = useMemo(() => {
    if (!logData) return []
    return logData.split('\n').filter(Boolean).map((line) => ({
      raw: line,
      level: detectLevel(line),
      timestamp: extractTimestamp(line),
    }))
  }, [logData])

  const filteredLines = useMemo(() => {
    return parsedLines.filter((line) => {
      if (!levelFilter.has(line.level) && line.level !== 'UNKNOWN') return false
      if (keyword && !line.raw.toLowerCase().includes(keyword.toLowerCase())) return false
      return true
    })
  }, [parsedLines, levelFilter, keyword])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [filteredLines, autoScroll])

  const toggleLevel = useCallback((level: LogLevel) => {
    setLevelFilter((prev) => {
      const next = new Set(prev)
      if (next.has(level)) {
        next.delete(level)
      } else {
        next.add(level)
      }
      return next
    })
  }, [])

  const highlightKeyword = useCallback((text: string, kw: string): React.ReactNode => {
    if (!kw) return text
    const parts = text.split(new RegExp(`(${kw.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi'))
    return parts.map((part, i) =>
      part.toLowerCase() === kw.toLowerCase() ? (
        <mark key={i} className="bg-yellow-500/30 text-yellow-200 rounded px-0.5">{part}</mark>
      ) : (
        part
      )
    )
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <ScrollText className="h-6 w-6" />
        <h1 className="text-2xl font-bold">Logs</h1>
      </div>

      {/* Controls */}
      <Card>
        <CardContent className="pt-6">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-6 items-end">
            <div className="space-y-2">
              <Label>Namespace</Label>
              <Input
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                placeholder="novaedge-system"
              />
            </div>

            <div className="space-y-2">
              <Label>Pod</Label>
              <Select value={selectedPod} onValueChange={setSelectedPod}>
                <SelectTrigger>
                  <SelectValue placeholder="Select a pod" />
                </SelectTrigger>
                <SelectContent>
                  {podOptions.map((pod) => (
                    <SelectItem key={pod.value} value={pod.value}>{pod.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>Tail Lines</Label>
              <Select value={String(tailLines)} onValueChange={(v) => setTailLines(Number(v))}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TAIL_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>Search</Label>
              <Input
                placeholder="Filter by keyword..."
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
              />
            </div>

            <div className="flex gap-2">
              <Button onClick={() => refetch()} variant="outline" size="icon" title="Refresh">
                <RefreshCw className={cn('h-4 w-4', isLoading && 'animate-spin')} />
              </Button>
              <Button
                onClick={() => setAutoScroll(!autoScroll)}
                variant={autoScroll ? 'default' : 'outline'}
                size="icon"
                title={autoScroll ? 'Auto-scroll ON' : 'Auto-scroll OFF'}
              >
                <ArrowDownToLine className="h-4 w-4" />
              </Button>
            </div>

            <div className="space-y-2">
              <Label>Level Filter</Label>
              <div className="flex gap-1 flex-wrap">
                {LEVEL_OPTIONS.map((level) => (
                  <Button
                    key={level}
                    variant={levelFilter.has(level) ? 'default' : 'outline'}
                    size="sm"
                    className={cn(
                      'text-xs px-2 h-7',
                      levelFilter.has(level) && level === 'DEBUG' && 'bg-slate-600 hover:bg-slate-700',
                      levelFilter.has(level) && level === 'INFO' && 'bg-blue-600 hover:bg-blue-700',
                      levelFilter.has(level) && level === 'WARN' && 'bg-yellow-600 hover:bg-yellow-700',
                      levelFilter.has(level) && level === 'ERROR' && 'bg-red-600 hover:bg-red-700',
                    )}
                    onClick={() => toggleLevel(level)}
                  >
                    {level}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Log output */}
      {error ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Failed to Load Logs</p>
            <p className="text-sm mt-1">Could not fetch logs for the selected pod</p>
          </CardContent>
        </Card>
      ) : !selectedPod ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
            <ScrollText className="h-12 w-12 mb-4" />
            <p className="text-lg font-medium">Select a Pod</p>
            <p className="text-sm mt-1">Choose a pod from the dropdown to view its logs</p>
          </CardContent>
        </Card>
      ) : (
        <div className="rounded-lg border border-border overflow-hidden">
          <div
            ref={logContainerRef}
            className="bg-[#0d1117] p-4 font-mono text-xs leading-5 overflow-auto"
            style={{ height: 'calc(100vh - 380px)', minHeight: 400 }}
          >
            {isLoading ? (
              <div className="flex items-center justify-center h-full text-muted-foreground">
                <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary mr-3" />
                Loading logs...
              </div>
            ) : filteredLines.length === 0 ? (
              <div className="flex items-center justify-center h-full text-muted-foreground">
                No log lines match the current filters
              </div>
            ) : (
              filteredLines.map((line, i) => (
                <div key={i} className="hover:bg-white/5 px-1 -mx-1 rounded">
                  {line.timestamp && (
                    <span className="text-slate-500 mr-2 select-none">{line.timestamp}</span>
                  )}
                  <span className={cn(LEVEL_COLORS[line.level])}>
                    {highlightKeyword(line.raw, keyword)}
                  </span>
                </div>
              ))
            )}
          </div>

          {/* Status bar */}
          <div className="bg-[#161b22] border-t border-border px-4 py-2 flex items-center justify-between text-xs text-muted-foreground">
            <span>
              Showing {filteredLines.length} of {parsedLines.length} lines
              {selectedPod && <> from pod <Badge variant="outline" className="ml-1 text-xs">{selectedPod}</Badge></>}
            </span>
            {autoScroll && (
              <Badge variant="secondary" className="text-xs">Auto-scroll</Badge>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
