import type { Node } from '@xyflow/react'
import { LANE_GAP } from './layout'
import { laneMinimum, separateLanes } from './lanes'

const lane = (id: string, order: number, y: number, height: number): Node =>
  ({ id, type: 'box', position: { x: 0, y }, width: 800, height, data: { kind: 'lane', order } })
const card = (id: string, parentId: string, x: number, y: number): Node =>
  ({ id, type: 'card', parentId, position: { x, y }, measured: { width: 216, height: 56 }, data: {} })

test('separateLanes leaves apart lanes alone', () => {
  const nodes = [lane('prod', 0, 0, 300), lane('staging', 1, 300 + LANE_GAP, 200)]
  expect(separateLanes(nodes)).toBe(nodes)
})

test('separateLanes pushes the lanes under a grown one', () => {
  const nodes = [lane('staging', 1, 400, 200), lane('prod', 0, 0, 450), lane('dev', 2, 700, 100), card('c', 'prod', 24, 44)]
  const out = separateLanes(nodes)
  expect(out.find((n) => n.id === 'prod')!.position.y).toBe(0)
  expect(out.find((n) => n.id === 'staging')!.position.y).toBe(450 + LANE_GAP)
  expect(out.find((n) => n.id === 'dev')!.position.y).toBe(450 + LANE_GAP + 200 + LANE_GAP)
  expect(out.find((n) => n.id === 'c')!.position).toEqual({ x: 24, y: 44 })
})

test('separateLanes keeps the order of the envs, not their places', () => {
  // staging, its top grown above prod, still goes under it.
  const out = separateLanes([lane('prod', 0, 100, 200), lane('staging', 1, 50, 300)])
  expect(out.find((n) => n.id === 'staging')!.position.y).toBe(300 + LANE_GAP)
})

test('laneMinimum holds the cards of the lane', () => {
  const prod = lane('prod', 0, 0, 300)
  expect(laneMinimum(prod, [prod, card('a', 'prod', 24, 44), card('b', 'prod', 400, 120), card('x', 'staging', 900, 900)]))
    .toEqual({ width: 400 + 216 + 24, height: 120 + 56 + 24 })
  expect(laneMinimum(prod, [prod])).toEqual({ width: 0, height: 44 })
})
