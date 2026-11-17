import { useQuery } from '@apollo/client'
import { useId, useState } from 'react'
import { Link } from 'react-router'
import { Ago } from '@/components/Ago'
import { Dialog } from '@/components/Dialog'
import { chipsField, FilterBar, type FilterField } from '@/components/FilterBar'
import { Eye } from '@/components/Icon'
import { Pager, SearchInput, SortHeader, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AuditEventsQuery, AuditSortField, AuditSource } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { formatDateTime, formatFull } from '@/lib/format'

const AuditEventsDoc = graphql(`
  query AuditEvents($filter: AuditFilter, $sort: AuditSort, $page: PageInput) {
    auditEvents(filter: $filter, sort: $sort, page: $page) {
      total
      items { id at username source action namespace project target details }
    }
  }
`)

const AuditUsersDoc = graphql(`
  query AuditUsers($search: String) { auditUsers(search: $search) }
`)

const AuditActionsDoc = graphql(`
  query AuditActions($search: String) { auditActions(search: $search) }
`)

type Event = AuditEventsQuery['auditEvents']['items'][number]

const sources: AuditSource[] = ['MCP', 'UI', 'AUTH']

// eventSort reads the sort of the URL: the date, newest first, by default;
// desc=0 or desc=1 when the user chose the direction.
export function eventSort(sort: string, desc: string | null): { by: AuditSortField; desc: boolean } {
  const by = (['AT', 'USER', 'SOURCE', 'ACTION', 'NAMESPACE', 'TARGET'].includes(sort) ? sort : 'AT') as AuditSortField
  return { by, desc: desc === null ? by === 'AT' : desc === '1' }
}

// dayBounds turns the days of the filter (YYYY-MM-DD, local time) into the
// instants it covers: from the start of from to the end of to.
export function dayBounds(from: string, to: string): { from?: string; to?: string } {
  const start = (day: string) => {
    const [y, m, d] = day.split('-').map(Number)
    return new Date(y, m - 1, d)
  }
  const out: { from?: string; to?: string } = {}
  if (from) out.from = start(from).toISOString()
  if (to) {
    const end = start(to)
    end.setDate(end.getDate() + 1)
    out.to = end.toISOString()
  }
  return out
}

// detailsText writes the arguments of an event on one line, an object
// flattened (input.username), the empty ones and the keys that the other
// columns tell left out.
export function detailsText(details: unknown, shown: string[] = []): string {
  const parts: string[] = []
  const walk = (value: unknown, prefix: string) => {
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      const key = prefix + k
      if (shown.includes(key) || v === null || v === '' || (Array.isArray(v) && !v.length)) continue
      if (typeof v === 'object' && !Array.isArray(v)) walk(v, key + '.')
      else parts.push(`${key}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
    }
  }
  if (details && typeof details === 'object' && !Array.isArray(details)) walk(details, '')
  return parts.join(' · ')
}

// Events lists what the users did, newest first: their MCP calls, their
// actions in the front, their sign-ins. The filters live in the URL.
export function Events() {
  const t = useT()
  const e = t.admin.events
  const l = useListParams({ sort: 'AT', size: 50 })
  const sort = eventSort(l.sort, l.get('desc'))
  // A column clicked again turns; another one starts ascending, the date newest first.
  const onSort = (by: string) => l.set(by === sort.by ? { desc: sort.desc ? '0' : '1' } : { sort: by, desc: by === 'AT' ? '1' : '0' })
  const head = (label: string, by: AuditSortField) => <SortHeader label={label} by={by} sort={sort.by} desc={sort.desc} onSort={onSort} />
  const from = l.get('from') ?? ''
  const to = l.get('to') ?? ''
  const user = l.get('user') ?? ''
  const action = l.get('action') ?? ''
  const picked = l.list('source') as AuditSource[]
  const { data, error, loading } = useQuery(AuditEventsDoc, {
    variables: {
      filter: { ...dayBounds(from, to), user: user || null, action: action || null, sources: picked.length ? picked : null, search: l.q || null },
      sort,
      page: { offset: (l.page - 1) * l.size, limit: l.size },
    },
  })
  const page = data?.auditEvents
  const [shown, setShown] = useState<Event | null>(null)
  const period: FilterField = {
    label: e.period, value: from || to ? e.fromTo(from, to) : null, clear: () => l.set({ from: null, to: null }),
    control: <PeriodControl from={from} to={to} onChange={(f, t2) => l.set({ from: f, to: t2 })} />,
  }
  const who: FilterField = {
    label: e.who, value: user || null, clear: () => l.set({ user: null }),
    control: <Suggest label={e.who} placeholder={e.whoPlaceholder} value={user} onChange={(u) => l.set({ user: u })} of="users" />,
  }
  const actionField: FilterField = {
    label: e.action, value: action || null, clear: () => l.set({ action: null }),
    control: <Suggest label={e.action} placeholder={e.actionPlaceholder} value={action} onChange={(a) => l.set({ action: a })} of="actions" />,
  }
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">{e.intro}</p>
      <FilterBar
        search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder={e.search} width={320} />}
        fields={[period, who, actionField, chipsField({ label: e.type, options: sources.map((s) => ({ value: s, label: e.sources[s] })),
          selected: picked, onChange: (v) => l.set({ source: v }) })]}
        onClearAll={() => l.set({ from: null, to: null, user: null, action: null, source: null })}
        count={page ? e.count(page.total) : null} />
      {error ? <p role="alert">{e.loadError}{error.message}</p> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(e.at, 'AT')}{head(e.who, 'USER')}{head(e.source, 'SOURCE')}{head(e.action, 'ACTION')}{head(e.where, 'NAMESPACE')}{head(e.target, 'TARGET')}
            <th title={e.detailsHint}>{e.details}</th><th><span className="sr-only">{t.admin.actions}</span></th>
          </tr></thead>
          <tbody>
            {page?.items.map((x) => <EventRow key={x.id} event={x} onShow={() => setShown(x)} />)}
            {!loading && page && !page.items.length ? <tr><td colSpan={8} className="text-ink-muted">{e.none}</td></tr> : null}
          </tbody>
        </table>
      </div>
      {shown ? <EventDialog event={shown} onClose={() => setShown(null)} /> : null}
      {page ? <Pager page={l.page} size={l.size} total={page.total} noun={e.noun} onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} /> : null}
    </>
  )
}

// sourceBadge is the class of the badge of a source.
function sourceBadge(source: string): string {
  return source === 'MCP' ? 'lc-badge lc-badge--inactive' : source === 'AUTH' ? 'lc-badge' : 'lc-badge lc-badge--stale'
}

function EventRow({ event: x, onShow }: { event: Event; onShow: () => void }) {
  const e = useT().admin.events
  const details = x.details as Record<string, unknown> | null
  const method = x.action === 'login' && typeof details?.method === 'string' ? e.methods[details.method] ?? details.method : null
  const text = method ?? detailsText(details, ['namespace', 'method'])
  return (
    <tr>
      <td className="whitespace-nowrap"><Ago at={x.at}>{formatDateTime(x.at)}</Ago></td>
      <td className="font-semibold">{x.username}</td>
      <td><span className={sourceBadge(x.source)}>{e.sources[x.source]}</span></td>
      <td>{e.actions[x.action] ? <span title={x.action}>{e.actions[x.action]}</span> : <span className="lc-mono">{x.action}</span>}</td>
      <td>{x.namespace ? (
        <span>
          <Link to={`/${x.namespace}`}>{x.namespace}</Link>
          {x.project ? <> / <Link to={`/${x.namespace}/projects/${x.project}`}>{x.project}</Link></> : null}
        </span>
      ) : x.project ? <span>{x.project}</span> : <span className="lc-muted">—</span>}</td>
      <td>{x.target ? <span className="lc-mono">{x.target}</span> : <span className="lc-muted">—</span>}</td>
      <td className="lc-mono max-w-[16rem] truncate text-[12px] text-ink-muted" title={text}>{text}</td>
      <td className="lc-num">
        <button type="button" className="lc-btn lc-btn--icon lc-btn--small" aria-label={e.show} title={e.show} onClick={onShow}><Eye /></button>
      </td>
    </tr>
  )
}

// EventDialog shows the whole of an event, its parameters as sent.
function EventDialog({ event: x, onClose }: { event: Event; onClose: () => void }) {
  const t = useT()
  const e = t.admin.events
  const rows: [string, React.ReactNode][] = [
    [e.at, formatFull(x.at)],
    [e.who, x.username],
    [e.source, <span className={sourceBadge(x.source)}>{e.sources[x.source]}</span>],
    [e.action, e.actions[x.action] ? <>{e.actions[x.action]} <span className="lc-mono text-ink-muted">({x.action})</span></> : <span className="lc-mono">{x.action}</span>],
    [e.namespace, x.namespace ? <Link to={`/${x.namespace}`}>{x.namespace}</Link> : '—'],
    [e.project, x.project ? (x.namespace ? <Link to={`/${x.namespace}/projects/${x.project}`}>{x.project}</Link> : x.project) : '—'],
    [e.target, x.target ? <span className="lc-mono">{x.target}</span> : '—'],
  ]
  return (
    <Dialog title={e.eventTitle(x.id)} onClose={onClose} wide closeOutside>
      <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
        {rows.map(([k, v]) => <div key={k} className="contents"><dt>{k}</dt><dd>{v}</dd></div>)}
      </dl>
      <span className="lc-label">{e.details}</span>
      <pre className="lc-mono m-0 max-h-80 overflow-auto rounded-md border border-border bg-surface-sunken p-3 text-[12px] whitespace-pre-wrap break-all">
        {JSON.stringify(x.details ?? {}, null, 2)}
      </pre>
    </Dialog>
  )
}

function PeriodControl({ from, to, onChange }: { from: string; to: string; onChange: (from: string | null, to: string | null) => void }) {
  const e = useT().admin.events
  const id = useId()
  return (
    <span className="grid grid-cols-[auto_1fr] items-center gap-2">
      <label htmlFor={id + 'from'} className="text-[13px]">{e.from}</label>
      <input id={id + 'from'} type="date" className="lc-input" value={from} max={to || undefined}
        onChange={(ev) => onChange(ev.target.value || null, to || null)} />
      <label htmlFor={id + 'to'} className="text-[13px]">{e.to}</label>
      <input id={id + 'to'} type="date" className="lc-input" value={to} min={from || undefined}
        onChange={(ev) => onChange(from || null, ev.target.value || null)} />
    </span>
  )
}

// Suggest is a text field suggesting, for what is typed, the usernames or
// the actions the events hold.
function Suggest({ label, placeholder, value, onChange, of }: {
  label: string; placeholder: string; value: string; onChange: (v: string | null) => void; of: 'users' | 'actions'
}) {
  const id = useId()
  const variables = { search: value || null }
  const users = useQuery(AuditUsersDoc, { variables, skip: of !== 'users' }).data?.auditUsers
  const actions = useQuery(AuditActionsDoc, { variables, skip: of !== 'actions' }).data?.auditActions
  const options = (of === 'users' ? users : actions) ?? []
  return (
    <>
      <input className="lc-input" list={id} aria-label={label} placeholder={placeholder} value={value} autoComplete="off"
        onChange={(ev) => onChange(ev.target.value.trim() ? ev.target.value : null)} />
      <datalist id={id}>{options.map((o) => <option key={o} value={o} />)}</datalist>
    </>
  )
}
