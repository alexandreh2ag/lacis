import { routed } from './EdgeLayer'

test('routed draws the spline of ELK as cubic segments, else a smooth curve through its points', () => {
  const spline = routed([{ x: 0, y: 0 }, { x: 10, y: 0 }, { x: 20, y: 10 }, { x: 30, y: 10 }])
  expect(spline).toEqual({ path: 'M0,0 C10,0 20,10 30,10', labelX: 20, labelY: 10 })
  const bends = routed([{ x: 0, y: 0 }, { x: 10, y: 10 }, { x: 20, y: 0 }])
  expect(bends.path.match(/C/g)).toHaveLength(2)
  expect(bends.path.endsWith(' 20,0')).toBe(true)
})
