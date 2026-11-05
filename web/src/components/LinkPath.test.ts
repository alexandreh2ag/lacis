import { describe, expect, it } from 'vitest'
import { crossing, hostOf, nameOf, techOf, type End, type PathLink } from './LinkPath'

const host = (id: string) => ({ id, hostname: id, project: { slug: 'shop' }, environment: { slug: 'prod' } })
const front: End = { __typename: 'App', id: 'a1', name: 'front', framework: 'nuxt', host: host('web1') }
const redis: End = { __typename: 'Service', id: 's1', type: 'redis', instance: 'default', host: host('db1') }
const pg: End = { __typename: 'Service', id: 's2', type: 'postgresql', instance: '16/main', host: host('db1') }
const s3: End = { __typename: 'External', target: 's3.example' }
const db1: End = { __typename: 'Host', ...host('db1') }

describe('ends', () => {
  it.each([
    [front, 'front', 'nuxt', 'web1'],
    [redis, 'redis', 'redis', 'db1'],
    [pg, 'postgresql 16/main', 'postgresql', 'db1'],
    [s3, 's3.example', 'ext', undefined],
    [db1, 'db1', 'host', 'db1'],
  ] as const)('%#', (end, name, tech, hostname) => {
    expect(nameOf(end)).toBe(name)
    expect(techOf(end)).toBe(tech)
    expect(hostOf(end)?.hostname).toBe(hostname)
  })
})

describe('crossing', () => {
  it('keeps the links going through a proxy', () => {
    const link = (id: string, via: string[]): PathLink => ({ id, confidence: 'EXACT', deadEnd: false, resolvedBy: [], via, from: front, to: redis })
    expect(crossing([link('a', []), link('b', ['traefik', 'nginx'])]).map((l) => l.id)).toEqual(['b'])
  })
})
