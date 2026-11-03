import { limitsText, precheck, refusal, sendIcon, techPattern } from './icons'

const limits = { maxSize: 256 * 1024, minSide: 16, maxSide: 512, extensions: ['.jpeg', '.jpg', '.png', '.svg', '.webp'] }

test('limitsText', () => {
  expect(limitsText(limits)).toBe('PNG, JPEG, WebP ou SVG · 256 Ko au plus · 16 à 512 px de côté')
})

test('techPattern', () => {
  expect(['traefik', 'php-fpm', 'next.js', 'c++'].every((t) => techPattern.test(t))).toBe(true)
  expect(['', 'Traefik', '-x', 'a/b', 'a b'].some((t) => techPattern.test(t))).toBe(false)
})

test('precheck refuses the extension and the weight', () => {
  expect(precheck({ name: 'logo.PNG', size: 1000 }, limits)).toBeNull()
  expect(precheck({ name: 'logo.gif', size: 1000 }, limits)).toContain('PNG, JPEG, WebP ou SVG')
  expect(precheck({ name: 'logo', size: 1000 }, limits)).toContain('PNG, JPEG, WebP ou SVG')
  expect(precheck({ name: 'logo.svg', size: 300 * 1024 }, limits)).toBe('Le fichier pèse 300 Ko : 256 Ko au plus.')
})

test('refusal says the reason of the server', () => {
  expect(refusal(400, 'dimension', limits)).toBe('Chaque côté de l\'image doit faire entre 16 et 512 px.')
  expect(refusal(400, 'unsafe', limits)).toContain('SVG')
  expect(refusal(400, 'tech', limits)).toContain('Nom de techno')
  expect(refusal(413, 'too_big', limits)).toContain('256 Ko')
  expect(refusal(403, undefined, limits)).toContain('pas le droit')
  expect(refusal(404, undefined, limits, false)).toContain('pas à jour')
  expect(refusal(500, undefined, limits)).toBe('Le logo n\'a pas pu être enregistré.')
})

test('sendIcon puts the file, deletes without, and tells the refusal', async () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockResolvedValueOnce(new Response('{}', { status: 200 }))
  const file = new File(['<svg/>'], 'traefik.svg', { type: 'image/svg+xml' })
  expect(await sendIcon('traefik', limits, file)).toBeNull()
  expect(fetchMock.mock.calls[0][0]).toBe('icons/traefik')
  expect(fetchMock.mock.calls[0][1].method).toBe('PUT')

  fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
  expect(await sendIcon('traefik', limits)).toBeNull()
  expect(fetchMock.mock.calls[1][1].method).toBe('DELETE')

  fetchMock.mockResolvedValueOnce(new Response('{"error":"x","reason":"unsafe"}', { status: 400 }))
  expect(await sendIcon('traefik', limits, file)).toContain('script')
  expect(await sendIcon('traefik', limits, new File(['x'], 'x.gif'))).toContain('PNG')
  expect(fetchMock).toHaveBeenCalledTimes(3)
  vi.unstubAllGlobals()
})
