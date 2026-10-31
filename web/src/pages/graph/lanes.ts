import type { Node } from '@xyflow/react'
import { LANE_GAP, NODE_H, NODE_W } from './layout'

// LANE_PAD is the room a lane keeps around its cards, its title above.
export const LANE_PAD = { top: 44, right: 24, bottom: 24 }

function isLane(n: Node): boolean {
  return n.type === 'box' && (n.data as { kind?: string }).kind === 'lane'
}

function height(n: Node): number {
  return n.height ?? n.measured?.height ?? 0
}

// separateLanes keeps the env lanes in their order, one under the other,
// LANE_GAP apart at least: a lane grown or moved over the next pushes it
// down, and those under it. It returns nodes itself when no lane moves.
export function separateLanes(nodes: Node[]): Node[] {
  const lanes = nodes.filter(isLane).sort((a, b) => ((a.data.order as number) ?? 0) - ((b.data.order as number) ?? 0))
  const moved = new Map<string, number>()
  let bottom = -Infinity
  for (const lane of lanes) {
    const y = Math.max(lane.position.y, bottom + LANE_GAP)
    if (y !== lane.position.y) moved.set(lane.id, y)
    bottom = y + height(lane)
  }
  if (moved.size === 0) return nodes
  return nodes.map((n) => (moved.has(n.id) ? { ...n, position: { x: n.position.x, y: moved.get(n.id)! } } : n))
}

// laneMinimum is the smallest size a lane keeps its cards in, resized by
// its right and bottom edges.
export function laneMinimum(lane: Node, nodes: Node[]): { width: number; height: number } {
  let width = 0
  let height = LANE_PAD.top
  for (const n of nodes) {
    if (n.parentId !== lane.id) continue
    width = Math.max(width, n.position.x + (n.measured?.width ?? NODE_W) + LANE_PAD.right)
    height = Math.max(height, n.position.y + (n.measured?.height ?? NODE_H) + LANE_PAD.bottom)
  }
  return { width, height }
}
