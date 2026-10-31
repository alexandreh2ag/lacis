import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useApolloClient } from '@apollo/client'
import { Logo } from '@/components/Layout'
import { config } from '@/config'
import { useT } from '@/i18n'

// safeRedirect keeps a page of the front.
function safeRedirect(target: string | null): string {
  return target && target.startsWith('/') && !target.startsWith('//') ? target : '/'
}

export function Login() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const client = useApolloClient()
  const t = useT()
  const tl = t.pages.login
  const redirect = safeRedirect(params.get('redirect'))
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(tl.oidc[params.get('error') ?? ''] ?? null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('auth/login', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })
      if (res.ok) {
        // The session lives in a cookie: check the browser kept it before
        // leaving, or the next page would bring back here without a word.
        const check = await fetch('graphql', {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query: '{ me { id } }' }),
        })
        if (!check.ok) {
          setError(window.location.protocol === 'http:' ? tl.cookieHttp : tl.cookieRefused)
          return
        }
        await client.clearStore()
        navigate(redirect, { replace: true })
        return
      }
      setError(res.status === 401 ? tl.badCredentials
        : res.status === 429 ? tl.tooMany
          : res.status === 403 ? tl.localDisabled
            : tl.failed)
    } catch {
      setError(tl.unreachable)
    } finally {
      setBusy(false)
    }
  }

  const sso = `auth/oidc/login?redirect=${encodeURIComponent(config.basePath.replace(/\/$/, '') + redirect)}`
  return (
    <div className="grid min-h-full place-items-center bg-surface-sunken px-4"
      style={{ backgroundImage: 'radial-gradient(var(--border-strong) 0.8px, transparent 0.9px)', backgroundSize: '16px 16px' }}>
      <form onSubmit={(e) => void submit(e)} className="flex w-full max-w-[380px] flex-col gap-4 rounded-lg border border-border bg-surface-raised p-7 shadow-panel">
        <div className="flex items-center gap-2.5 text-xl font-bold"><Logo />Lacis</div>
        <p className="lc-muted m-0">{tl.intro}</p>
        {error ? <div className="lc-badge lc-badge--error h-auto py-2 leading-[18px]" role="alert">{error}</div> : null}
        {config.auth.local ? (
          <>
            <label htmlFor="username" className="lc-label">{tl.username}</label>
            <input className="lc-input -mt-2.5" id="username" type="text" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={username} onChange={(e) => setUsername(e.target.value)} />
            <label htmlFor="password" className="lc-label">{tl.password}</label>
            <input className="lc-input -mt-2.5" id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            <button className="lc-btn lc-btn--primary h-10 justify-center" type="submit" disabled={busy}>{busy ? tl.submitting : tl.submit}</button>
          </>
        ) : null}
        {config.auth.local && config.auth.oidc ? (
          <div className="flex items-center gap-2.5 text-xs text-ink-muted">
            <span className="h-px flex-1 bg-border" />{tl.or}<span className="h-px flex-1 bg-border" />
          </div>
        ) : null}
        {config.auth.oidc ? <a className="lc-btn h-10 justify-center no-underline" href={sso}>{tl.sso}</a> : null}
        <p className="lc-muted m-0 text-xs">Lacis {config.version}</p>
      </form>
    </div>
  )
}
