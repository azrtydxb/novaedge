import { useMemo, useState } from 'react'
import type { Span } from '@/api/types'
import { formatDuration } from '@/lib/utils'

interface TraceTimelineProps {
  spans: Span[]
}

function hashColor(str: string): string {
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
  }
  const hue = Math.abs(hash) % 360
  return `hsl(${hue}, 65%, 55%)`
}

interface SpanNode {
  span: Span
  depth: number
}

function buildSpanTree(spans: Span[]): SpanNode[] {
  if (spans.length === 0) return []

  const spanMap = new Map<string, Span>()
  for (const span of spans) {
    spanMap.set(span.spanID, span)
  }

  const childMap = new Map<string, string[]>()
  const rootIds: string[] = []

  for (const span of spans) {
    const parentRef = span.references?.find(
      (ref) => ref.refType === 'CHILD_OF' || ref.refType === 'childOf'
    )
    if (parentRef && spanMap.has(parentRef.spanID)) {
      const children = childMap.get(parentRef.spanID) ?? []
      children.push(span.spanID)
      childMap.set(parentRef.spanID, children)
    } else {
      rootIds.push(span.spanID)
    }
  }

  const result: SpanNode[] = []
  function visit(spanId: string, depth: number) {
    const span = spanMap.get(spanId)
    if (!span) return
    result.push({ span, depth })
    const children = childMap.get(spanId) ?? []
    children.sort((a, b) => {
      const sa = spanMap.get(a)
      const sb = spanMap.get(b)
      return (sa?.startTime ?? 0) - (sb?.startTime ?? 0)
    })
    for (const childId of children) {
      visit(childId, depth + 1)
    }
  }

  rootIds.sort((a, b) => {
    const sa = spanMap.get(a)
    const sb = spanMap.get(b)
    return (sa?.startTime ?? 0) - (sb?.startTime ?? 0)
  })
  for (const rootId of rootIds) {
    visit(rootId, 0)
  }

  return result
}

export function TraceTimeline({ spans }: TraceTimelineProps) {
  const [hoveredSpan, setHoveredSpan] = useState<string | null>(null)

  const { spanNodes, traceStart, traceDuration, timeMarkers } = useMemo(() => {
    const nodes = buildSpanTree(spans)
    if (nodes.length === 0) {
      return { spanNodes: [], traceStart: 0, traceDuration: 0, timeMarkers: [] }
    }

    const start = Math.min(...spans.map((s) => s.startTime))
    const end = Math.max(...spans.map((s) => s.startTime + s.duration))
    const duration = end - start

    const markerCount = 5
    const markers: number[] = []
    for (let i = 0; i <= markerCount; i++) {
      markers.push((duration / markerCount) * i)
    }

    return {
      spanNodes: nodes,
      traceStart: start,
      traceDuration: duration,
      timeMarkers: markers,
    }
  }, [spans])

  if (spanNodes.length === 0) {
    return (
      <div className="text-muted-foreground text-sm p-4">No spans to display</div>
    )
  }

  const rowHeight = 32
  const labelWidth = 280
  const chartPadding = 16

  return (
    <div className="overflow-x-auto">
      {/* Time scale header */}
      <div className="flex border-b border-border mb-1">
        <div style={{ width: labelWidth, minWidth: labelWidth }} className="text-xs text-muted-foreground px-2 py-1 flex-shrink-0">
          Service / Operation
        </div>
        <div className="flex-1 relative" style={{ minWidth: 400 }}>
          <div className="flex justify-between px-2 py-1">
            {timeMarkers.map((marker, i) => (
              <span key={i} className="text-xs text-muted-foreground">
                {formatDuration(marker / 1000)}
              </span>
            ))}
          </div>
        </div>
      </div>

      {/* Span rows */}
      <div>
        {spanNodes.map(({ span, depth }) => {
          const offset = traceDuration > 0
            ? ((span.startTime - traceStart) / traceDuration) * 100
            : 0
          const width = traceDuration > 0
            ? Math.max((span.duration / traceDuration) * 100, 0.5)
            : 100
          const color = hashColor(span.serviceName)
          const isHovered = hoveredSpan === span.spanID

          return (
            <div
              key={span.spanID}
              className={`flex items-center border-b border-border/50 transition-colors ${
                isHovered ? 'bg-muted/50' : 'hover:bg-muted/30'
              }`}
              style={{ height: rowHeight }}
              onMouseEnter={() => setHoveredSpan(span.spanID)}
              onMouseLeave={() => setHoveredSpan(null)}
            >
              {/* Label */}
              <div
                style={{ width: labelWidth, minWidth: labelWidth, paddingLeft: 8 + depth * 16 }}
                className="flex-shrink-0 truncate text-xs pr-2"
                title={`${span.serviceName}: ${span.operationName}`}
              >
                <span style={{ color }} className="font-medium">
                  {span.serviceName}
                </span>
                <span className="text-muted-foreground ml-1">
                  {span.operationName}
                </span>
              </div>

              {/* Bar area */}
              <div className="flex-1 relative" style={{ minWidth: 400, paddingRight: chartPadding }}>
                {/* Grid lines */}
                {timeMarkers.map((_, i) => (
                  <div
                    key={i}
                    className="absolute top-0 bottom-0 border-l border-border/30"
                    style={{ left: `${(i / (timeMarkers.length - 1)) * 100}%` }}
                  />
                ))}

                {/* Span bar */}
                <div
                  className="absolute rounded-sm transition-opacity"
                  style={{
                    left: `${offset}%`,
                    width: `${width}%`,
                    top: 6,
                    height: rowHeight - 12,
                    backgroundColor: color,
                    opacity: isHovered ? 1 : 0.85,
                    minWidth: 2,
                  }}
                />

                {/* Duration label on bar */}
                {width > 5 && (
                  <div
                    className="absolute text-[10px] text-white font-medium pointer-events-none truncate"
                    style={{
                      left: `${offset}%`,
                      width: `${width}%`,
                      top: 8,
                      height: rowHeight - 16,
                      lineHeight: `${rowHeight - 16}px`,
                      paddingLeft: 4,
                    }}
                  >
                    {formatDuration(span.duration / 1000)}
                  </div>
                )}

                {/* Tooltip on hover */}
                {isHovered && (
                  <div
                    className="absolute z-50 bg-popover border border-border rounded-md shadow-lg p-3 text-xs pointer-events-none"
                    style={{
                      left: `${Math.min(offset + width / 2, 70)}%`,
                      top: rowHeight,
                      minWidth: 220,
                    }}
                  >
                    <div className="font-medium mb-1">{span.operationName}</div>
                    <div className="text-muted-foreground space-y-0.5">
                      <div>Service: <span className="text-foreground">{span.serviceName}</span></div>
                      <div>Duration: <span className="text-foreground">{formatDuration(span.duration / 1000)}</span></div>
                      <div>Span ID: <span className="text-foreground font-mono">{span.spanID.slice(0, 16)}</span></div>
                      {span.tags && Object.keys(span.tags).length > 0 && (
                        <div className="mt-1 pt-1 border-t border-border/50">
                          {Object.entries(span.tags).slice(0, 5).map(([k, v]) => (
                            <div key={k}>
                              {k}: <span className="text-foreground">{v}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
