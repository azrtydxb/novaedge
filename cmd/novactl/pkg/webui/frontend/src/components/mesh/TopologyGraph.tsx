import { useEffect, useRef, useCallback } from 'react'
import * as d3 from 'd3'
import type { MeshNode, MeshEdge } from '@/api/types'

interface TopologyGraphProps {
  nodes: MeshNode[]
  edges: MeshEdge[]
}

interface SimNode extends d3.SimulationNodeDatum {
  id: string
  name: string
  namespace: string
  spiffeId?: string
}

interface SimLink extends d3.SimulationLinkDatum<SimNode> {
  mtls: boolean
  traffic?: number
}

export function TopologyGraph({ nodes, edges }: TopologyGraphProps) {
  const svgRef = useRef<SVGSVGElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const renderGraph = useCallback(() => {
    if (!svgRef.current || !containerRef.current) return
    if (nodes.length === 0) return

    const svg = d3.select(svgRef.current)
    svg.selectAll('*').remove()

    const rect = containerRef.current.getBoundingClientRect()
    const width = rect.width || 800
    const height = 400

    svg.attr('width', width).attr('height', height)

    // Build simulation data
    const simNodes: SimNode[] = nodes.map((n) => ({
      id: n.id,
      name: n.name,
      namespace: n.namespace,
      spiffeId: n.spiffeId,
    }))

    const nodeMap = new Map(simNodes.map((n) => [n.id, n]))

    const simLinks: SimLink[] = edges
      .filter((e) => nodeMap.has(e.source) && nodeMap.has(e.target))
      .map((e) => ({
        source: e.source,
        target: e.target,
        mtls: e.mtls,
        traffic: e.traffic,
      }))

    // Tooltip
    const tooltip = d3
      .select(containerRef.current)
      .selectAll<HTMLDivElement, unknown>('.topology-tooltip')
      .data([null])
      .join('div')
      .attr('class', 'topology-tooltip')
      .style('position', 'absolute')
      .style('pointer-events', 'none')
      .style('background', 'hsl(222.2 84% 4.9%)')
      .style('border', '1px solid hsl(217.2 32.6% 17.5%)')
      .style('border-radius', '6px')
      .style('padding', '8px 12px')
      .style('font-size', '12px')
      .style('color', '#e2e8f0')
      .style('opacity', 0)
      .style('z-index', 50)

    // Defs for arrow markers
    const defs = svg.append('defs')
    defs
      .append('marker')
      .attr('id', 'arrow-mtls')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 25)
      .attr('refY', 0)
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', '#22c55e')

    defs
      .append('marker')
      .attr('id', 'arrow-no-mtls')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 25)
      .attr('refY', 0)
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', '#ef4444')

    // Force simulation
    const simulation = d3
      .forceSimulation(simNodes)
      .force(
        'link',
        d3
          .forceLink<SimNode, SimLink>(simLinks)
          .id((d) => d.id)
          .distance(120)
      )
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(40))

    // Draw edges
    const link = svg
      .append('g')
      .selectAll<SVGLineElement, SimLink>('line')
      .data(simLinks)
      .join('line')
      .attr('stroke', (d) => (d.mtls ? '#22c55e' : '#ef4444'))
      .attr('stroke-width', (d) => (d.traffic && d.traffic > 0 ? 2.5 : 1.5))
      .attr('stroke-opacity', 0.7)
      .attr('marker-end', (d) => (d.mtls ? 'url(#arrow-mtls)' : 'url(#arrow-no-mtls)'))
      .attr('stroke-dasharray', (d) => (d.traffic && d.traffic > 0 ? '8,4' : 'none'))
      .on('mouseover', function (_event: MouseEvent, d: SimLink) {
        const sourceNode = d.source as SimNode
        const targetNode = d.target as SimNode
        tooltip
          .html(
            `<strong>${sourceNode.name} → ${targetNode.name}</strong><br/>` +
              `mTLS: ${d.mtls ? 'Enabled' : 'Disabled'}<br/>` +
              `Traffic: ${d.traffic !== undefined ? `${d.traffic} req/s` : 'N/A'}`
          )
          .style('opacity', 1)
      })
      .on('mousemove', function (event: MouseEvent) {
        const containerRect = containerRef.current?.getBoundingClientRect()
        if (containerRect) {
          tooltip
            .style('left', `${event.clientX - containerRect.left + 12}px`)
            .style('top', `${event.clientY - containerRect.top - 10}px`)
        }
      })
      .on('mouseout', function () {
        tooltip.style('opacity', 0)
      })

    // Animate dashed edges
    function animateEdges() {
      link
        .filter((d) => (d.traffic ?? 0) > 0)
        .attr('stroke-dashoffset', function () {
          const current = parseFloat(d3.select(this).attr('stroke-dashoffset') || '0')
          return String(current - 1)
        })
      requestAnimationFrame(animateEdges)
    }
    animateEdges()

    // Draw nodes
    const node = svg
      .append('g')
      .selectAll<SVGGElement, SimNode>('g')
      .data(simNodes)
      .join('g')
      .call(
        d3
          .drag<SVGGElement, SimNode>()
          .on('start', (_event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) => {
            if (!_event.active) simulation.alphaTarget(0.3).restart()
            d.fx = d.x
            d.fy = d.y
          })
          .on('drag', (_event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) => {
            d.fx = _event.x
            d.fy = _event.y
          })
          .on('end', (_event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) => {
            if (!_event.active) simulation.alphaTarget(0)
            d.fx = null
            d.fy = null
          })
      )

    node
      .append('circle')
      .attr('r', 18)
      .attr('fill', '#3b82f6')
      .attr('stroke', '#1e40af')
      .attr('stroke-width', 2)
      .attr('cursor', 'grab')

    node
      .append('text')
      .text((d) => d.name)
      .attr('text-anchor', 'middle')
      .attr('dy', 32)
      .attr('fill', '#e2e8f0')
      .attr('font-size', '11px')
      .attr('pointer-events', 'none')

    node
      .on('mouseover', function (_event: MouseEvent, d: SimNode) {
        d3.select(this).select('circle').attr('stroke-width', 3).attr('stroke', '#60a5fa')
        tooltip
          .html(
            `<strong>${d.name}</strong><br/>` +
              `Namespace: ${d.namespace}<br/>` +
              (d.spiffeId ? `SPIFFE ID: ${d.spiffeId}` : '')
          )
          .style('opacity', 1)
      })
      .on('mousemove', function (event: MouseEvent) {
        const containerRect = containerRef.current?.getBoundingClientRect()
        if (containerRect) {
          tooltip
            .style('left', `${event.clientX - containerRect.left + 12}px`)
            .style('top', `${event.clientY - containerRect.top - 10}px`)
        }
      })
      .on('mouseout', function () {
        d3.select(this).select('circle').attr('stroke-width', 2).attr('stroke', '#1e40af')
        tooltip.style('opacity', 0)
      })

    // Tick handler
    simulation.on('tick', () => {
      link
        .attr('x1', (d) => (d.source as SimNode).x ?? 0)
        .attr('y1', (d) => (d.source as SimNode).y ?? 0)
        .attr('x2', (d) => (d.target as SimNode).x ?? 0)
        .attr('y2', (d) => (d.target as SimNode).y ?? 0)

      node.attr('transform', (d) => `translate(${d.x ?? 0},${d.y ?? 0})`)
    })

    return () => {
      simulation.stop()
    }
  }, [nodes, edges])

  useEffect(() => {
    renderGraph()
  }, [renderGraph])

  // Handle resize
  useEffect(() => {
    const observer = new ResizeObserver(() => {
      renderGraph()
    })
    if (containerRef.current) {
      observer.observe(containerRef.current)
    }
    return () => observer.disconnect()
  }, [renderGraph])

  if (nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-[400px] text-muted-foreground">
        No mesh services found
      </div>
    )
  }

  return (
    <div ref={containerRef} className="relative w-full" style={{ minHeight: 400 }}>
      <svg ref={svgRef} className="w-full" style={{ height: 400 }} />
    </div>
  )
}
