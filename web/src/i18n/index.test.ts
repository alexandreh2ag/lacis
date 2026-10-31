import { detect, lang, pick, picked, t } from '.'

test('the first language of the browser the front speaks, else English', () => {
  expect(detect(['fr-FR', 'en-US'])).toBe('fr')
  expect(detect(['de-DE', 'en-GB', 'fr'])).toBe('en')
  expect(detect(['de-DE', 'FR-ca'])).toBe('fr')
  expect(detect(['de-DE'])).toBe('en')
  expect(detect([])).toBe('en')
})

test('a language picked is kept and spoken; none gives the choice back to the browser', () => {
  pick('en')
  expect(picked()).toBe('en')
  expect(lang()).toBe('en')
  expect(t().common.account.logout).toBe('Log out')
  expect(document.documentElement.lang).toBe('en')
  pick(null)
  expect(picked()).toBeNull()
  expect(lang()).toBe(detect())
  localStorage.setItem('lacis-lang', 'de')
  expect(picked()).toBeNull()
  pick('fr')
})
