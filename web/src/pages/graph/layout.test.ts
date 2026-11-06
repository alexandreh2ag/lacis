import { MAX_BETWEEN, spacingFor, widened } from './layout'

test('spacingFor spreads the cards on a wider screen, within bounds', () => {
  expect(spacingFor(1200)).toEqual({ between: 150, node: 48, aspect: undefined })
  expect(spacingFor(2312, 1384)).toEqual({ between: 248, node: 64, aspect: 1.7 })
  expect(spacingFor(5000).between).toBe(330)
})

test('widened spreads the columns of a tall graph to the aspect of the screen', () => {
  const s = { between: 248, node: 64, aspect: 1.7 }
  // 3 columns, 1000 wide for 700 high: 1190 wide would fit the screen.
  expect(widened(s, 1000, 700, 3)).toBe(248 + 95)
  expect(widened({ ...s, between: 150 }, 1000, 700, 3)).toBe(150 + 95)
  expect(widened(s, 1000, 1100, 3)).toBe(MAX_BETWEEN)
  expect(widened(s, 1800, 1100, 3)).toBe(248)
  expect(widened(s, 1000, 1100, 1)).toBe(248)
  expect(widened({ ...s, aspect: undefined }, 1000, 1100, 3)).toBe(248)
})

const calls = vi.hoisted(() => ({ n: 0 }))
vi.mock('elkjs/lib/elk-api.js', () => ({
  default: class {
    layout(g: { id: string }) {
      calls.n++
      return Promise.resolve({ ...g, x: 0, y: 0 })
    }
  },
}))

test('layoutLane lays a lane out once while it stays the same', async () => {
  const { layoutLane } = await import('./layout')
  const lane = { id: 'lane:production', children: [{ id: 'a', width: 216, height: 56 }], edges: [] }
  calls.n = 0
  await layoutLane(lane, 0)
  await layoutLane({ ...lane }, 1)
  expect(calls.n).toBe(1)
  await layoutLane({ ...lane, children: [{ id: 'b', width: 216, height: 56 }] }, 0)
  expect(calls.n).toBe(2)
})
