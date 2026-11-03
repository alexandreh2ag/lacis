import { useQuery } from '@apollo/client'
import { Background, BackgroundVariant, Controls, ReactFlow, ReactFlowProvider, useReactFlow, type Node, type NodeChange, applyNodeChanges } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { refresh } from '@/config'
import { t as text, useT } from '@/i18n'
import { GraphFilters } from './GraphFilters'
import { GraphToolbar, Legend } from './GraphToolbar'
import { Highlight, HighlightContext, neighbours } from './highlight'
import { ImpactPanel, useImpact } from './Impact'
import { canOpen, title } from './kinds'
import { laneMinimum, separateLanes } from './lanes'
import type { Layout } from './layout'
import { LacisEdge, Markers } from './LacisEdge'
import { BoxNode, NodeCard, type BoxData } from './NodeCard'
import { GraphViewDoc, type GNode } from './query'
import { toFlowEdges, toFlowNodes } from './toFlow'
import { fitOptions, useGraphLayout } from './useGraphLayout'
import { useGraphParams } from './useGraphParams'
import { MoreNode, ZoneNode, type ZoneData } from './Zone'
import { planZones, zoned, zoneOfItem, type Zone } from './zones'

const nodeTypes = { card: NodeCard, box: BoxNode, zone: ZoneNode, more: MoreNode }
const edgeTypes = { link: LacisEdge }

// otherLens is the link of a zone to the same thing in the other lens.
function otherLens(ns: string, n: GNode): ZoneData['other'] {
  if (n.kind === 'PROJECT') {
    if (!n.project) return null
    const q = new URLSearchParams({ lens: 'infra', project: n.project })
    if (n.env) q.set('env', n.env)
    return { label: text().graph.seeIn.infra, to: `/${ns}/graph?${q}` }
  }
  if (!n.project || !n.env) return null
  return { label: text().graph.seeIn.projects, to: `/${ns}/graph?${new URLSearchParams({ lens: 'projects', env: n.env, open: `project:${n.project}@${n.env}` })}` }
}

export function GraphPage() {
  return (
    <ReactFlowProvider>
      <Graph />
    </ReactFlowProvider>
  )
}

function Graph() {
  const { ns = '' } = useParams()
  const p = useGraphParams()
  const flow = useReactFlow()
  const navigate = useNavigate()
  const t = useT()
  const { data, previousData, loading, error } = useQuery(GraphViewDoc, { variables: { ns, level: p.lens.level, filter: p.filter }, ...refresh })
  // The graph of the previous request stays while a zone opens.
  const namespace = (data ?? previousData)?.namespace
  const graph = namespace?.graph
  const [relayout, setRelayout] = useState(0)
  // flowNodes are the nodes as React Flow holds them: their positions once
  // dragged, the boxes grown to keep them, the cards picked together by
  // Ctrl + click or Ctrl + drag. Nothing is saved: a new layout, a refresh
  // or another page brings the computed layout back.
  const [flowNodes, setFlowNodes] = useState<Node[]>([])
  const laidOut = useRef<Layout | null>(null)
  // highlight holds the hover and the impact, read by the cards and edges.
  const [highlight] = useState(() => new Highlight())

  const zones = useMemo(() => (graph ? planZones(graph) : new Map<string, Zone>()), [graph])
  const view = useMemo(() => (graph ? zoned(graph, zones) : null), [graph, zones])
  const zoneOf = useMemo(() => (graph ? zoneOfItem(graph, zones) : (id: string) => id), [graph, zones])
  const byId = useMemo(() => new Map((graph?.nodes ?? []).map((n) => [n.id, n])), [graph])
  const envs = useMemo(() => [...(namespace?.environments ?? [])].sort((a, b) => a.sortOrder - b.sortOrder).map((e) => e.slug), [namespace])

  const layout = useGraphLayout(view, zones, envs, p.viewKey, relayout)
  const placed = layout.placed
  const impact = useImpact(ns, p.lens, p.filter, zoneOf)

  useEffect(() => { highlight.setGraph(neighbours(view?.edges ?? [])) }, [highlight, view])
  useEffect(() => { highlight.setImpact(impact.impact) }, [highlight, impact.impact])

  // One zone opens at a time: opening another closes it.
  const toggle = (id: string) => p.set({ open: p.open === id ? null : id })
  const close = () => p.set({ open: null })
  // Escape closes the zone.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && p.open && !(e.target instanceof HTMLInputElement)) p.set({ open: null })
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [p])

  const nodes = useMemo(() => {
    if (!view || !placed) return []
    return toFlowNodes(view, placed, zones, { ns, level: p.lens.level, impact: impact.impact, other: (n) => otherLens(ns, n), onClose: toggle })
  // toggle follows p, whose open zone stands for it.
  }, [view, placed, zones, impact.impact, ns, p.open, p.lens.level])

  // The data of the nodes follows the page; what the user moved stays,
  // until a new layout.
  useEffect(() => {
    const fresh = laidOut.current !== placed
    laidOut.current = placed
    setFlowNodes((prev) => {
      if (fresh) return nodes
      const held = new Map(prev.map((n) => [n.id, n]))
      return nodes.map((n) => {
        const h = held.get(n.id)
        return h ? { ...n, position: h.position, width: h.width, height: h.height, measured: h.measured, selected: h.selected } : n
      })
    })
  }, [nodes, placed])

  // onNodesChange keeps the drags, the measures and the selection; a lane
  // grown or resized over the next one pushes it down.
  const onNodesChange = (changes: NodeChange[]) => setFlowNodes((ns) => separateLanes(applyNodeChanges(changes, ns)))
  // shown gives each lane the smallest size holding its cards.
  const shown = useMemo(() => flowNodes.map((n) => {
    if (n.type !== 'box' || (n.data as BoxData).kind !== 'lane') return n
    const min = laneMinimum(n, flowNodes)
    return { ...n, data: { ...n.data, minWidth: min.width, minHeight: min.height } }
  }), [flowNodes])

  const edges = useMemo(() => (view && placed ? toFlowEdges(view, placed, nodes, byId, impact.impact) : []), [view, placed, nodes, byId, impact.impact])
  const edgeOf = useMemo(() => new Map((view?.edges ?? []).map((e) => [e.id, e])), [view])

  const label = (id: string) => (byId.get(id) ? title(byId.get(id)!) : id.startsWith('more:') ? t.graph.otherItems : id)
  const opened = p.open ? { id: p.open, label: byId.get(p.open) ? title(byId.get(p.open)!) : p.open } : null
  const failed = error?.message ?? null
  return (
    <div className="flex min-h-0 flex-1">
      <GraphFilters ns={ns} p={p} envs={envs} projects={(namespace?.projects ?? []).map((x) => x.slug)}
        counts={graph ? { nodes: graph.nodes.length, edges: graph.edges.length } : null} />

      <div ref={layout.pane} className="relative min-w-0 flex-1">
        <Markers />
        <GraphToolbar ns={ns} p={p} opened={opened} impact={impact} onClose={close} onFit={() => void flow.fitView(fitOptions)}
          onRelayout={() => setRelayout((x) => x + 1)} />
        {failed ? <div className="absolute inset-0 grid place-items-center" role="alert">{t.graph.loadError(failed)}</div> : null}
        {layout.error ? <div className="absolute inset-0 grid place-items-center" role="alert">{t.graph.layoutError(layout.error)}</div> : null}
        {!loading && graph && graph.nodes.length === 0 ? (
          <div className="absolute inset-0 z-10 grid place-items-center">
            <div className="lc-panel w-[380px]"><div className="lc-panel__section">
              <span className="font-semibold">{t.graph.empty.title}</span>
              <span className="lc-muted">{t.graph.empty.text}</span>
            </div></div>
          </div>
        ) : null}
        <HighlightContext.Provider value={highlight}>
          <ReactFlow
            className="lc-flow"
            nodes={shown}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            onNodesChange={onNodesChange}
            onlyRenderVisibleElements
            nodesConnectable={false}
            selectionKeyCode={['Control', 'Meta']}
            multiSelectionKeyCode={['Control', 'Meta']}
            edgesFocusable={false}
            minZoom={0.1}
            proOptions={{ hideAttribution: true }}
            onNodeClick={(e, n) => {
              if (n.type === 'box' || e.ctrlKey || e.metaKey) return
              // The content of the opened zone stands for the zone.
              const id = n.parentId && n.parentId === p.open ? p.open : n.id
              if (impact.on) {
                impact.setTarget(id)
                return
              }
              const g = byId.get(n.id)
              // An app of the opened zone opens its page.
              if (n.parentId === p.open && g?.entity?.__typename === 'App') {
                navigate(`/${ns}/apps/${g.entity.id}`)
                return
              }
              if (n.type === 'card' && g && canOpen(g)) toggle(n.id)
              else if (p.open && id !== p.open) close()
            }}
            onPaneClick={() => { if (p.open && !impact.on) close() }}
            onNodeMouseEnter={(_, n) => { if (n.type === 'card' || n.type === 'more') highlight.hoverNodeOn(n.id) }}
            onNodeMouseLeave={() => highlight.hoverNodeOn(null)}
            onEdgeMouseEnter={(_, e) => highlight.hoverEdgeOn(edgeOf.get(e.id) ?? null)}
            onEdgeMouseLeave={() => highlight.hoverEdgeOn(null)}
          >
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="var(--border-strong)" />
            <Controls showInteractive={false} position="bottom-right" />
          </ReactFlow>
        </HighlightContext.Provider>
        <Legend />
        {impact.on ? (
          <ImpactPanel target={impact.target} label={label} impact={impact.impact} nodes={byId} direction={impact.direction}
            error={impact.error?.message} onClose={impact.quit} />
        ) : null}
      </div>
    </div>
  )
}
