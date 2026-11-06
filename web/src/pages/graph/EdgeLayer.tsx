import { getBezierPath, Position, useStore, useStoreApi, ViewportPortal, type Edge, type InternalNode } from '@xyflow/react'
import { memo, useContext, useMemo, useSyncExternalStore } from 'react'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'
import { HighlightContext, useEdgeState } from './highlight'
import { NODE_H, NODE_W, type Point } from './layout'
import type { GEdge } from './query'

export interface LinkData extends Record<string, unknown> {
  edge: GEdge
  color: number
  fromLabel: string
  toLabel: string
  // impact draws an edge of the impact chain.
  impact?: boolean
  // route is the way ELK drew the edge, round the cards; it holds while
  // its ends stay at from and to, where the layout put them.
  route?: Point[]
  from?: Point
  to?: Point
}

export type LinkEdge = Edge<LinkData>

// Drawn is an edge placed between its two cards.
interface Drawn {
  id: string
  data: LinkData
  path: string
  labelX: number
  labelY: number
}

const size = (n: InternalNode) => ({ width: n.measured.width ?? n.width ?? NODE_W, height: n.measured.height ?? n.height ?? NODE_H })

// EdgeLayer draws every edge of the graph in one SVG under the cards, in
// place of React Flow, which gives each edge an SVG of its own: Firefox
// draws each of them as an image as large as the area its curve spans,
// hundreds of them, and uploads them again on every change. Its edges go
// from the right side of a card to the left side of another, the color of
// their source, the dash of their confidence, their weight when they merge
// several links, and a tooltip when hovered.
// With crossHidden, an edge between two envs is drawn only while lit: the
// cards count them.
export const EdgeLayer = memo(function EdgeLayer({ edges, crossHidden }: { edges: LinkEdge[]; crossHidden: boolean }) {
  // The nodes change with every move of a card and every measure: the
  // edges follow them, not the pans and zooms, which the viewport does.
  const nodes = useStore((s) => s.nodes)
  const store = useStoreApi()
  const drawn = useMemo(() => {
    const lookup = store.getState().nodeLookup
    const out: Drawn[] = []
    for (const e of edges) {
      const a = lookup.get(e.source)
      const b = lookup.get(e.target)
      if (!a || !b || a.hidden || b.hidden || !e.data) continue
      const pa = a.internals.positionAbsolute
      const pb = b.internals.positionAbsolute
      const d = e.data
      if (d.route && d.from && d.to && near(pa, d.from) && near(pb, d.to)) {
        out.push({ id: e.id, data: d, ...routed(d.route) })
        continue
      }
      const sa = size(a)
      const sb = size(b)
      const [path, labelX, labelY] = getBezierPath({
        sourceX: a.internals.positionAbsolute.x + sa.width, sourceY: a.internals.positionAbsolute.y + sa.height / 2, sourcePosition: Position.Right,
        targetX: b.internals.positionAbsolute.x, targetY: b.internals.positionAbsolute.y + sb.height / 2, targetPosition: Position.Left,
      })
      out.push({ id: e.id, data: e.data, path, labelX, labelY })
    }
    return out
  // nodes stands for the positions read in the lookup.
  }, [edges, nodes, store])
  return (
    <ViewportPortal>
      <div className="lc-edges-layer">
        <svg className="lc-edges" aria-hidden="true">
          {drawn.map((d) => <EdgePath key={d.id} id={d.id} path={d.path} data={d.data} hidden={crossHidden && !!d.data.edge.crossEnv} />)}
        </svg>
        {drawn.map((d) => (d.data.edge.count > 1
          ? <EdgeWeight key={d.id} id={d.id} x={d.labelX} y={d.labelY} data={d.data} hidden={crossHidden && !!d.data.edge.crossEnv} /> : null))}
        <EdgeTooltip drawn={drawn} />
      </div>
    </ViewportPortal>
  )
})

const near = (p: Point, q: Point) => Math.abs(p.x - q.x) < 0.5 && Math.abs(p.y - q.y) < 0.5

// routed draws the route of ELK: its spline as cubic segments, else a
// smooth curve through its points; the label sits at its middle point.
export function routed(pts: Point[]): { path: string; labelX: number; labelY: number } {
  const mid = pts[Math.floor(pts.length / 2)]
  let path = `M${pts[0].x},${pts[0].y}`
  if ((pts.length - 1) % 3 === 0) {
    for (let i = 1; i < pts.length; i += 3) path += ` C${pts[i].x},${pts[i].y} ${pts[i + 1].x},${pts[i + 1].y} ${pts[i + 2].x},${pts[i + 2].y}`
  } else {
    for (let i = 0; i < pts.length - 1; i++) {
      const p0 = pts[i - 1] ?? pts[i], p1 = pts[i], p2 = pts[i + 1], p3 = pts[i + 2] ?? p2
      path += ` C${p1.x + (p2.x - p0.x) / 6},${p1.y + (p2.y - p0.y) / 6} ${p2.x - (p3.x - p1.x) / 6},${p2.y - (p3.y - p1.y) / 6} ${p2.x},${p2.y}`
    }
  }
  return { path, labelX: mid.x, labelY: mid.y }
}

const strokeOf = (d: LinkData) => (d.impact ? 'var(--danger)' : `var(--link-${d.color})`)

// EdgePath is the curve of an edge and its wider invisible twin, which the
// pointer hovers; a hidden edge shows only while lit.
const EdgePath = memo(function EdgePath({ id, path, data, hidden }: { id: string; path: string; data: LinkData; hidden: boolean }) {
  const h = useContext(HighlightContext)
  const state = useEdgeState(id)
  const related = state === 'hovered' || state === 'related'
  if (hidden && !related) return null
  const e = data.edge
  const width = 1.5 + Math.min(e.count - 1, 3) * 0.75 + (related ? 1.5 : 0)
  const dash = e.confidence === 'INFERRED' ? '6 4' : e.confidence === 'AMBIGUOUS' ? '1 4' : undefined
  return (
    <>
      <path className={'lc-edge' + (related ? ' lc-edge--lit' : '')} d={path} fill="none" stroke={strokeOf(data)} strokeWidth={width}
        strokeDasharray={dash} strokeLinecap="round" markerEnd={`url(#lc-arrow-${data.impact ? 'danger' : data.color})`} />
      <path className="lc-edge-hit" d={path} fill="none" stroke="transparent" strokeWidth={14}
        onMouseEnter={() => h.hoverEdgeOn(e)} onMouseLeave={() => h.hoverEdgeOn(null)} />
    </>
  )
})

// EdgeWeight is the number of links an edge merges.
const EdgeWeight = memo(function EdgeWeight({ id, x, y, data, hidden }: { id: string; x: number; y: number; data: LinkData; hidden: boolean }) {
  const state = useEdgeState(id)
  if (hidden && state !== 'hovered' && state !== 'related') return null
  const lit = state === 'hovered' || state === 'related' ? ' lc-edge--lit' : ''
  return (
    <div className={'lc-edge__weight nodrag nopan' + lit}
      style={{ position: 'absolute', transform: `translate(-50%, -50%) translate(${x}px, ${y}px)`, borderColor: strokeOf(data) }}>
      {data.edge.count}
    </div>
  )
})

// EdgeTooltip tells the edge hovered: its ends, its links and their
// confidence.
function EdgeTooltip({ drawn }: { drawn: Drawn[] }) {
  const h = useContext(HighlightContext)
  const hovered = useSyncExternalStore(h.subscribe, () => h.hoveredEdge())
  const t = useT()
  const d = hovered ? drawn.find((x) => x.id === hovered) : undefined
  if (!d) return null
  const e = d.data.edge
  return (
    <div className="lc-tooltip" role="tooltip" style={{ position: 'absolute', width: 260, zIndex: 10, pointerEvents: 'none',
      transform: `translate(-50%, calc(-100% - 14px)) translate(${d.labelX}px, ${d.labelY}px)` }}>
      <b><span className="mr-1.5 inline-block size-2 rounded-[2px]" style={{ background: strokeOf(d.data) }} />{d.data.fromLabel} → {d.data.toLabel}</b>
      {e.labels.length ? <span className="lc-mono">{e.labels.slice(0, 3).join(' · ')}{e.labels.length > 3 ? ` +${e.labels.length - 3}` : ''}</span> : null}
      <span>{t.graph.edge.links(e.count)} · {confidenceText(e.confidence)}</span>
      {e.crossEnv ? <span>{t.graph.edge.crossEnv}</span> : null}
    </div>
  )
}

// Markers are the arrow heads, one per color.
export function Markers() {
  return (
    <svg width="0" height="0" aria-hidden="true" style={{ position: 'absolute' }}>
      <defs>
        {Array.from({ length: 8 }, (_, i) => i + 1).map((c) => (
          <marker key={c} id={`lc-arrow-${c}`} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse">
            <path d="M0 0 L10 5 L0 10 z" fill={`var(--link-${c})`} />
          </marker>
        ))}
        <marker id="lc-arrow-danger" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse">
          <path d="M0 0 L10 5 L0 10 z" fill="var(--danger)" />
        </marker>
      </defs>
    </svg>
  )
}
