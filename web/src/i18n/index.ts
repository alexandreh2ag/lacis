import { createContext, useContext } from 'react'
import { en } from './en'
import { fr, type Messages } from './fr'

// Lang is a language of the front. The one picked in the account menu is
// kept in the browser; without it, the first of the browser that the
// front speaks, else English.
export type Lang = 'fr' | 'en'

export const langs: Lang[] = ['fr', 'en']

const key = 'lacis-lang'
const messages: Record<Lang, Messages> = { fr, en }

// picked is the language stored, null when the browser decides.
export function picked(): Lang | null {
  try {
    const l = localStorage.getItem(key)
    return l === 'fr' || l === 'en' ? l : null
  } catch {
    return null
  }
}

export function detect(languages: readonly string[] = navigator.languages ?? [navigator.language]): Lang {
  for (const l of languages) {
    const base = l.toLowerCase().split('-')[0]
    if (base === 'fr' || base === 'en') return base
  }
  return 'en'
}

let current: Lang | null = null

// lang is the language the front speaks now.
export function lang(): Lang {
  current ??= picked() ?? detect()
  return current
}

// pick stores the language; null gives the choice back to the browser.
export function pick(l: Lang | null) {
  try {
    if (l) localStorage.setItem(key, l)
    else localStorage.removeItem(key)
  } catch {
    // Without storage, the language holds until the page reloads.
  }
  current = l ?? detect()
  document.documentElement.lang = current
}

// t is the text of the language spoken now, for the code outside React
// as well; the app is drawn again when the language changes.
export function t(): Messages {
  return messages[lang()]
}

export const LangContext = createContext<{ lang: Lang; picked: Lang | null; pick: (l: Lang | null) => void }>({
  lang: 'fr', picked: null, pick: () => {},
})

// useT gives the text of the language spoken, and draws the component
// again when it changes.
export function useT(): Messages {
  useContext(LangContext)
  return t()
}

export function useLang() {
  return useContext(LangContext)
}
