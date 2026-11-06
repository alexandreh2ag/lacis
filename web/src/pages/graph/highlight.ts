import { createContext, useContext, useSyncExternalStore } from 'react'
import type { GEdge } from './query'

// Lit are the nodes and edges lit: the impact, else what a hovered node
// or edge reaches.
export interface Lit {
  nodes: Set<string>
  edges: Set<string>
}

// Neighbours give each node the edges touching it and the nodes at their
// other end, computed once per graph so that a hover costs nothing.
export type Neighbours = Map<string, Lit>

export function neighbours(edges: GEdge[]): Neighbours {
  const out: Neighbours = new Map()
  const of = (id: string) => {
    let n = out.get(id)
    if (!n) out.set(id, (n = { nodes: new Set([id]), edges: new Set() }))
    return n
  }
  for (const e of edges) {
    for (const [a, b] of [[e.from, e.to], [e.to, e.from]]) {
      const n = of(a)
      n.edges.add(e.id)
      n.nodes.add(b)
    }
  }
  return out
}

// State is the look of a node or an edge: hovered, lit with the hovered
// one, or plain. What is not lit fades while something is: the class
// LIT_CLASS on the root of the graph does it in CSS.
export type State = 'hovered' | 'related' | ''

// LIT_CLASS marks the root of the graph while something is lit.
export const LIT_CLASS = 'lc-flow--lit'

// Highlight holds the hover and the impact out of the React state of the
// graph: a hover renders again only the cards and edges it lights, the
// others fading through the class of the root, never the whole graph.
export class Highlight {
  private hoverNode: string | null = null
  private hoverEdge: GEdge | null = null
  private impact: Lit | null = null
  private around: Neighbours = new Map()
  private lit: Lit | null = null
  private listeners = new Set<() => void>()
  private root: Element | null = null

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => { this.listeners.delete(fn) }
  }

  // setRoot gives the element holding the graph, marked while something
  // is lit.
  setRoot = (el: Element | null) => {
    this.root = el
    this.root?.classList.toggle(LIT_CLASS, !!this.lit)
  }

  // setGraph gives the neighbours of the graph drawn.
  setGraph(around: Neighbours) {
    this.around = around
    this.update()
  }

  setImpact(impact: Lit | null) {
    this.impact = impact
    this.update()
  }

  hoverNodeOn(id: string | null) {
    this.hoverNode = id
    this.update()
  }

  hoverEdgeOn(edge: GEdge | null) {
    this.hoverEdge = edge
    this.update()
  }

  private update() {
    if (this.impact) this.lit = this.impact
    else if (this.hoverNode) this.lit = this.around.get(this.hoverNode) ?? { nodes: new Set([this.hoverNode]), edges: new Set() }
    else if (this.hoverEdge) this.lit = { nodes: new Set([this.hoverEdge.from, this.hoverEdge.to]), edges: new Set([this.hoverEdge.id]) }
    else this.lit = null
    this.root?.classList.toggle(LIT_CLASS, !!this.lit)
    this.listeners.forEach((fn) => fn())
  }

  node(id: string): State {
    if (!this.lit || !this.lit.nodes.has(id)) return ''
    // The hovered card itself stays as it is.
    return !this.impact && id === this.hoverNode ? 'hovered' : 'related'
  }

  // hoveredEdge is the id of the edge under the pointer, for its tooltip.
  hoveredEdge(): string | null {
    return this.hoverEdge?.id ?? null
  }

  // edge is 'hovered' for the edge under the pointer, which shows its
  // tooltip, impact or not.
  edge(id: string): State {
    if (id === this.hoverEdge?.id) return 'hovered'
    return this.lit?.edges.has(id) ? 'related' : ''
  }
}

// HighlightContext gives the cards and the edges the highlight of their
// graph; out of a graph, nothing is lit.
export const HighlightContext = createContext(new Highlight())

// useNodeState is the look of a node, rendering again only when it changes.
export function useNodeState(id: string): State {
  const h = useContext(HighlightContext)
  return useSyncExternalStore(h.subscribe, () => h.node(id))
}

export function useEdgeState(id: string): State {
  const h = useContext(HighlightContext)
  return useSyncExternalStore(h.subscribe, () => h.edge(id))
}
