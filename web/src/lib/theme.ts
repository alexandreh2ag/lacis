import { useState } from 'react'

// Theme is the look the user picked: the one of the system, light or
// dark. index.html applies it before the first paint.
export type Theme = 'system' | 'light' | 'dark'

const key = 'lacis-theme'

export function readTheme(): Theme {
  try {
    const t = localStorage.getItem(key)
    return t === 'light' || t === 'dark' ? t : 'system'
  } catch {
    return 'system'
  }
}

// applyTheme sets data-theme on the root, where the tokens read it; the
// system theme leaves it out, for prefers-color-scheme to decide.
export function applyTheme(theme: Theme) {
  if (theme === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = theme
  try {
    if (theme === 'system') localStorage.removeItem(key)
    else localStorage.setItem(key, theme)
  } catch {
    // Without storage, the theme holds until the page reloads.
  }
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setTheme] = useState(readTheme)
  return [theme, (t) => { applyTheme(t); setTheme(t) }]
}
