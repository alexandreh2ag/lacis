import { useReactFlow } from '@xyflow/react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { layout, spacingFor, type Layout, type Spacing } from './layout'
import type { GraphData } from './query'
import type { Zone } from './zones'

// fitOptions keep the graph clear of the toolbar and the legend.
export const fitOptions = { padding: { top: '72px', bottom: '84px', left: '24px', right: '24px' } as const, duration: 200 }

// shapeOf is what the layout of a view depends on: its nodes, their lanes
// and sizes, its edges and boxes. A refresh bringing the same shape keeps
// the cards where they are, moved by hand or not.
export function shapeOf(view: GraphData, zones: Map<string, Zone>): string {
  return JSON.stringify([
    view.level,
    view.nodes.map((n) => [n.id, n.env, n.kind, n.count, n.members.length, zones.get(n.id)?.height]),
    view.edges.map((e) => e.id),
    view.groups.map((g) => [g.id, g.label, g.nodes]),
  ])
}

// useGraphLayout places the view in a Web Worker, again when its shape,
// the envs order or the width of the pane change, or when relayout does.
// A new viewKey fits the viewport to the graph.
export function useGraphLayout(view: GraphData | null, zones: Map<string, Zone>, envOrder: string[], viewKey: string, relayout: number) {
  const flow = useReactFlow()
  const [placed, setPlaced] = useState<Layout | null>(null)
  const [error, setError] = useState<string | null>(null)
  // fitted is the view the graph was last fitted to: opening a zone keeps
  // the viewport, another lens or filter fits it again.
  const fitted = useRef('')
  // spacing follows the width of the graph: a large screen spreads the
  // cards instead of leaving its sides empty. It changes past a few
  // pixels only, each change laying the graph out again.
  const pane = useRef<HTMLDivElement>(null)
  const [spacing, setSpacing] = useState<Spacing>(() => spacingFor(window.innerWidth - 248, window.innerHeight - 56))
  useEffect(() => {
    const el = pane.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      const next = spacingFor(entry.contentRect.width, entry.contentRect.height)
      setSpacing((cur) => (Math.abs(cur.between - next.between) < 16 && cur.aspect === next.aspect ? cur : next))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  // The latest view is read through a ref: the layout runs again on a
  // new shape only, not on each refresh of the data.
  const latest = useRef({ view, zones })
  latest.current = { view, zones }
  const shape = useMemo(() => (view ? shapeOf(view, zones) : ''), [view, zones])
  const envKey = envOrder.join('\n')
  const fitKey = JSON.stringify([viewKey, relayout, spacing])
  useEffect(() => {
    const { view, zones } = latest.current
    if (!view) return
    let live = true
    const sizes = new Map([...zones.values()].map((z) => [z.id, { width: z.width, height: z.height }]))
    layout(view, envKey ? envKey.split('\n') : [], sizes, spacing).then((l) => {
      if (!live) return
      setError(null)
      setPlaced(l)
      if (fitted.current !== fitKey) {
        fitted.current = fitKey
        requestAnimationFrame(() => void flow.fitView(fitOptions))
      }
    }).catch((err: unknown) => {
      if (live) setError(err instanceof Error ? err.message : String(err))
    })
    return () => { live = false }
  }, [shape, envKey, spacing, fitKey, flow])

  return { placed, error, pane }
}
