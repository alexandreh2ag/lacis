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
// one, faded by it, or plain.
export type State = 'hovered' | 'related' | 'faded' | ''

// Highlight holds the hover and the impact out of the React state of the
// graph: a hover renders again only the cards and edges whose look
// changes, never the whole graph.
export class Highlight {
  private hoverNode: string | null = null
  private hoverEdge: GEdge | null = null
  private impact: Lit | null = null
  private around: Neighbours = new Map()
  private lit: Lit | null = null
  private listeners = new Set<() => void>()

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => { this.listeners.delete(fn) }
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
    this.listeners.forEach((fn) => fn())
  }

  node(id: string): State {
    if (!this.lit) return ''
    if (!this.lit.nodes.has(id)) return 'faded'
    // The hovered card itself stays as it is.
    return !this.impact && id === this.hoverNode ? 'hovered' : 'related'
  }

  // edge is 'hovered' for the edge under the pointer, which shows its
  // tooltip, impact or not.
  edge(id: string): State {
    if (id === this.hoverEdge?.id) return 'hovered'
    if (!this.lit) return ''
    return this.lit.edges.has(id) ? 'related' : 'faded'
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
