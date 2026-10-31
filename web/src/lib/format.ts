const rtf = new Intl.RelativeTimeFormat('fr', { numeric: 'auto' })
const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1],
]

// ago tells how long ago an instant was: « il y a 12 minutes ».
export function ago(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return ''
  const seconds = Math.round((new Date(iso).getTime() - now) / 1000)
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size || unit === 'second') {
      return rtf.format(Math.round(seconds / size), unit)
    }
  }
  return ''
}

const dateTime = new Intl.DateTimeFormat('fr', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
const date = new Intl.DateTimeFormat('fr', { day: 'numeric', month: 'short', year: 'numeric' })

export function formatDateTime(iso: string | null | undefined): string {
  return iso ? dateTime.format(new Date(iso)) : ''
}

export function formatDate(iso: string | null | undefined): string {
  return iso ? date.format(new Date(iso)) : ''
}

// duration writes seconds as days or hours: « 7 j », « 12 h ».
export function duration(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400} j`
  if (seconds % 3600 === 0) return `${seconds / 3600} h`
  return `${Math.round(seconds / 60)} min`
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n > 1 ? many : one}`
}

export const confidenceFr: Record<string, string> = { EXACT: 'exact', INFERRED: 'déduit', AMBIGUOUS: 'ambigu' }
export const statusFr: Record<string, string> = { ACTIVE: 'Actif', STALE: 'Stale', INACTIVE: 'Inactif' }

// parseDuration reads a duration written as 7d, 12h, 1d12h or 30m, in
// seconds; null when it is no duration.
export function parseDuration(text: string): number | null {
  const s = text.trim().toLowerCase().replace(/\s+/g, '')
  if (!/^(\d+[dhm])+$/.test(s)) return null
  let seconds = 0
  for (const [, n, unit] of s.matchAll(/(\d+)([dhm])/g)) {
    seconds += Number(n) * (unit === 'd' ? 86400 : unit === 'h' ? 3600 : 60)
  }
  return seconds
}

// writeDuration writes seconds as parseDuration reads them: 7d, 1d12h.
export function writeDuration(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.round((seconds % 3600) / 60)
  const out = (d ? `${d}d` : '') + (h ? `${h}h` : '') + (m ? `${m}m` : '')
  return out || '0m'
}
