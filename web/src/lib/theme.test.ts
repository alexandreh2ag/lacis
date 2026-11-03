import { applyTheme, readTheme } from './theme'

test('a theme picked is kept and set on the root; the system one leaves both out', () => {
  expect(readTheme()).toBe('system')
  applyTheme('dark')
  expect(document.documentElement.dataset.theme).toBe('dark')
  expect(readTheme()).toBe('dark')
  applyTheme('light')
  expect(document.documentElement.dataset.theme).toBe('light')
  applyTheme('system')
  expect(document.documentElement.dataset.theme).toBeUndefined()
  expect(readTheme()).toBe('system')
  localStorage.setItem('lacis-theme', 'pink')
  expect(readTheme()).toBe('system')
})
