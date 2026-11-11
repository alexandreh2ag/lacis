import { Handle, NodeResizeControl, Position, ResizeControlVariant, type NodeProps } from '@xyflow/react'
import { memo, type HTMLAttributes, type ReactNode } from 'react'
import { CopyButton } from '@/components/Copy'
import { Glyph, TechLogo } from '@/components/Icon'
import { useT } from '@/i18n'
import { useNodeState } from './highlight'
import { canOpen, countOf, foldedTechs, info, kindOf, mainTech, stacked, title } from './kinds'
import type { GNode } from './query'

// CardData is what a card shows; whether it is lit by a hover or an
// impact comes from the highlight of the graph.
export interface CardData extends Record<string, unknown> {
  node: GNode
  selected: boolean
  // impact is the distance of the node to the target of the impact.
  impact?: number
  // copy is the hostname a card of the view of the hosts offers to copy.
  copy?: string
  // cross counts the links of the node with each other env, drawn only
  // when the card is hovered.
  cross?: CrossEnv[]
}

// CrossEnv is the number of links of a node with another env, whose lane
// is above or below.
export interface CrossEnv {
  env: string
  count: number
  up: boolean
}

// NodeCard is a node of the graph as the design system draws it: always
// 216 × 56, the name cut, the logos of what it folds.
export const NodeCard = memo(function NodeCard({ id, data }: NodeProps) {
  return (
    <CardFace id={id} data={data as CardData}>
      <Handle type="target" position={Position.Left} className="lc-handle" isConnectable={false} />
      <Handle type="source" position={Position.Right} className="lc-handle" isConnectable={false} />
    </CardFace>
  )
})

// CardFace is the look of a card, drawn by a node of the graph or, with
// no handles, inside the row of an opened zone.
export function CardFace({ id, data, children, ...rest }: { id: string; data: CardData; children?: ReactNode } & HTMLAttributes<HTMLDivElement>) {
  const { node: n, selected, impact, copy, cross } = data
  const state = useNodeState(id)
  const t = useT()
  const kind = kindOf(n)
  const count = countOf(n)
  const cls = ['lc-node', `lc-node--${kind}`,
    stacked(n) ? 'lc-node--stack' : '',
    n.divergences.length ? 'lc-node--diverges' : '',
    canOpen(n) ? 'lc-node--opens' : '',
    n.status === 'STALE' ? 'lc-node--stale' : '',
    n.status === 'INACTIVE' ? 'lc-node--inactive' : '',
    selected ? 'lc-node--selected' : '',
    state === 'related' ? 'lc-node--related' : '',
    state === 'hovered' ? 'lc-node--hovered' : '',
    impact ? 'lc-node--impact' : '',
    n.selected ? '' : 'lc-node--neighbor'].join(' ')
  return (
    <div {...rest} className={cls} title={canOpen(n) ? t.graph.card.clickToOpen(title(n)) : title(n)}>
      {count ? <span className="lc-node__count">{count}</span> : null}
      {impact ? <span className="lc-node__impact" title={t.graph.card.impactTitle(impact)}>{t.graph.card.impactBadge(impact)}</span> : null}
      <span className="lc-node__icon"><Glyph kind={kind} tech={mainTech(n)} /></span>
      <span className="lc-node__text">
        {copy ? (
          <span className="lc-copyable lc-node__head">
            <span className="lc-node__title">{title(n)}</span>
            <span className="nodrag"><CopyButton value={copy} /></span>
          </span>
        ) : <span className="lc-node__title">{title(n)}</span>}
        <span className="lc-node__meta">
          <span className="lc-node__info">{info(n)}</span>
          <span className="lc-node__techs">{foldedTechs(n).map((t) => <TechLogo key={t} tech={t} title={t} />)}</span>
        </span>
      </span>
      {cross?.length ? (
        <span className="lc-node__cross">
          {cross.map((c) => (
            <span key={c.env} className="lc-node__xenv" title={t.graph.card.crossEnv(c.count, c.env)}>
              <span aria-hidden="true">{c.up ? '↑' : '↓'}</span> {c.env} {c.count}
            </span>
          ))}
        </span>
      ) : null}
      {children}
    </div>
  )
}

export interface BoxData extends Record<string, unknown> {
  label: string
  meta?: string
  kind: 'lane' | 'host'
  // order is the place of an env lane, from the top.
  order?: number
  // minWidth and minHeight keep the cards of a lane when it is resized.
  minWidth?: number
  minHeight?: number
}

// BoxNode is an env lane or the box of a host.
// An env lane resizes by its right and bottom edges, never smaller than
// its cards: the left and top edges would leave them out.
export const BoxNode = memo(function BoxNode({ data }: NodeProps) {
  const { label, meta, kind, minWidth, minHeight } = data as BoxData
  return (
    <div className={`lc-box lc-box--${kind}`}>
      <div className="lc-box__head">{label}{meta ? <span className="lc-box__meta">{meta}</span> : null}</div>
      {kind === 'lane' ? (
        <>
          <NodeResizeControl position="right" variant={ResizeControlVariant.Line} className="lc-box__edge" minWidth={minWidth} minHeight={minHeight} />
          <NodeResizeControl position="bottom" variant={ResizeControlVariant.Line} className="lc-box__edge" minWidth={minWidth} minHeight={minHeight} />
          <NodeResizeControl position="bottom-right" className="lc-box__corner" minWidth={minWidth} minHeight={minHeight} />
        </>
      ) : null}
    </div>
  )
})
