import type { Layout } from './layout'
import type { GEdge, GNode, GraphData } from './query'
import { hash, toFlowEdges, toFlowNodes } from './toFlow'
import { shapeOf } from './useGraphLayout'
import { planZones } from './zones'

const node = (id: string, kind: string, extra: Partial<GNode> = {}): GNode => ({
  id, kind, label: id, techs: [], count: 1, apps: 0, members: [], selected: true, expanded: false, hosts: [], divergences: [], ...extra,
} as GNode)
const edge = (from: string, to: string): GEdge => ({
  id: `${from}→${to}`, from, to, source: 'env', confidence: 'EXACT', rule: '', labels: [], count: 1, crossEnv: false,
} as GEdge)

const zone = 'project:shop@prod'
const item = `item:${zone}|app|api`
const graph: GraphData = {
  level: 'PROJECT',
  nodes: [node(zone, 'PROJECT', { expanded: true }), node('project:db@prod', 'PROJECT'), node(item, 'APP'), node('lost', 'PROJECT')],
  edges: [edge(item, 'project:db@prod'), edge('project:db@prod', 'lost')],
  groups: [{ id: zone, label: zone, nodes: [item] } as GraphData['groups'][number]],
} as GraphData
const placed: Layout = {
  nodes: new Map([[zone, { id: zone, x: 0, y: 0, parent: 'lane:prod' }], ['project:db@prod', { id: 'project:db@prod', x: 500, y: 0, parent: 'lane:prod' }]]),
  boxes: [{ id: 'lane:prod', label: 'prod', kind: 'lane', x: 0, y: 0, width: 900, height: 400 }],
  colors: new Map([[zone, 3]]),
}
const zones = planZones(graph)
const options = { ns: 'acme', level: 'PROJECT', impact: null, other: () => null, onClose: () => {} }

test('toFlowNodes draws the lanes, the cards placed, the opened zone and its content', () => {
  const nodes = toFlowNodes(graph, placed, zones, options)
  expect(nodes.map((n) => [n.id, n.type, n.parentId])).toEqual([
    ['lane:prod', 'box', undefined],
    [zone, 'zone', 'lane:prod'],
    ['project:db@prod', 'card', 'lane:prod'],
    [item, 'card', zone],
  ])
})

test('toFlowNodes marks the target and the levels of the impact', () => {
  const nodes = toFlowNodes(graph, placed, zones, { ...options, impact: { target: 'project:db@prod', levels: new Map([[item, 1]]), edges: new Set() } })
  expect(nodes.find((n) => n.id === 'project:db@prod')?.data).toMatchObject({ selected: true })
  expect(nodes.find((n) => n.id === item)?.data).toMatchObject({ selected: false, impact: 1 })
})

test('toFlowEdges keeps the edges between drawn nodes, an item taking the color of its zone', () => {
  const nodes = toFlowNodes(graph, placed, zones, options)
  const byId = new Map(graph.nodes.map((n) => [n.id, n]))
  const edges = toFlowEdges(graph, placed, nodes, byId, { target: item, levels: new Map(), edges: new Set([`${item}→project:db@prod`]) })
  expect(edges.map((e) => e.id)).toEqual([`${item}→project:db@prod`])
  expect(edges[0].data).toMatchObject({ color: 3, impact: true, fromLabel: item, toLabel: 'project:db@prod' })
  expect(hash('x')).toBe(hash('x'))
})

test('shapeOf changes with the nodes and edges, not with their data', () => {
  const same = { ...graph, nodes: graph.nodes.map((n) => ({ ...n, status: 'STALE' as const, version: 'v2' })) }
  expect(shapeOf(same, zones)).toBe(shapeOf(graph, zones))
  expect(shapeOf({ ...graph, edges: graph.edges.slice(1) }, zones)).not.toBe(shapeOf(graph, zones))
})
