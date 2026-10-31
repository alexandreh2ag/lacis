import { lang, t } from '@/i18n'

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1],
]

// The formats follow the language spoken, made once per language.
const formats = new Map<string, { rtf: Intl.RelativeTimeFormat; dateTime: Intl.DateTimeFormat; date: Intl.DateTimeFormat }>()

function format() {
  const l = lang()
  let f = formats.get(l)
  if (!f) {
    f = {
      rtf: new Intl.RelativeTimeFormat(l, { numeric: 'auto' }),
      dateTime: new Intl.DateTimeFormat(l, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }),
      date: new Intl.DateTimeFormat(l, { day: 'numeric', month: 'short', year: 'numeric' }),
    }
    formats.set(l, f)
  }
  return f
}

// ago tells how long ago an instant was: « il y a 12 minutes ».
export function ago(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return ''
  const seconds = Math.round((new Date(iso).getTime() - now) / 1000)
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size || unit === 'second') {
      return format().rtf.format(Math.round(seconds / size), unit)
    }
  }
  return ''
}

export function formatDateTime(iso: string | null | undefined): string {
  return iso ? format().dateTime.format(new Date(iso)) : ''
}

export function formatDate(iso: string | null | undefined): string {
  return iso ? format().date.format(new Date(iso)) : ''
}

// duration writes seconds as days or hours: « 7 j », « 12 h ».
export function duration(seconds: number): string {
  const c = t().common
  if (seconds % 86400 === 0) return `${seconds / 86400} ${c.days}`
  if (seconds % 3600 === 0) return `${seconds / 3600} ${c.hours}`
  return `${Math.round(seconds / 60)} ${c.minutes}`
}

// plural writes a count and its word, by the rule of the language spoken.
export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${t().common.many(n) ? many : one}`
}

// confidenceText and statusText name a confidence and a host status.
export function confidenceText(c: string): string {
  return t().common.confidence[c] ?? c.toLowerCase()
}

export function statusText(s: string): string {
  return t().common.status[s] ?? s
}

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
