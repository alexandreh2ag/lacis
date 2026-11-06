import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'
import { useEdgeState } from './highlight'
import type { GEdge } from './query'

export interface LinkData extends Record<string, unknown> {
  edge: GEdge
  color: number
  fromLabel: string
  toLabel: string
  // impact draws an edge of the impact chain.
  impact?: boolean
}

// LacisEdge is a link: the color of its source, the dash of its
// confidence, its weight when it merges several links, and a tooltip when
// hovered, which tells a link between two envs.
export const LacisEdge = memo(function LacisEdge(props: EdgeProps) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data } = props
  const { edge: e, color, fromLabel, toLabel, impact } = data as LinkData
  const state = useEdgeState(id)
  const hovered = state === 'hovered'
  const related = hovered || state === 'related'
  // lit keeps the edge bright while the root of the graph fades the others.
  const lit = related ? ' lc-edge--lit' : ''
  const t = useT()
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition })
  const width = 1.5 + Math.min(e.count - 1, 3) * 0.75 + (related ? 1.5 : 0)
  const dash = e.confidence === 'INFERRED' ? '6 4' : e.confidence === 'AMBIGUOUS' ? '1 4' : undefined
  const stroke = impact ? 'var(--danger)' : `var(--link-${color})`
  return (
    <>
      <BaseEdge id={id} className={'lc-edge' + lit} path={path} markerEnd={`url(#lc-arrow-${impact ? 'danger' : color})`} interactionWidth={14}
        style={{ stroke, strokeWidth: width, strokeDasharray: dash, strokeLinecap: 'round' }} />
      <EdgeLabelRenderer>
        {e.count > 1 ? (
          <div className={'lc-edge__weight nodrag nopan' + lit} style={{ position: 'absolute', transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, borderColor: stroke }}>
            {e.count}
          </div>
        ) : null}
        {hovered ? (
          <div className="lc-tooltip" role="tooltip" style={{ position: 'absolute', width: 260, zIndex: 10, pointerEvents: 'none',
            transform: `translate(-50%, calc(-100% - 14px)) translate(${labelX}px, ${labelY}px)` }}>
            <b><span className="mr-1.5 inline-block size-2 rounded-[2px]" style={{ background: stroke }} />{fromLabel} → {toLabel}</b>
            {e.labels.length ? <span className="lc-mono">{e.labels.slice(0, 3).join(' · ')}{e.labels.length > 3 ? ` +${e.labels.length - 3}` : ''}</span> : null}
            <span>{t.graph.edge.links(e.count)} · {confidenceText(e.confidence)}</span>
            {e.crossEnv ? <span>{t.graph.edge.crossEnv}</span> : null}
          </div>
        ) : null}
      </EdgeLabelRenderer>
    </>
  )
})

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
