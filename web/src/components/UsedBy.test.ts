import { describe, expect, it } from 'vitest'
import { hostOf, mergeUsedBy, splitUsedBy, type UsedByRow } from './UsedBy'
import type { End, PathLink } from './LinkPath'

const host = (id: string, project = 'shop', env = 'prod') => ({ id, hostname: id, project: { slug: project }, environment: { slug: env } })
const app = (id: string, h: ReturnType<typeof host>, name = 'front') => ({ __typename: 'App' as const, id, name, framework: 'symfony', host: h })
const traefik = (h: ReturnType<typeof host>) => ({ __typename: 'Webserver' as const, id: 'w-' + h.id, type: 'traefik', host: h })
const mysql = (h?: { id: string; hostname: string }) => ({ name: 'mysql', tech: 'mysql', host: h })
const db = { __typename: 'Service' as const, id: 's1', type: 'mysql', instance: 'default', host: host('db1') }

// row is an effective link from a consumer to the database, through the
// proxies given.
function row(from: End, opts: { key?: string; confidence?: string; via?: End[] } = {}, target = mysql(host('db1'))): UsedByRow {
  const ends = [...(opts.via ?? []), db]
  const link: PathLink = {
    key: opts.key, confidence: opts.confidence ?? 'EXACT', deadEnd: false, from, to: db,
    path: ends.map((to, i) => ({ id: `${i}`, rule: i ? 'proxy' : 'route', confidence: 'EXACT', detail: {}, to })),
  }
  return { link, target }
}

describe('mergeUsedBy', () => {
  it.each<{ name: string; rows: UsedByRow[]; want: { env: string; targets?: string[]; keys: string[]; confidence: string }[] }>([
    {
      name: 'one app on two hosts of one env reads once',
      rows: [
        row(app('a1', host('web1')), { key: 'DATABASE_URL' }),
        row(app('a2', host('web2')), { key: 'DATABASE_URL', confidence: 'INFERRED' }),
      ],
      want: [{ env: 'prod', keys: ['DATABASE_URL'], confidence: 'INFERRED' }],
    },
    {
      name: 'the envs stay apart',
      rows: [
        row(app('a1', host('web1', 'shop', 'prod'))),
        row(app('a2', host('web2', 'shop', 'staging'))),
      ],
      want: [
        { env: 'prod', keys: [], confidence: 'EXACT' },
        { env: 'staging', keys: [], confidence: 'EXACT' },
      ],
    },
    {
      name: 'the keys add up whatever the hosts, each component its own row',
      rows: [
        row(app('a1', host('web2')), { key: 'DB_HOST' }, mysql(host('db2'))),
        row(app('a2', host('web1')), { key: 'DB_READ' }),
        row(app('a2', host('web1')), { key: 'REDIS_URL' }, { name: 'redis', tech: 'redis', host: undefined }),
      ],
      want: [
        { env: 'prod', targets: ['mysql'], keys: ['DB_HOST', 'DB_READ'], confidence: 'EXACT' },
        { env: 'prod', targets: ['redis'], keys: ['REDIS_URL'], confidence: 'EXACT' },
      ],
    },
    {
      name: 'two apps stay apart',
      rows: [
        row(app('a1', host('web1'))),
        row(app('a2', host('web1'), 'back')),
      ],
      want: [
        { env: 'prod', keys: [], confidence: 'EXACT' },
        { env: 'prod', keys: [], confidence: 'EXACT' },
      ],
    },
  ])('$name', ({ rows, want }) => {
    expect(mergeUsedBy(rows).map((g) => ({
      env: g.env, ...(want.some((w) => w.targets) ? { targets: g.targets.map((x) => x.name) } : {}), keys: g.keys, confidence: g.confidence,
    }))).toEqual(want)
  })

  it('merges the routes of one consumer across its hosts', () => {
    const groups = mergeUsedBy([
      row(app('a1', host('web1')), { via: [traefik(host('lb1', 'infra'))] }),
      row(app('a2', host('web2')), { via: [traefik(host('lb1', 'infra'))] }),
      row(app('a2', host('web2'))),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].routes.map((r) => r.map((s) => s.hosts.join(',')))).toEqual([['web1,web2', 'lb1', 'db1'], ['web2', 'db1']])
  })
})

describe('splitUsedBy', () => {
  it('sets apart the links of the entity to itself', () => {
    const rows = [
      row(app('a1', host('web1', 'shop'))),
      row(app('a2', host('web2', 'shop'))),
      row(app('a3', host('crm1', 'crm'), 'crm')),
    ]
    const got = splitUsedBy(rows, (r) => hostOf(r.link.from)?.project?.slug === 'shop')
    expect(got.others.map((g) => g.project)).toEqual(['crm'])
    expect(got.self.map((g) => g.project)).toEqual(['shop'])
  })
})
