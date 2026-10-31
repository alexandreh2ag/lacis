import { ago, duration, parseDuration, plural, writeDuration } from './format'

test('ago', () => {
  const now = Date.parse('2026-10-29T12:00:00Z')
  expect(ago('2026-10-29T11:48:00Z', now)).toBe('il y a 12 minutes')
  expect(ago('2026-10-26T12:00:00Z', now)).toBe('il y a 3 jours')
  expect(ago(null, now)).toBe('')
})

test('duration and plural', () => {
  expect(duration(7 * 86400)).toBe('7 j')
  expect(duration(7200)).toBe('2 h')
  expect(duration(90)).toBe('2 min')
  expect(plural(1, 'host')).toBe('1 host')
  expect(plural(3, 'host')).toBe('3 hosts')
})

test('parseDuration and writeDuration', () => {
  expect(parseDuration('7d')).toBe(7 * 86400)
  expect(parseDuration(' 1d 12h ')).toBe(86400 + 12 * 3600)
  expect(parseDuration('90M')).toBe(5400)
  expect(parseDuration('')).toBeNull()
  expect(parseDuration('7 jours')).toBeNull()
  expect(parseDuration('12')).toBeNull()
  expect(writeDuration(7 * 86400)).toBe('7d')
  expect(writeDuration(86400 + 12 * 3600)).toBe('1d12h')
  expect(writeDuration(5400)).toBe('1h30m')
  expect(writeDuration(0)).toBe('0m')
})
