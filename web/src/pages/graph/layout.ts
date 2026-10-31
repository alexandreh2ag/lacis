import ELK, { type ElkNode } from 'elkjs/lib/elk.bundled.js'
import type { GraphData } from './query'

// Every node of the graph has the same size: the name is cut, the panel
// gives it whole.
export const NODE_W = 216
export const NODE_H = 56

export interface Placed {
  id: string
  x: number
  y: number
  // parent is the lane or the host box holding it.
  parent?: string
}

export interface Box {
  id: string
  label: string
  meta?: string
  kind: 'lane' | 'host'
  x: number
  y: number
  width: number
  height: number
  parent?: string
}

export interface Layout {
  nodes: Map<string, Placed>
  boxes: Box[]
  // colors give each source node a color of link-1…8 in turn, top to
  // bottom then left to right, so that neighbouring edges differ.
  colors: Map<string, number>
}

const elk = new ELK()

// Spacing is the room around the cards: between two columns, and between
// two cards of a column.
export interface Spacing {
  between: number
  node: number
  // aspect is the width over the height of the screen the graph fills.
  aspect?: number
}

// MAX_BETWEEN bounds the room between two columns, however wide the
// screen.
export const MAX_BETWEEN = 520

// spacingFor gives the cards more room on a wider screen, so that a large
// one is not left empty on its sides: the columns spread with the width,
// the cards of a column half as much.
export function spacingFor(width: number, height = 0): Spacing {
  const f = Math.min(2.2, Math.max(1, width / 1400))
  return { between: Math.round(150 * f), node: Math.round(48 * (1 + (f - 1) / 2)), aspect: height > 0 ? Math.round((width / height) * 10) / 10 : undefined }
}

// widened is the room between columns that brings a graph of that width
// and height to the aspect of the screen: a tall graph, its envs stacked,
// spreads its columns instead of leaving the sides of a wide screen empty.
export function widened(spacing: Spacing, width: number, height: number, columns: number): number {
  if (!spacing.aspect || columns < 2 || height <= 0 || width / height >= spacing.aspect * 0.9) return spacing.between
  const extra = (spacing.aspect * height - width) / (columns - 1)
  return Math.min(MAX_BETWEEN, Math.round(spacing.between + extra))
}

function options(s: Spacing) {
  return {
    'elk.algorithm': 'layered',
    'elk.direction': 'RIGHT',
    'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
    'elk.spacing.nodeNode': String(s.node),
    'elk.layered.spacing.nodeNodeBetweenLayers': String(s.between),
    'elk.spacing.edgeNode': '24',
    'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  }
}

const boxOptions = { 'elk.padding': '[top=40,left=20,bottom=20,right=20]' }

// LANE_GAP is the space between two env lanes, stacked one under the other.
export const LANE_GAP = 96

const lanePadding = { top: 44, left: 24, bottom: 24, right: 24 }

export type Sizes = Map<string, { width: number; height: number }>

// envOrder sorts the lanes as the envs of the namespace; sizes give the
// nodes that are not cards, such as an opened zone, their size.
export async function layout(graph: GraphData, envOrder: string[], sizes: Sizes = new Map(), spacing = spacingFor(0)): Promise<Layout> {
  return graph.level === 'APP' ? hostBoxes(graph, spacing) : envLanes(graph, envOrder, sizes, spacing)
}

// envLanes lays each env out apart, then stacks the lanes from the first
// env down, as wide as the widest; the nodes without env come last.
async function envLanes(graph: GraphData, envOrder: string[], sizes: Sizes, spacing: Spacing): Promise<Layout> {
  const envs = [...new Set(graph.nodes.map((n) => n.env).filter((e): e is string => !!e))]
  envs.sort((a, b) => (envOrder.indexOf(a) + 1 || 999) - (envOrder.indexOf(b) + 1 || 999) || a.localeCompare(b))
  const groups: { env: string | null; nodes: GraphData['nodes'] }[] = envs.map((env) => ({ env, nodes: graph.nodes.filter((n) => n.env === env) }))
  const loose = graph.nodes.filter((n) => !n.env)
  if (loose.length) groups.push({ env: null, nodes: loose })

  const lay = (s: Spacing) => Promise.all(groups.map((g) => {
    const ids = new Set(g.nodes.map((n) => n.id))
    const pad = g.env ? lanePadding : { top: 0, left: 0, bottom: 0, right: 0 }
    const lane: ElkNode = {
      id: 'lane:' + (g.env ?? ''),
      layoutOptions: { ...options(s), 'elk.padding': `[top=${pad.top},left=${pad.left},bottom=${pad.bottom},right=${pad.right}]` },
      children: g.nodes.map((n) => ({ id: n.id, width: sizes.get(n.id)?.width ?? NODE_W, height: sizes.get(n.id)?.height ?? NODE_H })),
      edges: graph.edges.filter((e) => ids.has(e.from) && ids.has(e.to)).map((e) => ({ id: e.id, sources: [e.from], targets: [e.to] })),
    }
    return elk.layout(lane)
  }))
  let laid = await lay(spacing)
  // A graph narrower than the screen lays out again, its columns spread.
  const height = laid.reduce((h, l) => h + (l.height ?? 0), 0) + LANE_GAP * (laid.length - 1)
  const columns = Math.max(0, ...laid.map((l) => new Set((l.children ?? []).map((k) => Math.round(k.x ?? 0))).size))
  const between = widened(spacing, Math.max(0, ...laid.map((l) => l.width ?? 0)), height, columns)
  if (between !== spacing.between) laid = await lay({ ...spacing, between })

  const width = Math.max(0, ...laid.map((l) => l.width ?? 0))
  const placed = new Map<string, Placed>()
  const boxes: Box[] = []
  const absolute = new Map<string, { x: number; y: number }>()
  let y = 0
  groups.forEach((g, i) => {
    const l = laid[i]
    if (g.env) {
      const count = g.nodes.filter((n) => n.kind !== 'EXTERNAL').reduce((s, n) => s + (n.kind === 'PROJECT' ? n.count : n.members.length || 1), 0)
      boxes.push({ id: l.id, label: g.env, meta: `${count} host${count > 1 ? 's' : ''}`, kind: 'lane', x: 0, y, width, height: l.height ?? 0 })
    }
    for (const k of l.children ?? []) {
      placed.set(k.id, g.env ? { id: k.id, x: k.x ?? 0, y: k.y ?? 0, parent: l.id } : { id: k.id, x: k.x ?? 0, y: y + (k.y ?? 0) })
      absolute.set(k.id, { x: k.x ?? 0, y: y + (k.y ?? 0) })
    }
    y += (l.height ?? 0) + LANE_GAP
  })
  return { nodes: placed, boxes, colors: colors(graph, absolute) }
}

// colors give each source node a color of link-1…8 in turn.
function colors(graph: GraphData, absolute: Map<string, { x: number; y: number }>): Map<string, number> {
  const sources = [...new Set(graph.edges.map((e) => e.from))].filter((id) => absolute.has(id))
  sources.sort((a, b) => absolute.get(a)!.x - absolute.get(b)!.x || absolute.get(a)!.y - absolute.get(b)!.y)
  return new Map(sources.map((id, i) => [id, (i % 8) + 1]))
}

// hostBoxes lays the apps level out at once, the nodes of a host in its box.
async function hostBoxes(graph: GraphData, spacing: Spacing): Promise<Layout> {
  const children: ElkNode[] = []
  const boxes = new Map<string, ElkNode & { label: string; meta?: string; kind: 'lane' | 'host' }>()
  const parentOf = new Map<string, string>()
  for (const g of graph.groups) {
    // A host holding one node needs no box: the node names its host.
    if (g.nodes.length < 2) continue
    boxes.set(g.id, { id: g.id, label: g.label, kind: 'host', children: [], layoutOptions: boxOptions })
    for (const id of g.nodes) parentOf.set(id, g.id)
  }
  for (const n of graph.nodes) {
    const node: ElkNode = { id: n.id, width: NODE_W, height: NODE_H }
    const parent = parentOf.get(n.id)
    if (parent && boxes.has(parent)) boxes.get(parent)!.children!.push(node)
    else children.push(node)
  }
  children.push(...boxes.values())
  const ids = new Set(graph.nodes.map((n) => n.id))
  const root: ElkNode = {
    id: 'root',
    layoutOptions: options(spacing),
    children,
    edges: graph.edges.filter((e) => ids.has(e.from) && ids.has(e.to)).map((e) => ({ id: e.id, sources: [e.from], targets: [e.to] })),
  }
  const out = await elk.layout(root)

  const placed = new Map<string, Placed>()
  const placedBoxes: Box[] = []
  const absolute = new Map<string, { x: number; y: number }>()
  for (const c of out.children ?? []) {
    const box = boxes.get(c.id)
    if (box) {
      placedBoxes.push({ id: c.id, label: box.label, meta: box.meta, kind: box.kind, x: c.x ?? 0, y: c.y ?? 0, width: c.width ?? 0, height: c.height ?? 0 })
      for (const k of c.children ?? []) {
        placed.set(k.id, { id: k.id, x: k.x ?? 0, y: k.y ?? 0, parent: c.id })
        absolute.set(k.id, { x: (c.x ?? 0) + (k.x ?? 0), y: (c.y ?? 0) + (k.y ?? 0) })
      }
    } else {
      placed.set(c.id, { id: c.id, x: c.x ?? 0, y: c.y ?? 0 })
      absolute.set(c.id, { x: c.x ?? 0, y: c.y ?? 0 })
    }
  }
  return { nodes: placed, boxes: placedBoxes, colors: colors(graph, absolute) }
}
