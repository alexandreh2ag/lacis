import { t } from '@/i18n'
import { NODE_H, NODE_W } from './layout'
import type { GEdge, GNode, GraphData } from './query'

// The geometry of an opened zone: a header, then a row per kind of
// content, three cards wide.
export const ZONE_PAD = 24
export const ZONE_GAP = 16
export const ZONE_HEAD = 132
export const ROW_LABEL = 22
export const ROW_STEP = ROW_LABEL + NODE_H + 18
// VISIBLE is the most cards a row shows at once; past it, the row slides.
export const VISIBLE = 3
// SLIDE_W is the width of the track of a row: its visible cards.
export const SLIDE_W = VISIBLE * NODE_W + (VISIBLE - 1) * ZONE_GAP
// CHEVRON and CHEVRON_GAP are the buttons on each side of a row that
// slides, and their space to the track.
export const CHEVRON = 28
export const CHEVRON_GAP = 8
export const ZONE_W = 2 * ZONE_PAD + SLIDE_W
// ZONE_SLIDING_W makes room for the chevrons when a row of the zone slides.
export const ZONE_SLIDING_W = ZONE_W + 2 * (CHEVRON + CHEVRON_GAP)

export type RowKey = 'apps' | 'components' | 'runtimes'

export interface ZoneRow {
  key: RowKey
  label: string
  // items are all the content of the row, the first VISIBLE shown at
  // first, the others a slide away.
  items: GNode[]
  // top is the place of the row in its zone.
  top: number
}

export interface Zone {
  id: string
  node: GNode
  rows: ZoneRow[]
  // sliding tells a row of the zone holds more than VISIBLE cards: every
  // row then leaves room for the chevrons.
  sliding: boolean
  width: number
  height: number
}

function rowOf(n: GNode): RowKey {
  return n.kind === 'APP' ? 'apps' : n.kind === 'RUNTIME' ? 'runtimes' : 'components'
}

// planZones lays the content of each opened node out in rows, what has a
// problem first: an app deployed in several versions.
export function planZones(graph: GraphData): Map<string, Zone> {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]))
  const zones = new Map<string, Zone>()
  for (const g of graph.groups) {
    const node = byId.get(g.id)
    if (!node?.expanded) continue
    const items = g.nodes.map((id) => byId.get(id)).filter((n): n is GNode => !!n)
    const rows: ZoneRow[] = []
    for (const key of ['apps', 'components', 'runtimes'] as RowKey[]) {
      const all = items.filter((n) => rowOf(n) === key)
        .sort((a, b) => Number(!a.divergences.length) - Number(!b.divergences.length) || a.label.localeCompare(b.label, 'fr', { numeric: true }))
      if (!all.length) continue
      rows.push({ key, label: t().graph.zone.rows[key], items: all, top: ZONE_HEAD + rows.length * ROW_STEP })
    }
    const sliding = rows.some((r) => r.items.length > VISIBLE)
    zones.set(g.id, { id: g.id, node, rows, sliding, width: sliding ? ZONE_SLIDING_W : ZONE_W, height: ZONE_HEAD + Math.max(rows.length, 1) * ROW_STEP })
  }
  return zones
}

// slideMax is the last first card a row can show: the row slides card by
// card, from 0 to it.
export function slideMax(row: ZoneRow): number {
  return Math.max(0, row.items.length - VISIBLE)
}

// slideTo is the card a drag of dx pixels from the card at settles on: the
// nearest, or the next one on a clear gesture, within the row.
export function slideTo(row: ZoneRow, at: number, dx: number): number {
  const step = NODE_W + ZONE_GAP
  let i = Math.round(at - dx / step)
  if (i === at && Math.abs(dx) > 30) i += dx < 0 ? 1 : -1
  return Math.max(0, Math.min(slideMax(row), i))
}

const confidenceRank: Record<string, number> = { EXACT: 0, INFERRED: 1, AMBIGUOUS: 2 }

// mergeEdges joins the edges an endpoint mapping makes equal: their links
// add up, they keep the weakest confidence; an edge mapped onto a single
// node goes.
function mergeEdges(edges: GEdge[], map: (id: string) => string): GEdge[] {
  const out = new Map<string, GEdge>()
  for (const e of edges) {
    const from = map(e.from)
    const to = map(e.to)
    if (from === to) continue
    const id = `${from}→${to}`
    const cur = out.get(id)
    if (!cur) {
      out.set(id, { ...e, id, from, to })
      continue
    }
    out.set(id, {
      ...cur,
      count: cur.count + e.count,
      confidence: confidenceRank[e.confidence] > confidenceRank[cur.confidence] ? e.confidence : cur.confidence,
      labels: [...new Set([...cur.labels, ...e.labels])],
      source: cur.source === e.source ? cur.source : 'mixed',
      crossEnv: cur.crossEnv || e.crossEnv,
    })
  }
  return [...out.values()]
}

// zoned is the graph the layout places and the page draws: an opened
// zone stands for its content, so that its links reach the zone itself.
export function zoned(graph: GraphData, zones: Map<string, Zone>): GraphData {
  const zoneOf = new Map<string, string>()
  for (const g of graph.groups) {
    if (zones.has(g.id)) for (const id of g.nodes) zoneOf.set(id, g.id)
  }
  return { ...graph, nodes: graph.nodes.filter((n) => !zoneOf.has(n.id)), edges: mergeEdges(graph.edges, (id) => zoneOf.get(id) ?? id), groups: [] }
}

// zoneOfItem maps the content of the opened zones to their zone.
export function zoneOfItem(graph: GraphData, zones: Map<string, Zone>): (id: string) => string {
  const zoneOf = new Map<string, string>()
  for (const g of graph.groups) {
    if (zones.has(g.id)) for (const id of g.nodes) zoneOf.set(id, g.id)
  }
  return (id) => zoneOf.get(id) ?? id
}
