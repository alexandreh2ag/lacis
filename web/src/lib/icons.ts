import type { IconLimits } from '@/config'
import { t } from '@/i18n'

// techPattern is a technology as the server takes it, lowercased.
export const techPattern = /^[a-z0-9][a-z0-9._+-]{0,62}$/

// limitsText tells the limits of a logo: « PNG, JPEG, WebP ou SVG · 256 Ko
// au plus · 16 à 512 px de côté ».
export function limitsText(l: IconLimits): string {
  return t().components.icons.limits(Math.round(l.maxSize / 1024), l.minSide, l.maxSide)
}

// precheck refuses before the upload a file the server would refuse for
// its extension or its weight; its sides are checked by the server.
export function precheck(file: { name: string; size: number }, l: IconLimits): string | null {
  const ext = file.name.includes('.') ? file.name.slice(file.name.lastIndexOf('.')).toLowerCase() : ''
  const m = t().components.icons
  if (!l.extensions.includes(ext)) return m.extension
  if (file.size > l.maxSize) return m.weight(Math.ceil(file.size / 1024), Math.round(l.maxSize / 1024))
  return null
}

// refusal says in the language spoken why the server refused a logo; fromLacis tells
// an answer of lacis-server, which carries an error, from one of a proxy
// or an older server without the icon routes.
export function refusal(status: number, reason: string | undefined, l: IconLimits, fromLacis = true): string {
  const m = t().components.icons
  switch (reason) {
    case 'too_big': return m.tooBig(Math.round(l.maxSize / 1024))
    case 'format': return m.format
    case 'dimension': return m.dimension(l.minSide, l.maxSide)
    case 'unsafe': return m.unsafe
    case 'tech': return m.tech
  }
  if (status === 404 && !fromLacis) return m.outdated
  if (status === 403) return m.forbidden
  if (status === 401) return m.expired
  return m.failed
}

// sendIcon uploads the file as the logo of the technology, or removes it
// without file; it returns why it was refused, null once done.
export async function sendIcon(tech: string, l: IconLimits, file?: File): Promise<string | null> {
  if (file) {
    const refused = precheck(file, l)
    if (refused) return refused
  }
  let body: FormData | undefined
  if (file) {
    body = new FormData()
    body.append('file', file)
  }
  try {
    const res = await fetch(`icons/${encodeURIComponent(tech)}`, { method: file ? 'PUT' : 'DELETE', body, credentials: 'same-origin' })
    if (res.ok) return null
    const data = (await res.json().catch(() => ({}))) as { error?: string; reason?: string }
    return refusal(res.status, data.reason, l, data.error !== undefined)
  } catch {
    return t().components.errors.noServer
  }
}
