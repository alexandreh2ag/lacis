import type { GEdge, GNode, GraphData } from './query'
import { planZones, ROW_STEP, slideMax, slideTo, ZONE_HEAD, ZONE_SLIDING_W, ZONE_W, zoned, zoneOfItem } from './zones'

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

test('planZones lays the content out in rows, what has a problem first, all of it', () => {
  const z = planZones(graph).get(zone)!
  expect(z.rows.map((r) => r.key)).toEqual(['apps', 'components', 'runtimes'])
  const [appsRow, components, runtimes] = z.rows
  expect(appsRow.items.map((n) => n.label)).toEqual(['zz-worker', 'api-a', 'api-b', 'api-c', 'api-d'])
  expect(components.items).toEqual([nginx])
  expect(runtimes.top).toBe(ZONE_HEAD + 2 * ROW_STEP)
  // A row of five slides: the zone makes room for its chevrons.
  expect(z.sliding).toBe(true)
  expect(z.width).toBe(ZONE_SLIDING_W)
  expect(planZones({ ...graph, groups: [{ ...graph.groups[0], nodes: [nginx.id, php.id] }] }).get(zone)).toMatchObject({ sliding: false, width: ZONE_W })
  expect(planZones({ ...graph, groups: [{ ...graph.groups[0], id: 'project:infra@production' }] }).size).toBe(0)
})

test('a row slides card by card, within its ends', () => {
  const [appsRow, components] = planZones(graph).get(zone)!.rows
  expect(slideMax(appsRow)).toBe(2)
  expect(slideMax(components)).toBe(0)
  // A drag settles on the nearest card, a clear short one on the next.
  expect(slideTo(appsRow, 0, -240)).toBe(1)
  expect(slideTo(appsRow, 0, -40)).toBe(1)
  expect(slideTo(appsRow, 1, 40)).toBe(0)
  expect(slideTo(appsRow, 1, 10)).toBe(1)
  expect(slideTo(appsRow, 0, 300)).toBe(0)
  expect(slideTo(appsRow, 1, -2000)).toBe(2)
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
  expect(of('project:infra@production')).toBe('project:infra@production')
})
