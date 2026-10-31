import { useQuery } from '@apollo/client'
import { useParams } from 'react-router'
import { HistoryEvent } from '@/components/HistoryEvents'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { SearchInput, useListParams } from '@/components/Listing'
import { Chevron } from '@/components/Icon'
import { graphql } from '@/graphql'

const HistoryDoc = graphql(`
  query History($ns: String!, $filter: HistoryFilter, $page: PageInput) {
    namespace(slug: $ns) {
      id
      history(filter: $filter, page: $page) { hasMore items { id occurredAt hostname entityType kind before after } }
    }
  }
`)

const types = [
  { value: 'host', label: 'hosts' }, { value: 'app', label: 'apps' }, { value: 'service', label: 'services' },
  { value: 'webserver', label: 'webservers' }, { value: 'runtime', label: 'runtimes' }, { value: 'link', label: 'liens' },
]

const day = new Intl.DateTimeFormat('fr', { weekday: 'long', day: 'numeric', month: 'long' })

export function History() {
  const { ns = '' } = useParams()
  const l = useListParams({ sort: 'occurred', size: 50 })
  const type = l.list('type')[0] ?? null
  const host = l.q.trim()
  const oldestFirst = l.get('order') === 'asc'
  const { data, loading, error } = useQuery(HistoryDoc, {
    variables: { ns, filter: { hostname: host || null, entityType: type, oldestFirst }, page: { offset: (l.page - 1) * l.size, limit: l.size } },
  })
  const h = data?.namespace?.history
  // The events grouped by day, in the order asked.
  const groups = new Map<string, NonNullable<typeof h>['items']>()
  for (const e of h?.items ?? []) {
    const k = day.format(new Date(e.occurredAt))
    groups.set(k, [...(groups.get(k) ?? []), e])
  }
  return (
    <Page title="Historique">
      <div className="flex max-w-[960px] flex-col gap-6">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-0 flex-1">
            <FilterBar
              search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder="Hostname exact" width={260} />}
              fields={[chipsField({ label: 'Élément', options: types, selected: type ? [type] : [], onChange: (v) => l.set({ type: v.filter((x) => x !== type).slice(0, 1) }) })]}
            />
          </div>
          <div className="lc-zoom" role="group" aria-label="Ordre">
            <button type="button" aria-pressed={!oldestFirst} onClick={() => l.set({ order: null })}>Plus récents d'abord</button>
            <button type="button" aria-pressed={oldestFirst} onClick={() => l.set({ order: 'asc' })}>Plus anciens d'abord</button>
          </div>
        </div>
        {error ? <p role="alert">{error.message.includes('host not found') ? `Aucun host « ${host} ».` : `L'historique n'a pas pu être chargé : ${error.message}`}</p> : null}
        {!loading && h?.items.length === 0 ? <p className="lc-muted">Aucun changement.</p> : null}
        {[...groups].map(([title, events]) => (
          <section key={title} className="flex flex-col gap-3">
            <h2 className="lc-label m-0">{title}</h2>
            <div className="flex flex-col gap-3.5 rounded-lg border border-border bg-surface-raised p-4">
              {events.map((e) => <HistoryEvent key={e.id} event={e} />)}
            </div>
          </section>
        ))}
        {h ? (
          <nav className="lc-pager" aria-label="Pagination">
            <span className="lc-pager__range">Événements <b>{(l.page - 1) * l.size + (h.items.length ? 1 : 0)}–{(l.page - 1) * l.size + h.items.length}</b>, {oldestFirst ? 'les plus anciens' : 'les plus récents'} d'abord</span>
            <span className="lc-pager__pages">
              <button type="button" disabled={l.page <= 1} onClick={() => l.setPage(l.page - 1)} aria-label={oldestFirst ? 'Plus anciens' : 'Plus récents'}><Chevron dir="left" /></button>
              <button type="button" aria-current="page">{l.page}</button>
              <button type="button" disabled={!h.hasMore} onClick={() => l.setPage(l.page + 1)} aria-label={oldestFirst ? 'Plus récents' : 'Plus anciens'}><Chevron dir="right" /></button>
            </span>
          </nav>
        ) : null}
      </div>
    </Page>
  )
}
