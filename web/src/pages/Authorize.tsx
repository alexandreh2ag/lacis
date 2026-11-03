import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { Logo } from '@/components/Layout'
import { config } from '@/config'
import { useT } from '@/i18n'

// Consent is what the server tells of an authorization request.
interface Consent {
  client_name: string
  redirect_host: string
  user: { username: string; first_name?: string; last_name?: string }
}

type State =
  | { kind: 'loading' }
  | { kind: 'consent'; consent: Consent }
  | { kind: 'expired' }
  | { kind: 'invalid' }
  | { kind: 'failed' }

// loginBack is the login page coming back to this request.
function loginBack(request: string) {
  return `/login?redirect=${encodeURIComponent(`/authorize?request=${encodeURIComponent(request)}`)}`
}

// Authorize asks the user to let an assistant, such as Claude, read Lacis
// through MCP: the OAuth server of Lacis sends the browser here, the user
// logs in first if needed, then the answer goes back to the assistant.
export function Authorize() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const t = useT().pages.authorize
  const request = params.get('request') ?? ''
  const [state, setState] = useState<State>(params.get('error') || !request ? { kind: 'invalid' } : { kind: 'loading' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (state.kind !== 'loading') return
    let live = true
    fetch(`oauth/request?request=${encodeURIComponent(request)}`, { credentials: 'same-origin' })
      .then(async (res) => {
        if (!live) return
        if (res.status === 401) {
          navigate(loginBack(request), { replace: true })
          return
        }
        if (!res.ok) {
          setState({ kind: res.status === 400 ? 'expired' : 'failed' })
          return
        }
        setState({ kind: 'consent', consent: (await res.json()) as Consent })
      })
      .catch(() => { if (live) setState({ kind: 'failed' }) })
    return () => { live = false }
  }, [state.kind, request, navigate])

  const answer = async (approve: boolean) => {
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('oauth/authorize', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request, approve }),
      })
      if (res.ok) {
        const { redirect } = (await res.json()) as { redirect: string }
        window.location.assign(redirect)
        return
      }
      if (res.status === 401) {
        navigate(loginBack(request), { replace: true })
        return
      }
      if (res.status === 400) {
        setState({ kind: 'expired' })
        return
      }
      setError(t.failed)
    } catch {
      setError(t.failed)
    } finally {
      setBusy(false)
    }
  }

  const switchAccount = async () => {
    await fetch('auth/logout', { method: 'POST', credentials: 'same-origin' }).catch(() => undefined)
    navigate(loginBack(request), { replace: true })
  }

  let body: React.ReactNode
  switch (state.kind) {
    case 'loading':
      body = <p className="lc-muted m-0">{t.loading}</p>
      break
    case 'expired':
    case 'invalid':
    case 'failed':
      body = <div className="lc-badge lc-badge--error h-auto py-2 leading-[18px]" role="alert">{t[state.kind]}</div>
      break
    case 'consent': {
      const { consent } = state
      const name = [consent.user.first_name, consent.user.last_name].filter(Boolean).join(' ')
      body = (
        <>
          <h1 className="m-0 text-lg font-semibold">{t.title(consent.client_name)}</h1>
          <p className="m-0">{t.intro(consent.client_name)}</p>
          <div className="flex flex-col gap-0.5 rounded-md border border-border px-3 py-2 text-[13px]">
            <span className="lc-muted">{t.as}</span>
            <span className="font-semibold">{name ? `${name} · ` : ''}<span className="font-mono">{consent.user.username}</span></span>
            <span className="text-[12.5px]">
              <span className="lc-muted">{t.notYou} </span>
              <button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-brand underline" onClick={() => void switchAccount()}>
                {t.switchAccount}
              </button>
            </span>
          </div>
          <div className="flex flex-col gap-1.5 text-[13px]">
            <p className="m-0"><span className="font-semibold">{t.can}</span> {t.canRead}</p>
            <p className="m-0"><span className="font-semibold">{t.cannot}</span> {t.cannotWrite}</p>
          </div>
          <p className="m-0 text-[13px]">{t.redirect(consent.redirect_host)}</p>
          <p className="lc-muted m-0 text-xs">{t.revoke}</p>
          {error ? <div className="lc-badge lc-badge--error h-auto py-2 leading-[18px]" role="alert">{error}</div> : null}
          <div className="flex gap-2">
            <button type="button" className="lc-btn h-10 flex-1 justify-center" disabled={busy} onClick={() => void answer(false)}>{t.deny}</button>
            <button type="button" className="lc-btn lc-btn--primary h-10 flex-1 justify-center" disabled={busy} onClick={() => void answer(true)}>
              {busy ? t.sending : t.approve}
            </button>
          </div>
        </>
      )
    }
  }

  return (
    <div className="grid min-h-full place-items-center bg-surface-sunken px-4"
      style={{ backgroundImage: 'radial-gradient(var(--border-strong) 0.8px, transparent 0.9px)', backgroundSize: '16px 16px' }}>
      <div className="flex w-full max-w-[420px] flex-col gap-4 rounded-lg border border-border bg-surface-raised p-7 shadow-panel">
        <div className="flex items-center gap-2.5 text-xl font-bold"><Logo />Lacis</div>
        {body}
        <p className="lc-muted m-0 text-xs">Lacis {config.version}</p>
      </div>
    </div>
  )
}
