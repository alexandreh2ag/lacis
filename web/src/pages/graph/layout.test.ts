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
