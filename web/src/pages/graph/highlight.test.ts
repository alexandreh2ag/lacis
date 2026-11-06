import { Highlight, LIT_CLASS, neighbours } from './highlight'
import type { GEdge } from './query'

const edge = (from: string, to: string): GEdge => ({ id: `${from}→${to}`, from, to } as GEdge)
const edges = [edge('a', 'b'), edge('b', 'c'), edge('d', 'e')]

test('neighbours give each node its edges and the nodes at their other end', () => {
  const n = neighbours(edges)
  expect([...n.get('b')!.nodes].sort()).toEqual(['a', 'b', 'c'])
  expect([...n.get('b')!.edges].sort()).toEqual(['a→b', 'b→c'])
  expect([...n.get('d')!.nodes].sort()).toEqual(['d', 'e'])
})

test('a hover lights its neighbours and fades the rest, telling only the listeners', () => {
  const h = new Highlight()
  h.setGraph(neighbours(edges))
  let calls = 0
  const stop = h.subscribe(() => calls++)
  expect(h.node('a')).toBe('')

  h.hoverNodeOn('b')
  expect([h.node('b'), h.node('a'), h.node('c'), h.node('d')]).toEqual(['hovered', 'related', 'related', ''])
  expect([h.edge('a→b'), h.edge('d→e')]).toEqual(['related', ''])

  h.hoverNodeOn(null)
  h.hoverEdgeOn(edges[2])
  expect([h.node('d'), h.node('e'), h.node('a')]).toEqual(['related', 'related', ''])
  expect([h.edge('d→e'), h.edge('a→b')]).toEqual(['hovered', ''])
  h.hoverEdgeOn(null)
  expect(calls).toBe(4)
  stop()
  h.hoverNodeOn('a')
  expect(calls).toBe(4)
})

test('the impact wins over the hover', () => {
  const h = new Highlight()
  h.setGraph(neighbours(edges))
  h.setImpact({ nodes: new Set(['d', 'e']), edges: new Set(['d→e']) })
  h.hoverNodeOn('a')
  expect([h.node('a'), h.node('d')]).toEqual(['', 'related'])
  h.setImpact(null)
  expect(h.node('a')).toBe('hovered')
})

test('the root is marked while something is lit, the rest fading in CSS', () => {
  const h = new Highlight()
  h.setGraph(neighbours(edges))
  const root = document.createElement('div')
  h.setRoot(root)
  expect(root.classList.contains(LIT_CLASS)).toBe(false)
  h.hoverNodeOn('b')
  expect(root.classList.contains(LIT_CLASS)).toBe(true)
  h.hoverNodeOn(null)
  expect(root.classList.contains(LIT_CLASS)).toBe(false)
})
