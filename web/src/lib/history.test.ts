import { describe as describeEvent, diffs } from './history'

test('a deployment tells its version and commit', () => {
  const d = describeEvent({ id: 1, occurredAt: '', entityType: 'app', kind: 'changed',
    before: { name: 'shop-api', code: { version: 'v2.4.0', commit: '3f2a9c1' } },
    after: { name: 'shop-api', code: { version: 'v2.5.0', commit: 'a81c3e7' } } })
  expect(d.title).toBe('App déployé')
  expect(d.subject).toBe('shop-api')
  expect(d.diffs).toEqual([
    { field: 'version', before: 'v2.4.0', after: 'v2.5.0' },
    { field: 'commit', before: '3f2a9c1', after: 'a81c3e7' },
  ])
})

test('a removed service and a link', () => {
  expect(describeEvent({ id: 2, occurredAt: '', entityType: 'service', kind: 'removed', before: { type: 'redis', instance: 'default', version: '7.0.15' } }))
    .toMatchObject({ title: 'Service retiré', subject: 'redis', diffs: [] })
  expect(describeEvent({ id: 3, occurredAt: '', entityType: 'link', kind: 'added', after: { from: 'crm-api', to: 'acme-mongodb-prod-1/mongodb', key: 'MONGODB_URL', source: 'env' } }))
    .toMatchObject({ subject: 'crm-api → acme-mongodb-prod-1/mongodb', detail: 'MONGODB_URL · env' })
})

test('diffs leave the identity out', () => {
  expect(diffs('{"hostname":"a","kernel":"6.1"}', '{"hostname":"a","kernel":"6.8"}')).toEqual([{ field: 'noyau', before: '6.1', after: '6.8' }])
})
