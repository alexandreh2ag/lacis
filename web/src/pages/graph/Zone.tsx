import { useQuery } from '@apollo/client'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { memo, useContext, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { Link } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Glyph } from '@/components/Icon'
import { StatusBadge } from '@/components/Listing'
import { useT } from '@/i18n'
import { ago, plural } from '@/lib/format'
import { HighlightContext } from './highlight'
import { kindOf, title } from './kinds'
import { NODE_W } from './layout'
import { CardFace } from './NodeCard'
import { ZoneHostsDoc, type GNode } from './query'
import { CHEVRON, CHEVRON_GAP, ROW_LABEL, slideMax, slideTo, SLIDE_W, VISIBLE, ZONE_GAP, ZONE_PAD, type RowKey, type Zone, type ZoneRow } from './zones'

export interface ZoneData extends Record<string, unknown> {
  zone: Zone
  ns: string
  // other is the link to the zone in the other lens.
  other: { label: string; to: string } | null
  onClose: () => void
  onItem: (n: GNode) => void
}

// pageOf is the page of a zone, on the tab listing a row.
export function pageOf(ns: string, n: GNode, row?: RowKey): string {
  if (n.kind === 'PROJECT') return `/${ns}/projects/${encodeURIComponent(n.project ?? '')}${row === 'apps' ? '?tab=apps' : row ? '?tab=hosts' : ''}`
  if (n.hosts.length === 1) return `/${ns}/hosts/${n.hosts[0].id}${row === 'apps' ? '?tab=apps' : row ? '?tab=components' : ''}`
  return n.project ? `/${ns}/projects/${encodeURIComponent(n.project)}?tab=hosts` : `/${ns}/hosts/${n.hosts[0]?.id ?? ''}`
}

// Header tells what the zone is: the project or the host, its state, and
// where to read more.
function Header({ zone, ns, other, onClose }: ZoneData) {
  const n = zone.node
  const one = n.hosts.length === 1
  const { data } = useQuery(ZoneHostsDoc, {
    skip: !one && !n.project,
    variables: { ns, one, id: n.hosts[0]?.id ?? '', filter: { projects: n.project ? [n.project] : [], envs: n.env ? [n.env] : [], status: ['ACTIVE', 'STALE', 'INACTIVE'] } },
  })
  const host = one ? data?.namespace?.host ?? null : null
  const ids = new Set(n.hosts.map((h) => h.id))
  const hosts = one ? (host ? [host] : []) : (data?.namespace?.hosts?.items ?? []).filter((h) => ids.has(h.id))
  const apps = zone.rows.find((r) => r.key === 'apps')
  const diverging = apps?.items.filter((a) => a.divergences.length) ?? []
  const first = diverging[0]?.divergences[0]
  const ip = host?.ips.find((i) => i.defaultRoute) ?? host?.ips[0]
  const lastSeen = hosts.map((h) => h.lastSeenAt).sort()[0]
  const t = useT()
  return (
    <div className="lc-zone__head">
      <div className="flex items-center gap-2.5">
        <span className={`lc-node__icon lc-node--${kindOf(n)}`} style={{ width: 28, height: 28 }}><Glyph kind={kindOf(n)} /></span>
        <span className={n.kind === 'HOST' ? 'font-mono text-[14px] font-semibold' : 'heading'}>{title(n)}</span>
        {n.kind === 'HOST' && host ? <span className="nodrag -ml-2"><CopyButton value={host.hostname} /></span> : null}
        <span className="lc-muted text-[12.5px]">{[n.kind === 'PROJECT' ? t.graph.zone.project : n.hosts.length > 1 ? t.graph.zone.hosts(n.hosts.length) : t.graph.zone.host, n.kind === 'HOST' ? n.project : null, n.env].filter(Boolean).join(' · ')}</span>
        {n.status ? <StatusBadge status={n.status} /> : null}
        <span className="flex-1" />
        <button type="button" className="lc-btn lc-btn--ghost nodrag h-[26px]" onClick={onClose}>{t.graph.zone.close}</button>
      </div>
      <span className="lc-zone__line">
        {host ? [host.os?.pretty, ip?.address].filter(Boolean).join(' · ') : t.graph.zone.members(n.hosts.length, n.members.join(', '))}
        {apps ? ` · ${plural(apps.items.length, 'app')}` : ''}
      </span>
      {first ? (
        <span className="lc-badge lc-badge--stale self-start">
          {first.app} : {first.hosts.map((h) => t.graph.zone.on(h.version || h.commit?.slice(0, 7) || '?', h.hostname)).join(', ')}
          {diverging.length > 1 ? t.graph.zone.others(diverging.length - 1) : ''}
        </span>
      ) : (
        <span className="lc-zone__line">
          {host?.agentVersion ? t.graph.zone.agent(host.agentVersion) : ''}{lastSeen ? t.graph.zone.lastSnapshot(ago(lastSeen)) : ''}
        </span>
      )}
      <span className="nodrag flex gap-3.5 text-[12.5px]">
        <Link to={pageOf(ns, n)}>{n.kind === 'PROJECT' ? t.graph.zone.projectPage : t.graph.zone.hostPage}</Link>
        {other ? <Link to={other.to}>{other.label}</Link> : null}
        <Link to={`/${ns}/history`}>{t.graph.zone.history}</Link>
      </span>
    </div>
  )
}

// ZoneNode is a project, a host or a stack opened on the graph: its
// header, then its content in rows of three cards; a row holding more
// slides. The links of the content are those of the zone.
export const ZoneNode = memo(function ZoneNode({ data }: NodeProps) {
  const d = data as ZoneData
  const t = useT()
  return (
    <div className="lc-zone" style={{ width: d.zone.width, height: d.zone.height }}>
      <Handle type="target" position={Position.Left} className="lc-handle" isConnectable={false} />
      <Header {...d} />
      {d.zone.rows.length === 0 ? <span className="lc-muted absolute text-[13px]" style={{ left: ZONE_PAD, top: d.zone.height - 60 }}>{t.graph.zone.empty}</span> : null}
      {d.zone.rows.map((r) => <Row key={r.key} row={r} sliding={d.zone.sliding} to={pageOf(d.ns, d.zone.node, r.key)} onItem={d.onItem} />)}
      <Handle type="source" position={Position.Right} className="lc-handle" isConnectable={false} />
    </div>
  )
})

const STEP = NODE_W + ZONE_GAP
// DRAG_MIN is how far a pointer moves before a press becomes a drag, under
// which it stays a click.
const DRAG_MIN = 5

function Chevron({ dir, row, hidden, onClick }: { dir: -1 | 1; row: string; hidden: number; onClick: () => void }) {
  const t = useT()
  return (
    <button type="button" className={`lc-zone__chevron lc-zone__chevron--${dir < 0 ? 'prev' : 'next'}`} disabled={!hidden}
      aria-label={dir < 0 ? t.graph.zone.previous(row) : t.graph.zone.next(row)}
      title={hidden ? (dir < 0 ? t.graph.zone.hiddenBefore(hidden) : t.graph.zone.hiddenAfter(hidden)) : undefined}
      onClick={(e) => { e.stopPropagation(); onClick() }}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d={dir < 0 ? 'M10 3 5 8l5 5' : 'M6 3l5 5-5 5'} /></svg>
      {hidden ? <span className="lc-zone__hidden">{hidden}</span> : null}
    </button>
  )
}

// Row is a kind of content of a zone. Past VISIBLE cards, it slides card
// by card: by its chevrons, a drag of the mouse or the finger, the
// horizontal wheel, or the arrows of the keyboard once focused.
function Row({ row, sliding, to, onItem }: { row: ZoneRow; sliding: boolean; to: string; onItem: (n: GNode) => void }) {
  const t = useT()
  const highlight = useContext(HighlightContext)
  const max = slideMax(row)
  const slides = max > 0
  const [at, setAt] = useState(0)
  // offset is the move of a drag under way, in pixels of the graph.
  const [offset, setOffset] = useState<number | null>(null)
  const drag = useRef<{ x: number; id: number; scale: number; moved: boolean } | null>(null)
  // dragged swallows the click ending a drag, which is no click on a card.
  const dragged = useRef(false)
  const viewport = useRef<HTMLDivElement>(null)
  const first = Math.min(at, max)
  const go = (i: number) => setAt(Math.max(0, Math.min(max, i)))

  // A horizontal wheel slides the row and stops there, short of the zoom
  // of the graph; a vertical one goes on to the graph. The listener is
  // native for it runs before the one of the graph.
  const wheel = useRef({ acc: 0, at: 0, max: 0 })
  wheel.current.at = first
  wheel.current.max = max
  useEffect(() => {
    const el = viewport.current
    if (!el || !slides) return
    let reset: ReturnType<typeof setTimeout> | undefined
    const onWheel = (e: WheelEvent) => {
      const dx = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.shiftKey ? e.deltaY : 0
      if (!dx) return
      e.preventDefault()
      e.stopPropagation()
      const w = wheel.current
      w.acc += dx
      clearTimeout(reset)
      reset = setTimeout(() => { w.acc = 0 }, 200)
      if (Math.abs(w.acc) < 60) return
      setAt(Math.max(0, Math.min(w.max, w.at + Math.sign(w.acc))))
      w.acc = 0
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => { el.removeEventListener('wheel', onWheel); clearTimeout(reset) }
  }, [slides])

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!slides || e.button !== 0) return
    const el = e.currentTarget
    // The graph may be zoomed: a pixel of the screen is not one of the row.
    drag.current = { x: e.clientX, id: e.pointerId, scale: el.getBoundingClientRect().width / el.offsetWidth || 1, moved: false }
  }
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current
    if (!d || e.pointerId !== d.id) return
    const dx = (e.clientX - d.x) / d.scale
    if (!d.moved && Math.abs(dx) < DRAG_MIN) return
    if (!d.moved) {
      d.moved = true
      e.currentTarget.setPointerCapture(e.pointerId)
    }
    // Past the ends, the row resists.
    const x = -first * STEP + dx
    const lo = -max * STEP
    setOffset((x > 0 ? x * 0.35 : x < lo ? lo + (x - lo) * 0.35 : x) + first * STEP)
  }
  const onPointerEnd = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current
    if (!d || e.pointerId !== d.id) return
    drag.current = null
    if (!d.moved) return
    dragged.current = true
    go(slideTo(row, first, offset ?? 0))
    setOffset(null)
  }

  const x = -first * STEP + (offset ?? 0)
  return (
    <div className="lc-zone__row" style={{ top: row.top, left: ZONE_PAD, right: ZONE_PAD }}>
      {/* With the chevrons, the head lines up with the cards, clear of their counts. */}
      <div className="lc-zone__row-head" style={{ height: ROW_LABEL, paddingInline: sliding ? CHEVRON + CHEVRON_GAP : 0 }}>
        <span className="lc-label">{row.label} <span className="font-normal tracking-normal">· {row.items.length}</span></span>
        {slides ? <span className="lc-zone__pos">{t.graph.zone.position(first + 1, first + VISIBLE, row.items.length)}</span> : null}
        <span className="flex-1" />
        {slides ? <Link className="nodrag text-[12px]" to={to}>{t.graph.zone.seeAll}</Link> : null}
      </div>
      <div className="lc-zone__slider" style={{ gap: CHEVRON_GAP, paddingLeft: sliding && !slides ? CHEVRON + CHEVRON_GAP : 0 }}>
        {slides ? <Chevron dir={-1} row={row.label} hidden={first} onClick={() => go(first - 1)} /> : null}
        <div ref={viewport} style={{ width: SLIDE_W }}
          className={`lc-zone__track nodrag nopan${slides ? ' lc-zone__track--slides' : ''}${offset !== null ? ' lc-zone__track--dragging' : ''}`}
          tabIndex={slides ? 0 : undefined} role={slides ? 'group' : undefined} aria-roledescription={slides ? 'carousel' : undefined} aria-label={slides ? row.label : undefined}
          onKeyDown={(e) => {
            if (!slides) return
            const next = { ArrowLeft: first - 1, ArrowRight: first + 1, Home: 0, End: max }[e.key]
            if (next === undefined) return
            e.preventDefault()
            e.stopPropagation()
            go(next)
          }}
          onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerEnd} onPointerCancel={onPointerEnd}
          onClickCapture={(e) => {
            if (!dragged.current) return
            dragged.current = false
            e.stopPropagation()
          }}>
          <div className="lc-zone__cards" style={{ gap: ZONE_GAP, transform: `translateX(${x}px)` }}>
            {row.items.map((n) => (
              <CardFace key={n.id} id={n.id} data={{ node: n, selected: false }}
                onClick={(e) => { e.stopPropagation(); onItem(n) }}
                onMouseEnter={() => highlight.hoverNodeOn(n.id)} onMouseLeave={() => highlight.hoverNodeOn(null)} />
            ))}
          </div>
        </div>
        {slides ? <Chevron dir={1} row={row.label} hidden={max - first} onClick={() => go(first + 1)} /> : null}
      </div>
      {slides ? (
        <div className="lc-zone__dots" aria-hidden="true">
          {Array.from({ length: max + 1 }, (_, i) => <i key={i} className={i === first ? 'lc-zone__dot--on' : undefined} />)}
        </div>
      ) : null}
    </div>
  )
}
