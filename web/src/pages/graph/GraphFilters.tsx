import { useQuery } from '@apollo/client'
import { useState } from 'react'
import { Chips } from '@/components/Listing'
import { MultiPicker } from '@/components/MultiPicker'
import { useT } from '@/i18n'
import { HostnamesDoc } from './query'
import type { GraphParams } from './useGraphParams'

// picked tells a value chosen in the list of a datalist, not typed: typing
// web1 on the way to web10 must not add web1.
function picked(e: React.ChangeEvent<HTMLInputElement>): boolean {
  const ev = e.nativeEvent as Event & { inputType?: string }
  return !ev.inputType || ev.inputType === 'insertReplacementText'
}

// GraphFilters is the side panel of the graph: the envs, projects, hosts,
// statuses and links shown.
export function GraphFilters({ ns, p, envs, projects, counts }: {
  ns: string
  p: GraphParams
  envs: string[]
  projects: string[]
  counts: { nodes: number; edges: number } | null
}) {
  const t = useT()
  const hostnames = useQuery(HostnamesDoc, { variables: { ns } }).data?.namespace?.hosts.items ?? []
  const [hostInput, setHostInput] = useState('')
  const addHost = (h: string) => {
    const name = h.trim()
    if (name && !p.hosts.includes(name)) p.set({ host: [...p.hosts, name] })
    setHostInput('')
  }
  return (
    <aside aria-label={t.graph.filters.label} className="flex w-[248px] flex-none flex-col gap-5 overflow-auto border-r border-border bg-surface-raised p-4">
      <section className="flex flex-col gap-2">
        <span className="lc-label">{t.graph.filters.envs}</span>
        <Chips label={t.graph.filters.envs} options={envs.map((e) => ({ value: e, label: e }))} selected={p.envs} onChange={(v) => p.set({ env: v })} />
        <label className={`flex items-center gap-2 text-[13px]${p.envs.length ? '' : ' opacity-50'}`}>
          <input type="checkbox" checked={p.isolate} disabled={!p.envs.length} aria-describedby="graph-isolate-hint"
            onChange={(e) => p.set({ iso: e.target.checked ? '1' : null })} /> {t.graph.filters.isolate}
        </label>
        <span id="graph-isolate-hint" className="lc-muted text-xs leading-4">{p.envs.length ? t.graph.filters.isolateHint : t.graph.filters.isolateOff}</span>
      </section>
      <section className="flex flex-col gap-2">
        <span className="lc-label">{t.graph.filters.projects}</span>
        <MultiPicker label={t.graph.filters.pick} options={projects} selected={p.projects} onChange={(v) => p.set({ project: v })} />
      </section>
      <section className="flex flex-col gap-2">
        <span className="lc-label">{t.graph.filters.hosts}</span>
        <span className="lc-search">
          <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" aria-hidden="true"><circle cx="7" cy="7" r="4.5" /><path d="m10.5 10.5 3 3" /></svg>
          <label htmlFor="graph-host" className="sr-only">{t.graph.filters.byHost}</label>
          <input id="graph-host" className="lc-input" list="graph-hostnames" placeholder={t.graph.filters.byHost} value={hostInput}
            onChange={(e) => {
              setHostInput(e.target.value)
              if (picked(e) && hostnames.some((h) => h.hostname === e.target.value)) addHost(e.target.value)
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
      {counts ? <p className="lc-muted m-0 text-[12.5px] leading-[18px]">{t.graph.filters.counts(counts.nodes, counts.edges)}</p> : null}
    </aside>
  )
}
