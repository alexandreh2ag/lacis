import { kindOf } from './kinds'
import type { GNode } from './query'

const node = (kind: string, role?: string): GNode => ({
  id: `${kind.toLowerCase()}:1`, kind, label: kind, role, techs: [], count: 1, apps: 0, members: [], selected: true, expanded: false,
  hosts: [], divergences: [],
} as unknown as GNode)

test('a webserver draws as a proxy only when it routes', () => {
  expect(kindOf(node('WEBSERVER', 'proxy'))).toBe('proxy')
  expect(kindOf(node('WEBSERVER', 'web'))).toBe('service')
  expect(kindOf(node('WEBSERVER'))).toBe('service')
})
