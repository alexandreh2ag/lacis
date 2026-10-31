import { NODE_H, NODE_W } from './layout'
import type { GEdge, GNode, GraphData } from './query'

// The geometry of an opened zone: a header, then a row per kind of
// content, three cards wide.
export const ZONE_PAD = 24
export const ZONE_GAP = 16
export const ZONE_HEAD = 132
export const ROW_LABEL = 22
export const ROW_STEP = ROW_LABEL + NODE_H + 18
export const ZONE_W = 2 * ZONE_PAD + 3 * NODE_W + 2 * ZONE_GAP
// MAX_SHOWN is the most cards a row shows; past it, one card less and a
// "+N autres" card leading to the page of the zone.
export const MAX_SHOWN = 3

export type RowKey = 'apps' | 'components' | 'runtimes'

export interface ZoneRow {
  key: RowKey
  label: string
  total: number
  shown: GNode[]
  // more is the number of items the "+N autres" card stands for.
  more: number
  moreId: string
  // top is the place of the row in its zone.
  top: number
}

export interface Zone {
  id: string
  node: GNode
  rows: ZoneRow[]
  width: number
  height: number
}

const rowLabels: Record<RowKey, string> = { apps: 'Apps', components: 'Webservers et services', runtimes: 'Runtimes' }

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
      const shown = all.length > MAX_SHOWN ? all.slice(0, MAX_SHOWN - 1) : all
      rows.push({ key, label: rowLabels[key], total: all.length, shown, more: all.length - shown.length, moreId: `more:${g.id}|${key}`,
        top: ZONE_HEAD + rows.length * ROW_STEP })
    }
    zones.set(g.id, { id: g.id, node, rows, width: ZONE_W, height: ZONE_HEAD + Math.max(rows.length, 1) * ROW_STEP })
  }
  return zones
}

// cardPlace is where the i-th card of a row sits in its zone.
export function cardPlace(row: ZoneRow, i: number): { x: number; y: number } {
  return { x: ZONE_PAD + i * (NODE_W + ZONE_GAP), y: row.top + ROW_LABEL }
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
  for (const z of zones.values()) for (const r of z.rows) zoneOf.set(r.moreId, z.id)
  return (id) => zoneOf.get(id) ?? id
}
