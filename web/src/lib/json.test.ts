import { field, isObject, num, resourcesOf, str, strings } from './json'

test('the guards keep the values of their type only', () => {
  expect([isObject({}), isObject([]), isObject(null), isObject('x')]).toEqual([true, false, false, false])
  expect([field({ a: 1 }, 'a'), field([1], '0'), field(null, 'a')]).toEqual([1, undefined, undefined])
  expect([str('a'), str(1)]).toEqual(['a', undefined])
  expect([num(2), num('2'), num(Infinity), num(NaN)]).toEqual([2, undefined, undefined, undefined])
  expect(strings(['a', 1, null, 'b'])).toEqual(['a', 'b'])
  expect(strings('a')).toEqual([])
})

test('resourcesOf reads the resources of a host, leaving out what is malformed', () => {
  expect(resourcesOf({ cpu: { effective_cores: 4 }, memory_mb: { effective: 8192 }, disks: [{ mount: '/', size_gb: 40 }] }))
    .toEqual({ cores: 4, memoryMb: 8192, disks: [{ mount: '/', sizeGb: 40 }] })
  expect(resourcesOf({ cpu: 'many', memory_mb: { effective: '8G' }, disks: [null, { mount: 1, size_gb: 2 }, { mount: '/data' }] }))
    .toEqual({ cores: undefined, memoryMb: undefined, disks: [] })
  expect(resourcesOf(null)).toEqual({ cores: undefined, memoryMb: undefined, disks: [] })
  expect(resourcesOf({ disks: { mount: '/' } })).toEqual({ cores: undefined, memoryMb: undefined, disks: [] })
})
