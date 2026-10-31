import { useQuery } from '@apollo/client'
import { Background, BackgroundVariant, Controls, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node, type NodeChange, applyNodeChanges } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { Chips } from '@/components/Listing'
import { MultiPicker } from '@/components/MultiPicker'
import type { HostStatus, LinkConfidence } from '@/graphql/graphql'
import { t as text, useT } from '@/i18n'
import { canOpen, isStack, title } from './kinds'
import { laneMinimum, separateLanes } from './lanes'
import { layout, spacingFor, type Layout, type Spacing } from './layout'
import { LacisEdge, Markers, type LinkData } from './LacisEdge'
import { BoxNode, NodeCard, type BoxData, type CardData } from './NodeCard'
import { GraphViewDoc, HostnamesDoc, ImpactDoc, type GNode } from './query'
import { MoreNode, pageOf, ZoneNode, type MoreData, type ZoneData } from './Zone'
import { cardPlace, planZones, zoned, zoneOfItem, type Zone } from './zones'

const nodeTypes = { card: NodeCard, box: BoxNode, zone: ZoneNode, more: MoreNode }
// fitOptions keep the graph clear of the toolbar and the legend.
const fitOptions = { padding: { top: '72px', bottom: '84px', left: '24px', right: '24px' } as const, duration: 200 }
const edgeTypes = { link: LacisEdge }

// A lens is a way to read the namespace: its projects, to understand
// and document it, or its hosts, for incidents and changes. Both open
// on the same content: apps, services, webservers and runtimes.
const lenses = [
  { key: 'projects', level: 'PROJECT' },
  { key: 'infra', level: 'HOST' },
] as const

type ImpactDirection = 'DEPENDENTS' | 'NEEDS'

// useGraphParams keeps the lens, the opened zone and the filters of the
// graph in the URL.
function useGraphParams() {
  const [params, setParams] = useSearchParams()
  // level=host is the address of the graph before the lenses.
  const lens = lenses.find((l) => l.key === params.get('lens')) ?? (params.get('level') === 'host' ? lenses[1] : lenses[0])
  const statuses = params.getAll('status') as HostStatus[]
  return {
    lens,
    // open is the zone opened: one at a time.
    open: params.get('open'),
    envs: params.getAll('env'),
    projects: params.getAll('project'),
    hosts: params.getAll('host'),
    statuses: statuses.length ? statuses : (['ACTIVE', 'STALE'] as HostStatus[]),
    confidences: params.getAll('conf') as LinkConfidence[],
    externals: params.get('ext') !== '0',
    set(changes: Record<string, string | string[] | null>) {
      setParams((prev) => {
        const next = new URLSearchParams(prev)
        next.delete('level')
        for (const [k, v] of Object.entries(changes)) {
          next.delete(k)
          if (Array.isArray(v)) v.forEach((x) => next.append(k, x))
          else if (v !== null) next.set(k, v)
        }
        return next
      })
    },
  }
}

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
  const filter = { envs: p.envs, projects: p.projects, hosts: p.hosts, status: p.statuses, confidences: p.confidences, externals: p.externals,
    expand: p.open ? [p.open] : [] }
  const { data, previousData, loading, error } = useQuery(GraphViewDoc, { variables: { ns, level: p.lens.level, filter } })
  const hostnames = useQuery(HostnamesDoc, { variables: { ns } }).data?.namespace?.hosts.items ?? []
  // The graph of the previous request stays while a zone opens.
  const namespace = (data ?? previousData)?.namespace
  const graph = namespace?.graph
  const [placed, setPlaced] = useState<Layout | null>(null)
  const [relayout, setRelayout] = useState(0)
  const [hoverNode, setHoverNode] = useState<string | null>(null)
  const [hoverEdge, setHoverEdge] = useState<string | null>(null)
  const [hostInput, setHostInput] = useState('')
  // The impact mode: a click picks the node whose impact shows, instead of
  // opening it.
  const [impactOn, setImpactOn] = useState(false)
  const [impactTarget, setImpactTarget] = useState<string | null>(null)
  const [impactDir, setImpactDir] = useState<ImpactDirection>('DEPENDENTS')
  const impactQuery = useQuery(ImpactDoc, {
    skip: !impactOn || !impactTarget,
    variables: { ns, level: p.lens.level, filter, node: impactTarget ?? '', direction: impactDir },
  })
  // flowNodes are the nodes as React Flow holds them: their positions once
  // dragged, the boxes grown to keep them, the cards picked together by
  // Ctrl + click or Ctrl + drag. Nothing is saved: a new layout, a refresh
  // or another page brings the computed layout back.
  const [flowNodes, setFlowNodes] = useState<Node[]>([])
  const laidOut = useRef<Layout | null>(null)
  // fitted is the view the graph was last fitted to: opening a zone keeps
  // the viewport, another lens or filter fits it again.
  const fitted = useRef('')

  const zones = useMemo(() => (graph ? planZones(graph) : new Map<string, Zone>()), [graph])
  const view = useMemo(() => (graph ? zoned(graph, zones) : null), [graph, zones])
  const zoneOf = useMemo(() => (graph ? zoneOfItem(graph, zones) : (id: string) => id), [graph, zones])
  const byId = useMemo(() => new Map((graph?.nodes ?? []).map((n) => [n.id, n])), [graph])

  const envOrder = useMemo(() => [...(namespace?.environments ?? [])].sort((a, b) => a.sortOrder - b.sortOrder).map((e) => e.slug), [namespace])
  // spacing follows the width of the graph: a large screen spreads the
  // cards instead of leaving its sides empty. It changes past a few
  // pixels only, each change laying the graph out again.
  const pane = useRef<HTMLDivElement>(null)
  const [spacing, setSpacing] = useState<Spacing>(() => spacingFor(window.innerWidth - 248, window.innerHeight - 56))
  useEffect(() => {
    const el = pane.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      const next = spacingFor(entry.contentRect.width, entry.contentRect.height)
      setSpacing((cur) => (Math.abs(cur.between - next.between) < 16 && cur.aspect === next.aspect ? cur : next))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  const viewKey = JSON.stringify([p.lens.key, p.envs, p.projects, p.hosts, p.statuses, p.confidences, p.externals, relayout, spacing])
  useEffect(() => {
    if (!view) return
    let live = true
    const sizes = new Map([...zones.values()].map((z) => [z.id, { width: z.width, height: z.height }]))
    void layout(view, envOrder, sizes, spacing).then((l) => {
      if (!live) return
      setPlaced(l)
      if (fitted.current !== viewKey) {
        fitted.current = viewKey
        requestAnimationFrame(() => void flow.fitView(fitOptions))
      }
    })
    return () => { live = false }
  }, [view, zones, envOrder, viewKey, spacing, flow])

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

  // impact is what depends on the target, or what it needs, as the
  // server walks it; the content of the opened zone counts as the zone.
  const impactData = impactQuery.data ?? impactQuery.previousData
  const impact = useMemo(() => {
    const got = impactData?.namespace?.impact
    if (!impactOn || !impactTarget || !got) return null
    const levels = new Map<string, number>()
    for (const n of got.nodes) {
      const id = zoneOf(n.id)
      const known = levels.get(id)
      if (id !== impactTarget && (known === undefined || n.level < known)) levels.set(id, n.level)
    }
    const edges = new Set(got.edges.map((e) => {
      const [from, to] = e.split('→')
      return `${zoneOf(from)}→${zoneOf(to)}`
    }))
    return { target: [impactTarget], levels, edges }
  }, [impactOn, impactTarget, impactData, zoneOf])

  // related are the nodes and edges lit: the impact, else what a hovered
  // node or edge reaches.
  const related = useMemo(() => {
    const nodes = new Set<string>()
    const edges = new Set<string>()
    if (!view) return null
    if (impact) return { nodes: new Set([...impact.target, ...impact.levels.keys()]), edges: impact.edges }
    if (hoverNode) {
      nodes.add(hoverNode)
      for (const e of view.edges) {
        if (e.from === hoverNode || e.to === hoverNode) { edges.add(e.id); nodes.add(e.from); nodes.add(e.to) }
      }
    } else if (hoverEdge) {
      const e = view.edges.find((x) => x.id === hoverEdge)
      if (e) { edges.add(e.id); nodes.add(e.from); nodes.add(e.to) }
    } else {
      return null
    }
    return { nodes, edges }
  }, [view, impact, hoverNode, hoverEdge])

  const nodes: Node[] = useMemo(() => {
    if (!view || !placed) return []
    const card = (n: GNode): CardData => ({ node: n, selected: n.id === impactTarget && !!impact, related: !!related?.nodes.has(n.id) && n.id !== hoverNode,
      faded: !!related && !related.nodes.has(n.id), impact: impact?.levels.get(n.id),
      copy: p.lens.level === 'HOST' && n.kind === 'HOST' && !isStack(n) ? n.hostname ?? n.label : undefined })
    const out: Node[] = placed.boxes.map((b, i) => ({
      id: b.id, type: 'box', position: { x: b.x, y: b.y }, data: { label: b.label, meta: b.meta, kind: b.kind, order: i } satisfies BoxData,
      width: b.width, height: b.height, selectable: false, draggable: false, zIndex: -1,
    }))
    const content: Node[] = []
    for (const n of view.nodes) {
      const at = placed.nodes.get(n.id)
      if (!at) continue
      const zone = zones.get(n.id)
      if (!zone) {
        out.push({ id: n.id, type: 'card', position: { x: at.x, y: at.y }, parentId: at.parent, expandParent: !!at.parent, data: card(n) })
        continue
      }
      out.push({
        id: n.id, type: 'zone', position: { x: at.x, y: at.y }, parentId: at.parent, expandParent: !!at.parent, width: zone.width, height: zone.height,
        data: { zone, ns, other: otherLens(ns, n), onClose: () => toggle(n.id) } satisfies ZoneData,
      })
      for (const row of zone.rows) {
        row.shown.forEach((item, i) => {
          content.push({ id: item.id, type: 'card', position: cardPlace(row, i), parentId: zone.id, draggable: false, data: card(item) })
        })
        if (row.more) {
          const what = row.key === 'apps' ? 'apps' : row.key === 'runtimes' ? 'runtimes' : ''
          content.push({
            id: row.moreId, type: 'more', position: cardPlace(row, row.shown.length), parentId: zone.id, draggable: false, selectable: false,
            data: { label: t.graph.more(row.more, what), to: pageOf(ns, n, row.key) } satisfies MoreData,
          })
        }
      }
    }
    return [...out, ...content]
  // toggle follows p, a new object at every render: the opened zones
  // stand for it.
  }, [view, placed, zones, related, impact, impactTarget, hoverNode, ns, p.open, p.lens.level])

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

  const edges: Edge[] = useMemo(() => {
    if (!view || !placed) return []
    const drawn = new Set(nodes.map((n) => n.id))
    const label = (id: string) => (byId.get(id) ? title(byId.get(id)!) : t.graph.others)
    // A source the layout did not place, inside a zone, takes the color
    // of its zone.
    const color = (id: string) => placed.colors.get(id) ?? placed.colors.get(nodes.find((n) => n.id === id)?.parentId ?? '') ?? (hash(id) % 8) + 1
    return view.edges.filter((e) => drawn.has(e.from) && drawn.has(e.to)).map((e) => ({
      id: e.id, source: e.from, target: e.to, type: 'link',
      data: {
        edge: e, color: color(e.from), fromLabel: label(e.from), toLabel: label(e.to),
        related: !!related?.edges.has(e.id), faded: !!related && !related.edges.has(e.id), hovered: e.id === hoverEdge, impact: !!impact?.edges.has(e.id),
      } satisfies LinkData,
    }))
  }, [view, placed, nodes, byId, related, impact, hoverEdge])

  const addHost = (h: string) => {
    const name = h.trim()
    if (name && !p.hosts.includes(name)) p.set({ host: [...p.hosts, name] })
    setHostInput('')
  }

  const envs = [...(namespace?.environments ?? [])].sort((a, b) => a.sortOrder - b.sortOrder)
  const crumbs = [t.graph.lenses[p.lens.key], ...p.projects, ...p.envs, ...p.hosts]
  const opened = p.open ? { id: p.open, label: byId.get(p.open) ? title(byId.get(p.open)!) : p.open } : null
  return (
    <div className="flex min-h-0 flex-1">
      <aside aria-label={t.graph.filters.label} className="flex w-[248px] flex-none flex-col gap-5 overflow-auto border-r border-border bg-surface-raised p-4">
        <section className="flex flex-col gap-2">
          <span className="lc-label">{t.graph.filters.envs}</span>
          <Chips label={t.graph.filters.envs} options={envs.map((e) => ({ value: e.slug, label: e.slug }))} selected={p.envs} onChange={(v) => p.set({ env: v })} />
        </section>
        <section className="flex flex-col gap-2">
          <span className="lc-label">{t.graph.filters.projects}</span>
          <MultiPicker label={t.graph.filters.pick} options={(namespace?.projects ?? []).map((x) => x.slug)} selected={p.projects} onChange={(v) => p.set({ project: v })} />
        </section>
        <section className="flex flex-col gap-2">
          <span className="lc-label">{t.graph.filters.hosts}</span>
          <span className="lc-search">
            <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" aria-hidden="true"><circle cx="7" cy="7" r="4.5" /><path d="m10.5 10.5 3 3" /></svg>
            <label htmlFor="graph-host" className="sr-only">{t.graph.filters.byHost}</label>
            <input id="graph-host" className="lc-input" list="graph-hostnames" placeholder={t.graph.filters.byHost} value={hostInput}
              onChange={(e) => {
                setHostInput(e.target.value)
                if (hostnames.some((h) => h.hostname === e.target.value)) addHost(e.target.value)
              }}
              onKeyDown={(e) => { if (e.key === 'Enter') addHost(hostInput) }} />
          </span>
          <datalist id="graph-hostnames">{hostnames.map((h) => <option key={h.id} value={h.hostname} />)}</datalist>
          {p.hosts.length ? (
            <div className="flex flex-wrap gap-1.5">
              {p.hosts.map((h) => (
                <button key={h} className="lc-chip font-mono" type="button" aria-pressed="true" aria-label={t.graph.filters.remove(h)}
                  onClick={() => p.set({ host: p.hosts.filter((x) => x !== h) })}>{h} <span aria-hidden="true">×</span></button>
              ))}
            </div>
          ) : null}
          <span className="lc-muted text-xs leading-4">{t.graph.filters.hostsHint}</span>
        </section>
        <section className="flex flex-col gap-2">
          <span className="lc-label">{t.graph.filters.status}</span>
          <Chips label={t.graph.filters.statusChips} options={(['ACTIVE', 'STALE', 'INACTIVE'] as const).map((s) => ({ value: s, label: t.graph.filters.statuses[s] }))}
            selected={p.statuses} onChange={(v) => p.set({ status: v.length ? v : ['ACTIVE', 'STALE', 'INACTIVE'] })} />
        </section>
        <section className="flex flex-col gap-2">
          <span className="lc-label">{t.graph.filters.links}</span>
          <Chips label={t.graph.filters.confidence} options={(['EXACT', 'INFERRED', 'AMBIGUOUS'] as const).map((c) => ({ value: c, label: t.graph.filters.confidences[c] }))}
            selected={p.confidences} onChange={(v) => p.set({ conf: v })} />
          <label className="flex items-center gap-2 text-[13px]">
            <input type="checkbox" checked={p.externals} onChange={(e) => p.set({ ext: e.target.checked ? null : '0' })} /> {t.graph.filters.externals}
          </label>
        </section>
        <span className="flex-1" />
        <p className="lc-muted m-0 text-[12px] leading-[17px]">
          {t.graph.filters.help}
        </p>
        {graph ? <p className="lc-muted m-0 text-[12.5px] leading-[18px]">{t.graph.filters.counts(graph.nodes.length, graph.edges.length)}</p> : null}
      </aside>

      <div ref={pane} className="relative min-w-0 flex-1">
        <Markers />
        <div className="absolute inset-x-4 top-3 z-10 flex flex-wrap items-center gap-3">
          <nav className="lc-crumbs rounded-md border border-border bg-surface-raised px-2 py-1" aria-label={t.graph.toolbar.crumbs}>
            <Link to={`/${ns}`}>{ns}</Link>
            {crumbs.map((c, i) => (
              <span key={i} className="contents"><span className="lc-crumbs__sep">/</span><span aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</span></span>
            ))}
          </nav>
          {opened ? (
            <div className="flex items-center gap-1.5 rounded-md border border-border bg-surface-raised px-2 py-1" role="group" aria-label={t.graph.toolbar.openedZone}>
              <span className="text-[12.5px] text-ink-muted">{t.graph.toolbar.opened}</span>
              <button className="lc-chip" type="button" aria-pressed="true" aria-label={t.graph.toolbar.close(opened.label)} onClick={close}>
                {opened.label} <span aria-hidden="true">×</span>
              </button>
            </div>
          ) : null}
          <span className="flex-1" />
          {impactOn ? (
            <div className="lc-zoom" role="group" aria-label={t.graph.toolbar.impactDirection}>
              <button type="button" aria-pressed={impactDir === 'DEPENDENTS'} onClick={() => setImpactDir('DEPENDENTS')}>{t.graph.toolbar.dependents}</button>
              <button type="button" aria-pressed={impactDir === 'NEEDS'} onClick={() => setImpactDir('NEEDS')}>{t.graph.toolbar.needs}</button>
            </div>
          ) : null}
          <button className={'lc-btn' + (impactOn ? ' lc-btn--primary' : '')} type="button" aria-pressed={impactOn}
            onClick={() => {
              // The impact reads the whole graph: the opened zone closes.
              if (!impactOn && p.open) close()
              setImpactOn(!impactOn)
              setImpactTarget(null)
            }}>{t.graph.toolbar.impact}</button>
          <div className="lc-zoom" role="group" aria-label={t.graph.toolbar.lens}>
            {lenses.map((l) => (
              <button key={l.key} type="button" aria-pressed={l.key === p.lens.key} onClick={() => p.set({ lens: l.key, open: null })}>{t.graph.lenses[l.key]}</button>
            ))}
          </div>
          <button className="lc-btn" type="button" onClick={() => void flow.fitView(fitOptions)}>{t.graph.toolbar.fit}</button>
          <button className="lc-btn" type="button" title={t.graph.toolbar.relayoutTitle}
            onClick={() => setRelayout((x) => x + 1)}>{t.graph.toolbar.relayout}</button>
        </div>
        {error ? <div className="absolute inset-0 grid place-items-center" role="alert">{t.graph.loadError(error.message)}</div> : null}
        {!loading && graph && graph.nodes.length === 0 ? (
          <div className="absolute inset-0 z-10 grid place-items-center">
            <div className="lc-panel w-[380px]"><div className="lc-panel__section">
              <span className="font-semibold">{t.graph.empty.title}</span>
              <span className="lc-muted">{t.graph.empty.text}</span>
            </div></div>
          </div>
        ) : null}
        <ReactFlow
          className="lc-flow"
          nodes={shown}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
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
            if (impactOn) {
              setImpactTarget(id)
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
          onPaneClick={() => { if (p.open && !impactOn) close() }}
          onNodeMouseEnter={(_, n) => (n.type === 'card' || n.type === 'more') && setHoverNode(n.id)}
          onNodeMouseLeave={() => setHoverNode(null)}
          onEdgeMouseEnter={(_, e) => setHoverEdge(e.id)}
          onEdgeMouseLeave={() => setHoverEdge(null)}
        >
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="var(--border-strong)" />
          <Controls showInteractive={false} position="bottom-right" />
        </ReactFlow>
        <Legend />
        {impactOn ? (
          <ImpactPanel target={impactTarget} label={(id) => byId.get(id) ? title(byId.get(id)!) : id.startsWith('more:') ? t.graph.otherItems : id}
            impact={impact} nodes={byId} direction={impactDir} onClose={() => { setImpactOn(false); setImpactTarget(null) }} />
        ) : null}
      </div>
    </div>
  )
}

function hash(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

function Legend() {
  const t = useT()
  const l = t.graph.legend
  const line = (label: string, dash?: string, halo?: boolean) => (
    <span className="lc-edge text-xs">
      <svg viewBox="0 0 56 14" aria-hidden="true" style={{ width: 36 }}>
        {halo ? <line className="halo" x1="0" y1="7" x2="48" y2="7" /> : null}
        <line x1="0" y1="7" x2="48" y2="7" style={{ strokeDasharray: dash }} /><polygon points="48,3 56,7 48,11" />
      </svg>{label}
    </span>
  )
  const dot = (v: string, label: string, dashed = false) => (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span className="size-2.5 rounded-[3px]" style={{ border: `1.5px ${dashed ? 'dashed' : 'solid'} var(${v})`, background: dashed ? 'transparent' : `var(${v}-tint)` }} />{label}
    </span>
  )
  return (
    <div className="absolute bottom-4 left-4 z-10 flex max-w-[calc(100%-120px)] flex-wrap items-center gap-x-5 gap-y-2 rounded-md border border-border bg-surface-raised px-3.5 py-2.5 shadow-panel">
      {dot('--kind-app', l.app)}{dot('--kind-service', l.service)}{dot('--kind-proxy', l.proxy)}{dot('--kind-host', l.host)}{dot('--kind-external', l.external, true)}
      <span className="h-[18px] w-px bg-border" />
      <span className="inline-flex items-center gap-1.5 text-xs">
        <span className="inline-flex gap-0.5">{Array.from({ length: 8 }, (_, i) => <span key={i} className="h-1 w-3.5 rounded-sm" style={{ background: `var(--link-${i + 1})` }} />)}</span>
        {l.color}
      </span>
      {line(l.exact)}{line(l.inferred, '6 4')}{line(l.ambiguous, '1 4')}{line(l.crossEnv, undefined, true)}
    </div>
  )
}

// ImpactPanel lists what the impact reaches, level by level, and the
// projects it touches.
function ImpactPanel({ target, impact, nodes, label, direction, onClose }: {
  target: string | null
  impact: { levels: Map<string, number> } | null
  nodes: Map<string, GNode>
  label: (id: string) => string
  direction: ImpactDirection
  onClose: () => void
}) {
  const t = useT()
  const i = t.graph.impact
  const byLevel = new Map<number, string[]>()
  for (const [id, level] of impact?.levels ?? []) byLevel.set(level, [...(byLevel.get(level) ?? []), id])
  const projects = [...new Set([...(impact?.levels.keys() ?? [])].map((id) => nodes.get(id)?.project).filter((x): x is string => !!x))].sort()
  return (
    <aside className="lc-panel absolute right-4 top-16 z-10 max-h-[calc(100%-180px)] w-[320px] overflow-auto" aria-label={i.title}>
      <div className="lc-panel__head">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="lc-panel__title text-[17px]">{i.title}</span>
          <span className="lc-muted text-[12.5px]">
            {target ? <>{direction === 'DEPENDENTS' ? i.dependentsOf : i.needsOf}<b className="font-mono">{label(target)}</b></> : i.pick}
          </span>
        </div>
        <button className="lc-btn lc-btn--icon lc-btn--ghost" type="button" aria-label={i.quit} onClick={onClose}>×</button>
      </div>
      {target && impact && impact.levels.size === 0 ? (
        <div className="lc-panel__section"><span className="lc-muted">{direction === 'DEPENDENTS' ? i.noDependents : i.noNeeds}</span></div>
      ) : null}
      {[...byLevel].map(([level, ids]) => (
        <div key={level} className="lc-panel__section">
          <span className="lc-label">{i.level(level)} · {ids.length}</span>
          {ids.map((id) => <span key={id} className="truncate font-mono text-[12.5px]">{label(id)}</span>)}
        </div>
      ))}
      {projects.length ? (
        <div className="lc-panel__section">
          <span className="lc-label">{i.projects} · {projects.length}</span>
          <span className="text-[13px]">{projects.join(', ')}</span>
        </div>
      ) : null}
    </aside>
  )
}
