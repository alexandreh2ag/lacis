import { useQuery } from '@apollo/client'
import { useParams } from 'react-router'
import { HistoryEvent } from '@/components/HistoryEvents'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { SearchInput, useListParams } from '@/components/Listing'
import { Chevron } from '@/components/Icon'
import { graphql } from '@/graphql'
import { lang, useT } from '@/i18n'

const HistoryDoc = graphql(`
  query History($ns: String!, $filter: HistoryFilter, $page: PageInput) {
    namespace(slug: $ns) {
      id
      history(filter: $filter, page: $page) { hasMore items { id occurredAt hostname entityType kind before after } }
    }
  }
`)

const types = ['host', 'app', 'service', 'webserver', 'runtime', 'link']

export function History() {
  const { ns = '' } = useParams()
  const t = useT()
  const th = t.pages.history
  const l = useListParams({ sort: 'occurred', size: 50 })
  const type = l.list('type')[0] ?? null
  const host = l.q.trim()
  const oldestFirst = l.get('order') === 'asc'
  const { data, loading, error } = useQuery(HistoryDoc, {
    variables: { ns, filter: { hostname: host || null, entityType: type, oldestFirst }, page: { offset: (l.page - 1) * l.size, limit: l.size } },
  })
  const h = data?.namespace?.history
  // The events grouped by day, in the order asked.
  const day = new Intl.DateTimeFormat(lang(), { weekday: 'long', day: 'numeric', month: 'long' })
  const groups = new Map<string, NonNullable<typeof h>['items']>()
  for (const e of h?.items ?? []) {
    const k = day.format(new Date(e.occurredAt))
    groups.set(k, [...(groups.get(k) ?? []), e])
  }
  return (
    <Page title={th.title}>
      <div className="flex max-w-[960px] flex-col gap-6">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-0 flex-1">
            <FilterBar
              search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder={th.search} width={260} />}
              fields={[chipsField({ label: th.element, options: types.map((value) => ({ value, label: th.types[value] })), selected: type ? [type] : [], onChange: (v) => l.set({ type: v.filter((x) => x !== type).slice(0, 1) }) })]}
            />
          </div>
          <div className="lc-zoom" role="group" aria-label={th.order}>
            <button type="button" aria-pressed={!oldestFirst} onClick={() => l.set({ order: null })}>{th.latestFirst}</button>
            <button type="button" aria-pressed={oldestFirst} onClick={() => l.set({ order: 'asc' })}>{th.oldestFirst}</button>
          </div>
        </div>
        {error ? <p role="alert">{error.message.includes('host not found') ? th.noHost(host) : th.failed(error.message)}</p> : null}
        {!loading && h?.items.length === 0 ? <p className="lc-muted">{t.pages.shared.noChange}</p> : null}
        {[...groups].map(([title, events]) => (
          <section key={title} className="flex flex-col gap-3">
            <h2 className="lc-label m-0">{title}</h2>
            <div className="flex flex-col gap-3.5 rounded-lg border border-border bg-surface-raised p-4">
              {events.map((e) => <HistoryEvent key={e.id} event={e} />)}
            </div>
          </section>
        ))}
        {h ? (
          <nav className="lc-pager" aria-label={th.pagination}>
            <span className="lc-pager__range">{th.events} <b>{(l.page - 1) * l.size + (h.items.length ? 1 : 0)}–{(l.page - 1) * l.size + h.items.length}</b>{oldestFirst ? th.oldestFirstEnd : th.latestFirstEnd}</span>
            <span className="lc-pager__pages">
              <button type="button" disabled={l.page <= 1} onClick={() => l.setPage(l.page - 1)} aria-label={oldestFirst ? th.oldest : th.latest}><Chevron dir="left" /></button>
              <button type="button" aria-current="page">{l.page}</button>
              <button type="button" disabled={!h.hasMore} onClick={() => l.setPage(l.page + 1)} aria-label={oldestFirst ? th.latest : th.oldest}><Chevron dir="right" /></button>
            </span>
          </nav>
        ) : null}
      </div>
    </Page>
  )
}
