import { describe, expect, it } from 'vitest'
import { hostOf, mergeUsedBy, splitUsedBy, type UsedByRow } from './UsedBy'

const host = (id: string, project = 'shop', env = 'prod') => ({ id, hostname: id, project: { slug: project }, environment: { slug: env } })
const app = (id: string, h: ReturnType<typeof host>, name = 'front') => ({ __typename: 'App' as const, id, name, framework: 'symfony', host: h })
const mysql = (h?: { id: string; hostname: string }) => ({ name: 'mysql', tech: 'mysql', host: h })

describe('mergeUsedBy', () => {
  it.each<{ name: string; rows: UsedByRow[]; want: { env: string; targets?: string[]; keys: string[]; confidence: string }[] }>([
    {
      name: 'one app on two hosts of one env reads once',
      rows: [
        { id: '1', key: 'DATABASE_URL', confidence: 'EXACT', from: app('a1', host('web1')), target: mysql(host('db1')) },
        { id: '2', key: 'DATABASE_URL', confidence: 'INFERRED', from: app('a2', host('web2')), target: mysql(host('db1')) },
      ],
      want: [{ env: 'prod', keys: ['DATABASE_URL'], confidence: 'INFERRED' }],
    },
    {
      name: 'the envs stay apart',
      rows: [
        { id: '1', confidence: 'EXACT', from: app('a1', host('web1', 'shop', 'prod')), target: mysql() },
        { id: '2', confidence: 'EXACT', from: app('a2', host('web2', 'shop', 'staging')), target: mysql() },
      ],
      want: [
        { env: 'prod', keys: [], confidence: 'EXACT' },
        { env: 'staging', keys: [], confidence: 'EXACT' },
      ],
    },
    {
      name: 'the keys and the components add up, whatever the hosts',
      rows: [
        { id: '1', key: 'DB_HOST', confidence: 'EXACT', from: app('a1', host('web2')), target: mysql(host('db2')) },
        { id: '2', key: 'DB_READ', confidence: 'EXACT', from: app('a2', host('web1')), target: mysql(host('db1')) },
        { id: '3', key: 'REDIS_URL', confidence: 'EXACT', from: app('a2', host('web1')), target: { name: 'redis', tech: 'redis' } },
      ],
      want: [{ env: 'prod', targets: ['mysql', 'redis'], keys: ['DB_HOST', 'DB_READ', 'REDIS_URL'], confidence: 'EXACT' }],
    },
    {
      name: 'two apps stay apart',
      rows: [
        { id: '1', confidence: 'EXACT', from: app('a1', host('web1')), target: mysql() },
        { id: '2', confidence: 'EXACT', from: app('a2', host('web1'), 'back'), target: mysql() },
      ],
      want: [
        { env: 'prod', keys: [], confidence: 'EXACT' },
        { env: 'prod', keys: [], confidence: 'EXACT' },
      ],
    },
  ])('$name', ({ rows, want }) => {
    expect(mergeUsedBy(rows).map((g) => ({
      env: g.env, ...(g.targets.length > 1 ? { targets: g.targets.map((x) => x.name) } : {}), keys: g.keys, confidence: g.confidence,
    }))).toEqual(want)
  })
})

describe('splitUsedBy', () => {
  it('sets apart the links of the entity to itself', () => {
    const rows: UsedByRow[] = [
      { id: '1', confidence: 'EXACT', from: app('a1', host('web1', 'shop')), target: mysql() },
      { id: '2', confidence: 'EXACT', from: app('a2', host('web2', 'shop')), target: mysql() },
      { id: '3', confidence: 'EXACT', from: app('a3', host('crm1', 'crm'), 'crm'), target: mysql() },
    ]
    const got = splitUsedBy(rows, (r) => hostOf(r.from)?.project?.slug === 'shop')
    expect(got.others.map((g) => g.project)).toEqual(['crm'])
    expect(got.self.map((g) => g.project)).toEqual(['shop'])
  })
})
