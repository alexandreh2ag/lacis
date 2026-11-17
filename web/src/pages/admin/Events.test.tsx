import { MockedProvider, type MockedResponse } from '@apollo/client/testing'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { AuditActionsDocument, AuditEventsDocument, AuditUsersDocument } from '@/graphql/graphql'
import { dayBounds, detailsText, eventSort, Events } from './Events'

test('dayBounds covers whole local days', () => {
  expect(dayBounds('', '')).toEqual({})
  expect(dayBounds('2026-11-17', '')).toEqual({ from: new Date(2026, 10, 17).toISOString() })
  expect(dayBounds('', '2026-11-17')).toEqual({ to: new Date(2026, 10, 18).toISOString() })
  expect(dayBounds('2026-11-30', '2026-11-30')).toEqual({ from: new Date(2026, 10, 30).toISOString(), to: new Date(2026, 11, 1).toISOString() })
})

test.each([
  { sort: 'AT', desc: null, want: { by: 'AT', desc: true } },
  { sort: 'nope', desc: null, want: { by: 'AT', desc: true } },
  { sort: 'AT', desc: '0', want: { by: 'AT', desc: false } },
  { sort: 'USER', desc: null, want: { by: 'USER', desc: false } },
  { sort: 'ACTION', desc: '1', want: { by: 'ACTION', desc: true } },
])('eventSort $sort $desc', ({ sort, desc, want }) => {
  expect(eventSort(sort, desc)).toEqual(want)
})

test.each([
  { name: 'nothing', details: null, shown: [], want: '' },
  { name: 'no object', details: ['a'], shown: [], want: '' },
  { name: 'the arguments', details: { host: 'web-1', hosts: ['a', 'b'], level: 2 }, shown: [], want: 'host: web-1 · hosts: ["a","b"] · level: 2' },
  { name: 'the empty ones and those shown left out', details: { namespace: 'acme', query: '', projects: [], env: null, app: 'shop' }, shown: ['namespace'], want: 'app: shop' },
  { name: 'an object flattened', details: { input: { username: 'bob', password: '***', roles: [] }, id: 'u1' }, shown: [], want: 'input.username: bob · input.password: *** · id: u1' },
])('detailsText: $name', ({ details, shown, want }) => {
  expect(detailsText(details, shown)).toBe(want)
})

const event = (id: number, over: object) => ({
  __typename: 'AuditEvent' as const, id, at: '2026-11-17T09:00:00Z', username: 'bob', source: 'MCP' as const, action: 'get_host',
  namespace: 'acme', project: null, target: 'web-1', details: { host: 'web-1', namespace: 'acme' }, ...over,
})

test('the events show with their filters, read from the URL', async () => {
  const mocks: MockedResponse[] = [
    {
      request: { query: AuditEventsDocument, variables: {
        filter: { from: new Date(2026, 10, 17).toISOString(), user: 'bob', action: 'o', sources: ['MCP', 'AUTH'], search: 'web' },
        sort: { by: 'AT', desc: true },
        page: { offset: 0, limit: 50 },
      } },
      result: { data: { auditEvents: { __typename: 'AuditEventPage', total: 2, items: [
        event(2, {}),
        event(1, { source: 'AUTH', action: 'login', namespace: null, target: null, details: { method: 'sso' } }),
      ] } } },
    },
    { request: { query: AuditUsersDocument, variables: { search: 'bob' } }, result: { data: { auditUsers: ['bob', 'bobby'] } } },
    { request: { query: AuditActionsDocument, variables: { search: 'o' } }, result: { data: { auditActions: ['get_host', 'login'] } } },
  ]
  render(
    <MockedProvider mocks={mocks}>
      <MemoryRouter initialEntries={['/admin?tab=events&from=2026-11-17&user=bob&action=o&source=MCP&source=AUTH&q=web']}><Events /></MemoryRouter>
    </MockedProvider>,
  )
  expect(await screen.findByText('2 events')).toBeTruthy()
  const cells = (row: HTMLElement) => within(row).getAllByRole('cell').map((c) => c.textContent)
  expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Date', 'Qui', 'Source', 'Action', 'Namespace / projet', 'Cible', 'Paramètres', 'Actions'])
  const rows = screen.getAllByRole('row').slice(1)
  expect(cells(rows[0]).slice(1, 6)).toEqual(['bob', 'MCP', 'get_host', 'acme', 'web-1'])
  expect(cells(rows[1]).slice(2, 4)).toEqual(['Authentification', 'Connexion'])
  expect(within(rows[0]).getByText('web-1')).toBeTruthy()
  expect(within(rows[0]).getByText('host: web-1')).toBeTruthy()
  expect(within(rows[0]).getByRole('link', { name: 'acme' }).getAttribute('href')).toBe('/acme')
  expect(within(rows[1]).getByText('SSO')).toBeTruthy()
  expect(screen.getByText('depuis le 2026-11-17')).toBeTruthy()
  expect(screen.getByText('o', { selector: '.lc-pill *, .lc-pill' })).toBeTruthy()

  fireEvent.click(within(rows[0]).getByRole('button', { name: "Voir l'event" }))
  const dialog = screen.getByRole('dialog', { hidden: true })
  expect(within(dialog).getByText('Event n° 2')).toBeTruthy()
  expect(within(dialog).getByText(/"host": "web-1"/)).toBeTruthy()
  fireEvent.click(within(dialog).getAllByRole('button', { name: 'Fermer', hidden: true })[0])
  expect(screen.queryByRole('dialog', { hidden: true })).toBeNull()
})
