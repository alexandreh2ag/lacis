import type { GEdge, GNode, GraphData } from './query'
import { cardPlace, MAX_SHOWN, planZones, ROW_LABEL, ROW_STEP, ZONE_HEAD, ZONE_PAD, zoned, zoneOfItem } from './zones'

const node = (id: string, kind: string, extra: Partial<GNode> = {}): GNode => ({
  id, kind, label: id.split('|').pop()!, techs: [], count: 1, apps: 0, members: [], selected: true, expanded: false,
  hosts: [], divergences: [], ...extra,
} as GNode)
const edge = (from: string, to: string, extra: Partial<GEdge> = {}): GEdge => ({
  id: `${from}→${to}`, from, to, source: 'env', confidence: 'EXACT', rule: 'private_ip', labels: [], count: 1, crossEnv: false, ...extra,
} as GEdge)

const zone = 'project:shop@production'
const item = (kind: string, label: string, extra: Partial<GNode> = {}) => node(`item:${zone}|${kind.toLowerCase()}|${label}`, kind, extra)
const apps = ['api-a', 'api-b', 'api-c', 'api-d'].map((l) => item('APP', l))
const diverging = item('APP', 'zz-worker', { divergences: [{ app: 'zz-worker', hosts: [] }] as unknown as GNode['divergences'] })
const nginx = item('WEBSERVER', 'nginx')
const php = item('RUNTIME', 'php 8.3.14')
const graph: GraphData = {
  level: 'PROJECT',
  nodes: [node(zone, 'PROJECT', { expanded: true }), node('project:infra@production', 'PROJECT'), ...apps, diverging, nginx, php],
  edges: [
    edge(nginx.id, apps[0].id),
    edge(apps[3].id, 'project:infra@production', { labels: ['DATABASE_URL'] }),
    edge(diverging.id, 'project:infra@production', { labels: ['CACHE_URL'], confidence: 'INFERRED' }),
    edge(apps[2].id, 'project:infra@production', { labels: ['QUEUE_URL'], count: 2 }),
    edge('project:infra@production', nginx.id),
  ],
  groups: [{ id: zone, label: zone, nodes: [...apps, diverging, nginx, php].map((n) => n.id) }],
} as GraphData

test('planZones shows three cards a row, what has a problem first', () => {
  const z = planZones(graph).get(zone)!
  expect(z.rows.map((r) => r.key)).toEqual(['apps', 'components', 'runtimes'])
  const [appsRow, components, runtimes] = z.rows
  expect(appsRow.total).toBe(5)
  expect(appsRow.shown.map((n) => n.label)).toEqual(['zz-worker', 'api-a'])
  expect(appsRow.more).toBe(3)
  expect(appsRow.shown.length + 1).toBe(MAX_SHOWN)
  expect(components.shown).toEqual([nginx])
  expect(components.more).toBe(0)
  expect(runtimes.top).toBe(ZONE_HEAD + 2 * ROW_STEP)
  expect(cardPlace(appsRow, 1)).toEqual({ x: ZONE_PAD + 216 + 16, y: ZONE_HEAD + ROW_LABEL })
  expect(planZones({ ...graph, groups: [{ ...graph.groups[0], id: 'project:infra@production' }] }).size).toBe(0)
})

test('zoned draws an opened zone alone, its links on the zone', () => {
  const zones = planZones(graph)
  const top = zoned(graph, zones)
  expect(top.nodes.map((n) => n.id)).toEqual([zone, 'project:infra@production'])
  expect(top.edges.map((e) => e.id)).toEqual([`${zone}→project:infra@production`, `project:infra@production→${zone}`])
  expect(top.edges[0].count).toBe(4)
  expect(top.edges[0].labels).toEqual(['DATABASE_URL', 'CACHE_URL', 'QUEUE_URL'])
  expect(top.edges[0].confidence).toBe('INFERRED')

  const of = zoneOfItem(graph, zones)
  expect(of(apps[3].id)).toBe(zone)
  expect(of(`more:${zone}|apps`)).toBe(zone)
  expect(of('project:infra@production')).toBe('project:infra@production')
})
