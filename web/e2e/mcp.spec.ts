import { createHash, randomBytes } from 'node:crypto'
import { expect, test, type APIRequestContext } from '@playwright/test'
import { admin, baseURL } from './env'

// The redirect URI of the client: nobody listens there, the test catches
// the browser on its way.
const callback = 'http://127.0.0.1:9/callback'

// rpc calls the MCP endpoint and returns the result of the JSON-RPC
// response, whether it comes as JSON or as an event stream.
async function rpc(request: APIRequestContext, token: string, method: string, params: unknown) {
  const res = await request.post('mcp', {
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json, text/event-stream', 'Content-Type': 'application/json' },
    data: { jsonrpc: '2.0', id: 1, method, params },
  })
  const text = await res.text()
  const json = res.headers()['content-type']?.startsWith('text/event-stream')
    ? text.split('\n').filter((l) => l.startsWith('data: ')).map((l) => l.slice(6)).join('')
    : text
  return { status: res.status(), body: res.ok() && json ? JSON.parse(json) : null }
}

test('an assistant gets read access through OAuth, then loses it', async ({ page, request }) => {
  // The assistant discovers the server and registers itself.
  const unauthorized = await request.post('mcp', { data: {} })
  expect(unauthorized.status()).toBe(401)
  expect(unauthorized.headers()['www-authenticate']).toContain(`resource_metadata="${baseURL}.well-known/oauth-protected-resource"`)
  const metadata = await (await request.get('.well-known/oauth-authorization-server')).json()
  const registered = await request.post(metadata.registration_endpoint, {
    data: { client_name: 'Claude e2e', redirect_uris: [callback], token_endpoint_auth_method: 'none' },
  })
  expect(registered.status()).toBe(201)
  const { client_id } = await registered.json()

  // The user logs in, then consents.
  const verifier = randomBytes(32).toString('base64url')
  const challenge = createHash('sha256').update(verifier).digest('base64url')
  const authorize = new URL(metadata.authorization_endpoint)
  authorize.search = new URLSearchParams({ response_type: 'code', client_id, redirect_uri: callback, code_challenge: challenge,
    code_challenge_method: 'S256', state: 'st4te', scope: 'read', resource: `${baseURL}mcp` }).toString()
  let back: URL | null = null
  await page.route('http://127.0.0.1:9/**', async (route) => {
    back = new URL(route.request().url())
    await route.fulfill({ body: 'back to the assistant' })
  })
  await page.goto(authorize.toString())
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await page.getByLabel("Nom d'utilisateur").fill(admin.username)
  await page.getByLabel('Mot de passe').fill(admin.password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('heading', { name: 'Autoriser Claude e2e ?' })).toBeVisible()
  await expect(page.getByText('Vous serez ensuite renvoyé vers 127.0.0.1:9.')).toBeVisible()
  await page.getByRole('button', { name: 'Autoriser' }).click()
  await expect(page.getByText('back to the assistant')).toBeVisible()
  expect(back!.searchParams.get('state')).toBe('st4te')
  expect(back!.searchParams.get('iss')).toBe(metadata.issuer)
  const code = back!.searchParams.get('code')!

  // The assistant trades the code, then reads the inventory.
  const tokens = await request.post(metadata.token_endpoint, {
    form: { grant_type: 'authorization_code', code, redirect_uri: callback, code_verifier: verifier, client_id, resource: `${baseURL}mcp` },
  })
  expect(tokens.status()).toBe(200)
  const { access_token, refresh_token, token_type } = await tokens.json()
  expect(token_type).toBe('Bearer')
  expect(refresh_token).toMatch(/^lacis_rt_/)

  const init = await rpc(request, access_token, 'initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } })
  expect(init.status).toBe(200)
  expect(init.body.result.serverInfo.name).toBe('lacis')
  const found = await rpc(request, access_token, 'tools/call', { name: 'find_by_version', arguments: { technology: 'php', namespace: 'acme' } })
  expect(found.status).toBe(200)
  expect(found.body.result.isError).toBeFalsy()
  expect(found.body.result.structuredContent.count).toBeGreaterThan(0)

  // The user sees the assistant in their profile; an administrator removes
  // its access from the users.
  await page.goto('')
  await page.getByRole('button', { name: /^Compte de/ }).click()
  await page.getByRole('menuitem', { name: 'Profil' }).click()
  await expect(page).toHaveURL(/\/account$/)
  await page.getByRole('tab', { name: 'Accès MCP' }).click()
  await expect(page).toHaveURL(/\/account\?tab=mcp$/)
  await expect(page.getByText(`${baseURL}mcp`, { exact: true })).toBeVisible()
  await expect(page.getByText('Claude e2e')).toBeVisible()

  await page.goto('admin')
  await page.getByRole('row', { name: new RegExp(admin.username) }).getByRole('button', { name: 'Accès MCP' }).click()
  const dialog = page.getByRole('dialog', { name: `Accès MCP de ${admin.username}` })
  await expect(dialog.getByText('Claude e2e')).toBeVisible()
  await dialog.getByRole('button', { name: 'Retirer l\'accès' }).click()
  await expect(dialog.getByText('Aucun assistant autorisé.')).toBeVisible()
  expect((await rpc(request, access_token, 'tools/list', {})).status).toBe(401)
})
