import { useEffect, useId, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { lang, useT } from '@/i18n'
import { statusText } from '@/lib/format'
import { Chevron } from './Icon'

// useListParams keeps the state of a listing in the URL: a shared link
// opens the same search, filters, order and page.
export function useListParams(defaults: { sort: string; size?: number }) {
  const [params, setParams] = useSearchParams()
  const size = Number(params.get('size')) || defaults.size || 20
  const page = Math.max(1, Number(params.get('page')) || 1)
  const set = (changes: Record<string, string | string[] | null>, keepPage = false) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      for (const [k, v] of Object.entries(changes)) {
        next.delete(k)
        if (Array.isArray(v)) v.forEach((x) => next.append(k, x))
        else if (v !== null && v !== '') next.set(k, v)
      }
      if (!keepPage) next.delete('page')
      return next
    }, { replace: true })
  }
  return {
    q: params.get('q') ?? '',
    sort: params.get('sort') ?? defaults.sort,
    desc: params.get('desc') === '1',
    page,
    size,
    list: (key: string) => params.getAll(key),
    get: (key: string) => params.get(key),
    set,
    setPage: (p: number) => set({ page: p > 1 ? String(p) : null }, true),
    toggleSort: (by: string, current: string, desc: boolean) => set({ sort: by, desc: by === current && !desc ? '1' : null }),
  }
}

// useTable sorts, searches and pages a list held by the page; desc starts
// it in descending order, the newest first for a date.
export function useTable<T>(rows: T[], text: (r: T) => string[], value: (r: T, by: string) => string | number, initial: string, desc = false) {
  const [q, setQ] = useState('')
  const [sort, setSort] = useState({ by: initial, desc })
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(20)
  const list = useMemo(() => rows.filter((r) => !q || text(r).some((x) => x.toLowerCase().includes(q.toLowerCase())))
    .sort((a, b) => {
      const x = value(a, sort.by)
      const y = value(b, sort.by)
      const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), lang(), { numeric: true })
      return sort.desc ? -c : c
    }), [rows, q, sort, text, value])
  return {
    q, setQ: (v: string) => { setQ(v); setPage(1) }, sort, onSort: (by: string) => setSort((s) => ({ by, desc: s.by === by && !s.desc })),
    page, setPage, size, setSize: (s: number) => { setSize(s); setPage(1) }, list, shown: list.slice((page - 1) * size, page * size),
  }
}

export function SearchInput({ value, onChange, placeholder, width = 320 }: { value: string; onChange: (v: string) => void; placeholder: string; width?: number }) {
  const id = useId()
  const [text, setText] = useState(value)
  useEffect(() => setText(value), [value])
  // The listing follows the input after a short pause.
  useEffect(() => {
    if (text === value) return
    const t = setTimeout(() => onChange(text), 250)
    return () => clearTimeout(t)
  }, [text, value, onChange])
  return (
    <span className="lc-search" style={{ width }}>
      <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" aria-hidden="true">
        <circle cx="7" cy="7" r="4.5" />
        <path d="m10.5 10.5 3 3" />
      </svg>
      <label htmlFor={id} className="sr-only">{placeholder}</label>
      <input id={id} className="lc-input" type="search" placeholder={placeholder} value={text} onChange={(e) => setText(e.target.value)} />
    </span>
  )
}

export interface ChipOption {
  value: string
  label: string
  count?: number
}

// Chips toggle values in a list: none pressed selects all.
export function Chips({ options, selected, onChange, label }: { options: ChipOption[]; selected: string[]; onChange: (v: string[]) => void; label?: string }) {
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label={label}>
      {options.map((o) => {
        const on = selected.includes(o.value)
        return (
          <button key={o.value} className="lc-chip" type="button" aria-pressed={on}
            onClick={() => onChange(on ? selected.filter((v) => v !== o.value) : [...selected, o.value])}>
            {o.label}
            {o.count !== undefined ? <span className="lc-chip__count">{o.count}</span> : null}
          </button>
        )
      })}
    </div>
  )
}

const sortIcons = {
  none: 'M3.5 4.5 6 2l2.5 2.5M3.5 7.5 6 10l2.5-2.5',
  asc: 'M6 10V2.5M3 5.5l3-3 3 3',
  desc: 'M6 2v7.5M3 6.5l3 3 3-3',
}

// SortHeader is a column header sorting the listing when clicked.
export function SortHeader({ label, by, sort, desc, onSort, num }: { label: string; by: string; sort: string; desc: boolean; onSort: (by: string) => void; num?: boolean }) {
  const state = sort === by ? (desc ? 'desc' : 'asc') : 'none'
  return (
    <th className={num ? 'lc-num' : undefined} aria-sort={state === 'none' ? 'none' : state === 'asc' ? 'ascending' : 'descending'}>
      <button className="lc-sort" type="button" onClick={() => onSort(by)}>
        {label}
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d={sortIcons[state]} />
        </svg>
      </button>
    </th>
  )
}

// pages lists the pages to show: the first, the last, and those around the
// current one, null for a gap.
export function pages(current: number, count: number): (number | null)[] {
  if (count <= 7) return Array.from({ length: count }, (_, i) => i + 1)
  const out: (number | null)[] = [1]
  const from = Math.max(2, current - 1)
  const to = Math.min(count - 1, current + 1)
  if (from > 2) out.push(null)
  for (let p = from; p <= to; p++) out.push(p)
  if (to < count - 1) out.push(null)
  out.push(count)
  return out
}

export function Pager({ page, size, total, noun, onPage, onSize, sizes = [20, 50, 100] }: {
  page: number; size: number; total: number; noun: string; onPage: (p: number) => void; onSize: (s: number) => void; sizes?: number[]
}) {
  const t = useT().components.pager
  const id = useId()
  const count = Math.max(1, Math.ceil(total / size))
  const first = total === 0 ? 0 : (page - 1) * size + 1
  const last = Math.min(total, page * size)
  return (
    <nav className="lc-pager" aria-label={t.label}>
      <span className="lc-pager__range"><b>{first}–{last}</b> {t.of} <b>{total}</b> {noun}</span>
      <label className="lc-pager__size" htmlFor={id}>
        {t.perPage}
        <select id={id} value={size} onChange={(e) => onSize(Number(e.target.value))}>
          {sizes.map((s) => <option key={s} value={s}>{s}</option>)}
        </select>
      </label>
      <span className="lc-pager__pages">
        <button type="button" aria-label={t.previous} disabled={page <= 1} onClick={() => onPage(page - 1)}><Chevron dir="left" /></button>
        {pages(page, count).map((p, i) => p === null
          ? <span key={'gap' + i} aria-hidden="true">…</span>
          : <button key={p} type="button" aria-current={p === page ? 'page' : undefined} onClick={() => onPage(p)}>{p}</button>)}
        <button type="button" aria-label={t.next} disabled={page >= count} onClick={() => onPage(page + 1)}><Chevron dir="right" /></button>
      </span>
    </nav>
  )
}

export function StatusBadge({ status, lastSeen }: { status: string | null | undefined; lastSeen?: string }) {
  if (status === 'STALE') return <span className="lc-badge lc-badge--stale" title={lastSeen}>{statusText('STALE')}</span>
  if (status === 'INACTIVE') return <span className="lc-badge lc-badge--inactive" title={lastSeen}>{statusText('INACTIVE')}</span>
  return <span className="lc-badge">{statusText('ACTIVE')}</span>
}

// envOptions are the envs of a namespace in their order, as chips.
export function envOptions(envs: { slug: string; sortOrder: number }[] | undefined): ChipOption[] {
  return [...(envs ?? [])].sort((a, b) => a.sortOrder - b.sortOrder).map((e) => ({ value: e.slug, label: e.slug }))
}

// statusOptions name the statuses in the language spoken when read.
export const statusOptions: ChipOption[] = ['ACTIVE', 'STALE', 'INACTIVE'].map((value) => ({
  value, get label() { return statusText(value).toLowerCase() },
}))
