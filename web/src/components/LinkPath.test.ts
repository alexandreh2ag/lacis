import { describe, expect, it } from 'vitest'
import { calledName, crosses, mergeRoutes, routeOf, routes, type End, type PathLink } from './LinkPath'

const host = (id: string) => ({ id, hostname: id, project: { slug: 'shop' }, environment: { slug: 'prod' } })
const front: End = { __typename: 'App', id: 'a1', name: 'front', framework: 'nuxt', host: host('web1') }
const api = (h: string): End => ({ __typename: 'App', id: 'a-' + h, name: 'api', framework: 'symfony', host: host(h) })
const traefik: End = { __typename: 'Webserver', id: 'w1', type: 'traefik', host: host('lb1') }

// link is front calling the api through the traefik.
const link = (backend: string): PathLink => ({
  key: 'API_URL', confidence: 'EXACT', deadEnd: false, from: front, to: api(backend),
  path: [
    { id: '1', rule: 'route', confidence: 'EXACT', detail: { key: 'API_URL', target: 'https://api.example/v1' }, to: traefik },
    { id: '2', rule: 'proxy', confidence: 'INFERRED', detail: { routes: ['api@docker'], domains: ['api.example', 'www.example'], cdn: 'cloudflare' }, to: api(backend) },
  ],
})

describe('calledName', () => {
  it.each([
    ['https://api.example/v1', 'api.example'],
    ['Api.Example:443', 'api.example'],
    ['10.0.0.1:80', undefined],
    ['[::1]:80', undefined],
    ['/run/php.sock', undefined],
  ])('%s', (target, want) => expect(calledName(target)).toBe(want))
})

describe('routes', () => {
  it.each([
    ['api.example', 'api.example', true],
    ['*.example', 'api.example', true],
    ['*.example', 'example', false],
    ['.example', 'example', true],
    ['~^api\\.', 'api.example', true],
    ['~(', 'api.example', false],
    ['www.example', 'api.example', false],
  ])('%s serves %s: %s', (pattern, name, want) => expect(routes(pattern, name)).toBe(want))
})

describe('routeOf', () => {
  it('reads the route step by step', () => {
    expect(routeOf(link('web2'))).toEqual([
      { end: front, hosts: ['web1'], details: [], names: [] },
      { end: traefik, hosts: ['lb1'], rule: 'route', details: ['API_URL = https://api.example/v1'], names: [], confidence: 'EXACT' },
      { end: api('web2'), hosts: ['web2'], rule: 'proxy · cloudflare', details: ['api@docker'], names: ['api.example'], confidence: 'INFERRED' },
    ])
  })
})

describe('mergeRoutes', () => {
  it('merges the hosts of the same route', () => {
    const got = mergeRoutes([routeOf(link('web3')), routeOf(link('web2'))])
    expect(got).toHaveLength(1)
    expect(got[0][2].hosts).toEqual(['web2', 'web3'])
    expect(crosses(got)).toBe(true)
  })

  it('keeps a direct link apart', () => {
    const direct: PathLink = { ...link('web2'), key: 'DB', path: [{ id: '3', rule: 'private_ip', confidence: 'EXACT', detail: { key: 'DB' }, to: api('web2') }] }
    const got = mergeRoutes([routeOf(direct), routeOf(link('web2'))])
    expect(got).toHaveLength(2)
    expect(crosses([got[0]])).toBe(false)
    expect(got[0][1].details).toEqual(['DB'])
  })
})
