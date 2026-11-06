import type { Node } from '@xyflow/react'
import { t } from '@/i18n'
import { isStack, title } from './kinds'
import type { LinkData, LinkEdge } from './EdgeLayer'
import type { Layout } from './layout'
import type { BoxData, CardData } from './NodeCard'
import type { GNode, GraphData } from './query'
import { pageOf, type MoreData, type ZoneData } from './Zone'
import { cardPlace, type Zone } from './zones'

export interface FlowImpact {
  target: string
  levels: Map<string, number>
  edges: Set<string>
}

export interface FlowOptions {
  ns: string
  level: string
  impact: FlowImpact | null
  // other is the link of a zone to the same thing in the other lens.
  other: (n: GNode) => ZoneData['other']
  onClose: (id: string) => void
}

// toFlowNodes are the nodes React Flow draws: the lanes and host boxes,
// the cards, the opened zone and its content. Their look under a hover
// comes from the highlight, so a hover does not build them again.
export function toFlowNodes(view: GraphData, placed: Layout, zones: Map<string, Zone>, o: FlowOptions): Node[] {
  const card = (n: GNode): CardData => ({ node: n, selected: !!o.impact && n.id === o.impact.target, impact: o.impact?.levels.get(n.id),
    copy: o.level === 'HOST' && n.kind === 'HOST' && !isStack(n) ? n.hostname ?? n.label : undefined })
  const out: Node[] = placed.boxes.map((b, i) => ({
    id: b.id, type: 'box', position: { x: b.x, y: b.y }, data: { label: b.label, meta: b.meta, kind: b.kind, order: i } satisfies BoxData,
    width: b.width, height: b.height, selectable: false, draggable: false, zIndex: -1,
  }))
  const content: Node[] = []
  for (const n of view.nodes) {
    const at = placed.nodes.get(n.id)
    if (!at) continue
    const zone = zones.get(n.id)
    if (!zone) {
      out.push({ id: n.id, type: 'card', position: { x: at.x, y: at.y }, parentId: at.parent, expandParent: !!at.parent, data: card(n) })
      continue
    }
    out.push({
      id: n.id, type: 'zone', position: { x: at.x, y: at.y }, parentId: at.parent, expandParent: !!at.parent, width: zone.width, height: zone.height,
      data: { zone, ns: o.ns, other: o.other(n), onClose: () => o.onClose(n.id) } satisfies ZoneData,
    })
    for (const row of zone.rows) {
      row.shown.forEach((item, i) => {
        content.push({ id: item.id, type: 'card', position: cardPlace(row, i), parentId: zone.id, draggable: false, data: card(item) })
      })
      if (row.more) {
        const what = row.key === 'apps' ? 'apps' : row.key === 'runtimes' ? 'runtimes' : ''
        content.push({
          id: row.moreId, type: 'more', position: cardPlace(row, row.shown.length), parentId: zone.id, draggable: false, selectable: false,
          data: { label: t().graph.more(row.more, what), to: pageOf(o.ns, n, row.key) } satisfies MoreData,
        })
      }
    }
  }
  return [...out, ...content]
}

// toFlowEdges are the edges between two nodes drawn, each of the color of
// its source; a source the layout did not place, inside a zone, takes the
// color of its zone.
export function toFlowEdges(view: GraphData, placed: Layout, nodes: Node[], byId: Map<string, GNode>, impact: FlowImpact | null): LinkEdge[] {
  const parentOf = new Map(nodes.map((n) => [n.id, n.parentId]))
  const label = (id: string) => {
    const n = byId.get(id)
    return n ? title(n) : t().graph.others
  }
  const color = (id: string) => placed.colors.get(id) ?? placed.colors.get(parentOf.get(id) ?? '') ?? (hash(id) % 8) + 1
  return view.edges.filter((e) => parentOf.has(e.from) && parentOf.has(e.to)).map((e) => ({
    id: e.id, source: e.from, target: e.to, type: 'link',
    data: { edge: e, color: color(e.from), fromLabel: label(e.from), toLabel: label(e.to), impact: !!impact?.edges.has(e.id) } satisfies LinkData,
  }))
}

export function hash(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return Math.abs(h)
}
