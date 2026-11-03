import { Fragment, useState } from 'react'
import { lang, LangContext, pick, picked, type Lang } from '.'

// LangProvider draws its children again, from scratch, in the language
// picked: the text read outside React follows too.
export function LangProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState(() => {
    document.documentElement.lang = lang()
    return { lang: lang(), picked: picked() }
  })
  const value = {
    ...state,
    pick: (l: Lang | null) => {
      pick(l)
      setState({ lang: lang(), picked: l })
    },
  }
  return <LangContext.Provider value={value}><Fragment key={state.lang}>{children}</Fragment></LangContext.Provider>
}
