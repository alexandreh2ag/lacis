import { useApolloClient, useQuery } from '@apollo/client'
import { useMemo, useRef, useState } from 'react'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { TechLogo } from '@/components/Icon'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { config } from '@/config'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'
import { formatDate } from '@/lib/format'
import { limitsText, sendIcon, techPattern } from '@/lib/icons'
import { defaultTechs, logoOf } from '@/lib/logos'

const IconsDoc = graphql(`
  query AdminIcons {
    technologies
    techIcons { tech url updatedAt updatedBy }
  }
`)

type Source = 'custom' | 'builtin' | 'none'

export interface IconRow {
  tech: string
  source: Source
  // inventory tells a technology the agents found.
  inventory: boolean
  updatedAt?: string
  updatedBy?: string | null
}

// iconRows are the technologies to give a logo to: those of the inventory,
// those the front has a logo of, and those given one.
export function iconRows(technologies: string[], icons: { tech: string; updatedAt: string; updatedBy?: string | null }[], extra: string[]): IconRow[] {
  const custom = new Map(icons.map((i) => [i.tech, i]))
  const inventory = new Set(technologies)
  const all = new Set([...technologies, ...defaultTechs, ...custom.keys(), ...extra])
  return [...all].map((tech) => {
    const c = custom.get(tech)
    return {
      tech, inventory: inventory.has(tech), updatedAt: c?.updatedAt, updatedBy: c?.updatedBy,
      source: c ? 'custom' : logoOf(tech) ? 'builtin' : 'none',
    }
  })
}

// Icons lists the logos of the technologies: one uploaded shows in place
// of the one the front embeds, or gives one to a technology without.
export function Icons({ write }: { write: boolean }) {
  const client = useApolloClient()
  const { data, error } = useQuery(IconsDoc)
  const input = useRef<HTMLInputElement>(null)
  const [target, setTarget] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [message, setMessage] = useState<{ tech: string; text: string } | null>(null)
  const [extra, setExtra] = useState<string[]>([])
  const [other, setOther] = useState('')
  const [sources, setSources] = useState<string[]>([])
  const [inInventory, setInInventory] = useState<string[]>([])
  const t = useT()
  const c = t.admin.icons

  const rows = useMemo(() => iconRows(data?.technologies ?? [], data?.techIcons ?? [], extra), [data, extra])
  const shown = rows.filter((r) => (!sources.length || sources.includes(r.source))
    && (!inInventory.length || inInventory.includes(r.inventory ? 'yes' : 'no')))
  const tbl = useTable(shown, (r) => [r.tech], (r, by) => by === 'updated' ? r.updatedAt ?? '' : by === 'source' ? r.source : r.tech, 'tech')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />

  const send = async (tech: string, file?: File) => {
    setBusy(tech)
    setMessage(null)
    const refused = await sendIcon(tech, config.icons, file)
    if (refused) setMessage({ tech, text: refused })
    else await client.refetchQueries({ include: ['AdminIcons', 'Session'] })
    setBusy(null)
  }
  const pick = (tech: string) => {
    setTarget(tech)
    input.current?.click()
  }
  const addOther = () => {
    const tech = other.trim().toLowerCase()
    if (!techPattern.test(tech)) {
      setMessage({ tech: other, text: c.invalidTech })
      return
    }
    if (!extra.includes(tech)) setExtra([...extra, tech])
    setOther('')
    pick(tech)
  }

  if (error) return <p role="alert">{c.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">{c.intro}{limitsText(config.icons)}.</p>
      <input ref={input} type="file" className="sr-only" tabIndex={-1} aria-hidden="true" accept={config.icons.extensions.join(',')}
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ''
          if (file && target) void send(target, file)
        }} />
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={c.search} width={260} />}
            fields={[
              chipsField({ label: c.logo, options: (['custom', 'builtin', 'none'] as Source[]).map((s) => ({ value: s, label: c.sources[s].toLowerCase() })), selected: sources, onChange: setSources }),
              chipsField({ label: c.inventory, options: [{ value: 'yes', label: c.present }, { value: 'no', label: c.absent }], selected: inInventory, onChange: setInInventory }),
            ]}
            count={c.count(tbl.list.length)} />
        </div>
        {write ? (
          <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); addOther() }}>
            <label htmlFor="other-tech" className="sr-only">{c.other}</label>
            <input id="other-tech" className="lc-input lc-mono" placeholder={c.otherPlaceholder} autoCapitalize="none" spellCheck={false}
              value={other} onChange={(e) => setOther(e.target.value)} style={{ width: 160 }} />
            <button type="submit" className="lc-btn lc-btn--primary" disabled={!other.trim()}>{c.uploadOther}</button>
          </form>
        ) : null}
      </div>
      {message ? <div className="lc-alert" role="alert"><span className="lc-mono font-semibold">{message.tech}</span> : {message.text}</div> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            <th>{c.logo}</th>{head(c.tech, 'tech')}{head(c.source, 'source')}<th>{c.builtin}</th><th>{c.inventory}</th>{head(c.updated, 'updated')}
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((r) => (
              <tr key={r.tech}>
                <td><span className="lc-iconcell"><TechLogo tech={r.tech} title={r.tech} /></span></td>
                <td><span className="lc-mono font-semibold">{r.tech}</span></td>
                <td>{r.source === 'custom' ? <span className="lc-badge">{c.sources.custom}</span>
                  : <span className="lc-badge lc-badge--plain">{c.sources[r.source]}</span>}</td>
                <td>{r.source === 'custom' && logoOf(r.tech)
                  ? <span className="lc-iconcell lc-iconcell--muted" title={c.builtinHint}><TechLogo tech={r.tech} builtin /></span>
                  : <span className="lc-muted">—</span>}</td>
                <td>{r.inventory ? c.present : <span className="lc-muted">{c.absent}</span>}</td>
                <td>{r.updatedAt ? <span>{formatDate(r.updatedAt)}{r.updatedBy ? <span className="lc-muted"> · {r.updatedBy}</span> : null}</span>
                  : <span className="lc-muted">—</span>}</td>
                {write ? (
                  <td className="lc-num">
                    <span className="inline-flex gap-1">
                      <button type="button" className="lc-btn lc-btn--small" disabled={busy === r.tech} onClick={() => pick(r.tech)}>
                        {busy === r.tech ? c.sending : r.source === 'custom' ? c.replace : c.upload}
                      </button>
                      {r.source === 'custom' ? (
                        <button type="button" className="lc-btn lc-btn--small" disabled={busy === r.tech} onClick={() => void send(r.tech)}>{c.reset}</button>
                      ) : null}
                    </span>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={c.noun} onPage={tbl.setPage} onSize={tbl.setSize} />
    </>
  )
}
