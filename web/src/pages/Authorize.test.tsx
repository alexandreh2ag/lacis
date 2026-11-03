import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, vi } from 'vitest'
import { Authorize } from './Authorize'

const consent = { client_name: 'Claude', redirect_host: 'claude.ai', user: { username: 'alice', first_name: 'Alice' } }

function Where() {
  const l = useLocation()
  return <p data-testid="where">{l.pathname + l.search}</p>
}

function show(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/authorize" element={<Authorize />} />
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
}

function reply(status: number, body?: unknown) {
  return Promise.resolve(new Response(body === undefined ? null : JSON.stringify(body), { status }))
}

afterEach(() => vi.restoreAllMocks())

test('an untrusted request says so without asking the server', () => {
  const fetch = vi.spyOn(globalThis, 'fetch')
  show('/authorize?error=invalid_client')
  expect(screen.getByRole('alert').textContent).toContain('demande d\'autorisation est invalide')
  expect(fetch).not.toHaveBeenCalled()
})

test('without session, the login comes first and brings back here', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => reply(401))
  show('/authorize?request=a.b')
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/login?redirect=%2Fauthorize%3Frequest%3Da.b'))
})

test('an expired request says so', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => reply(400, { error: 'request_expired' }))
  show('/authorize?request=a.b')
  expect((await screen.findByRole('alert')).textContent).toContain('a expiré')
})

test('the consent goes back to the assistant', async () => {
  const assign = vi.fn()
  vi.spyOn(window, 'location', 'get').mockReturnValue({ ...window.location, assign })
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation((input) =>
    String(input).startsWith('oauth/request') ? reply(200, consent) : reply(200, { redirect: 'https://claude.ai/cb?code=x' }))
  show('/authorize?request=a.b')
  expect(await screen.findByRole('heading', { name: 'Autoriser Claude ?' })).toBeTruthy()
  expect(screen.getByText('alice')).toBeTruthy()
  expect(screen.getByText('Vous serez ensuite renvoyé vers claude.ai.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Autoriser' }))
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://claude.ai/cb?code=x'))
  expect(fetch).toHaveBeenLastCalledWith('oauth/authorize', expect.objectContaining({ method: 'POST', body: JSON.stringify({ request: 'a.b', approve: true }) }))
})

test('a refusal goes back to the assistant too', async () => {
  const assign = vi.fn()
  vi.spyOn(window, 'location', 'get').mockReturnValue({ ...window.location, assign })
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation((input) =>
    String(input).startsWith('oauth/request') ? reply(200, consent) : reply(200, { redirect: 'https://claude.ai/cb?error=access_denied' }))
  show('/authorize?request=a.b')
  fireEvent.click(await screen.findByRole('button', { name: 'Refuser' }))
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://claude.ai/cb?error=access_denied'))
  expect(fetch).toHaveBeenLastCalledWith('oauth/authorize', expect.objectContaining({ body: JSON.stringify({ request: 'a.b', approve: false }) }))
})

test('another account logs out and logs in again', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation((input) =>
    String(input).startsWith('oauth/request') ? reply(200, consent) : reply(204))
  show('/authorize?request=a.b')
  fireEvent.click(await screen.findByRole('button', { name: 'Changer de compte' }))
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/login?redirect=%2Fauthorize%3Frequest%3Da.b'))
  expect(fetch).toHaveBeenCalledWith('auth/logout', expect.objectContaining({ method: 'POST' }))
})

test('a failure keeps the page and says so', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) =>
    String(input).startsWith('oauth/request') ? reply(200, consent) : reply(500))
  show('/authorize?request=a.b')
  fireEvent.click(await screen.findByRole('button', { name: 'Autoriser' }))
  expect((await screen.findByRole('alert')).textContent).toContain('n\'a pas abouti')
})
