import { Handle, NodeResizeControl, Position, ResizeControlVariant, type NodeProps } from '@xyflow/react'
import { memo } from 'react'
import { CopyButton } from '@/components/Copy'
import { Glyph, TechLogo } from '@/components/Icon'
import { canOpen, countOf, foldedTechs, info, kindOf, mainTech, stacked, title } from './kinds'
import type { GNode } from './query'

export interface CardData extends Record<string, unknown> {
  node: GNode
  selected: boolean
  related: boolean
  faded: boolean
  // impact is the distance of the node to the target of the impact.
  impact?: number
  // copy is the hostname a card of the view of the hosts offers to copy.
  copy?: string
}

// NodeCard is a node of the graph as the design system draws it: always
// 216 × 56, the name cut, the logos of what it folds.
export const NodeCard = memo(function NodeCard({ data }: NodeProps) {
  const { node: n, selected, related, faded, impact, copy } = data as CardData
  const kind = kindOf(n)
  const count = countOf(n)
  const cls = ['lc-node', `lc-node--${kind}`,
    stacked(n) ? 'lc-node--stack' : '',
    n.divergences.length ? 'lc-node--diverges' : '',
    canOpen(n) ? 'lc-node--opens' : '',
    n.status === 'STALE' ? 'lc-node--stale' : '',
    n.status === 'INACTIVE' ? 'lc-node--inactive' : '',
    selected ? 'lc-node--selected' : '',
    related ? 'lc-node--related' : '',
    faded ? 'lc-node--faded' : '',
    impact ? 'lc-node--impact' : '',
    n.selected ? '' : 'lc-node--neighbor'].join(' ')
  return (
    <div className={cls} title={canOpen(n) ? `${title(n)} · cliquer pour déplier` : title(n)}>
      <Handle type="target" position={Position.Left} className="lc-handle" isConnectable={false} />
      {count ? <span className="lc-node__count">{count}</span> : null}
      {impact ? <span className="lc-node__impact" title={impact === 1 ? 'Impact direct' : `Impact de niveau ${impact}`}>{impact === 1 ? 'direct' : `niveau ${impact}`}</span> : null}
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
      <Handle type="source" position={Position.Right} className="lc-handle" isConnectable={false} />
    </div>
  )
})

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
