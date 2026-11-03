import { useMutation } from '@apollo/client'
import { useEffect, useState } from 'react'
import { Field, useAction } from '@/components/Dialog'
import { Page } from '@/components/Layout'
import { Tabs, useTab, type Tab } from '@/components/Tabs'
import { config } from '@/config'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'
import { useSession, type Session } from '@/session'
import { Assistants } from './Assistants'

const UpdateProfileDoc = graphql(`
  mutation UpdateProfile($input: ProfileInput!) {
    updateProfile(input: $input) { id firstName lastName }
  }
`)

const ChangePasswordDoc = graphql(`
  mutation ChangePassword($currentPassword: String!, $newPassword: String!) {
    changePassword(currentPassword: $currentPassword, newPassword: $newPassword)
  }
`)

// Profile is the page of one's own account, whatever the rights: the
// names, the password and the MCP access. An SSO user gets a first
// password from an administrator only, then changes it here.
export function Profile() {
  const { me } = useSession()
  const t = useT().pages.profile
  const tabs: Tab[] = [
    { id: 'general', label: t.tabs.general },
    { id: 'password', label: t.tabs.password },
    ...(config.mcp.enabled ? [{ id: 'mcp', label: t.tabs.mcp }] : []),
  ]
  const [tab, setTab] = useTab(tabs, 'general')
  return (
    <Page title={t.title}>
      <Tabs tabs={tabs} current={tab} onChange={setTab} label={t.title}>
        {tab === 'general' ? <General me={me} /> : null}
        {tab === 'password' ? (me.local ? <Password /> : <p className="lc-muted m-0 max-w-[600px]">{t.noPassword}</p>) : null}
        {tab === 'mcp' ? <Assistants /> : null}
      </Tabs>
    </Page>
  )
}

export function General({ me }: { me: Session['me'] }) {
  const [update] = useMutation(UpdateProfileDoc)
  const t = useT().pages.profile
  const a = useAction()
  const [form, setForm] = useState({ firstName: me.firstName ?? '', lastName: me.lastName ?? '' })
  const [saved, setSaved] = useState(false)
  useEffect(() => setSaved(false), [form])
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const save = async () => {
    await update({ variables: { input: form } })
    setSaved(true)
  }
  return (
    <form className="flex max-w-[720px] flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void a.run(save) }}>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <div className="lc-fields">
        <Field label={t.username} hint={t.usernameHint}>
          {(id) => <input id={id} className="lc-input lc-mono" readOnly value={me.username} />}
        </Field>
        <div className="flex flex-col gap-1.5">
          <span className="lc-label">{t.login}</span>
          <span className="py-1.5">{[me.local ? t.logins.local : null, me.oidc ? t.logins.oidc : null].filter(Boolean).join(', ')}</span>
        </div>
      </div>
      <div className="lc-fields">
        <Field label={t.firstName}>
          {(id) => <input id={id} className="lc-input" maxLength={100} autoComplete="given-name" value={form.firstName} onChange={set('firstName')} />}
        </Field>
        <Field label={t.lastName}>
          {(id) => <input id={id} className="lc-input" maxLength={100} autoComplete="family-name" value={form.lastName} onChange={set('lastName')} />}
        </Field>
      </div>
      {me.oidc ? <span className="lc-field__hint -mt-2">{t.ssoNames}</span> : null}
      <div className="flex items-center gap-3">
        <button type="submit" className="lc-btn lc-btn--primary" disabled={a.busy}>{a.busy ? t.saving : t.save}</button>
        {saved ? <span className="text-[13px] text-status-ok" role="status">{t.saved}</span> : null}
      </div>
    </form>
  )
}

export function Password() {
  const [change] = useMutation(ChangePasswordDoc)
  const t = useT().pages.profile
  const a = useAction()
  const empty = { current: '', password: '', confirm: '' }
  const [form, setForm] = useState(empty)
  const [changed, setChanged] = useState(false)
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setChanged(false)
    setForm({ ...form, [k]: e.target.value })
  }
  const differs = form.confirm !== '' && form.confirm !== form.password
  const save = async () => {
    if (form.confirm !== form.password) throw new Error(t.mismatch)
    await change({ variables: { currentPassword: form.current, newPassword: form.password } })
    setForm(empty)
    setChanged(true)
  }
  return (
    <form className="flex max-w-[480px] flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void a.run(save) }}>
      <p className="lc-muted m-0 text-[13px]">{t.passwordIntro}</p>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <Field label={t.currentPassword}>
        {(id) => <input id={id} className="lc-input" type="password" required maxLength={1024} autoComplete="current-password"
          value={form.current} onChange={set('current')} />}
      </Field>
      <Field label={t.newPassword} hint={t.newPasswordHint}>
        {(id) => <input id={id} className="lc-input" type="password" required minLength={8} maxLength={1024} autoComplete="new-password"
          value={form.password} onChange={set('password')} />}
      </Field>
      <Field label={t.confirmPassword} hint={differs ? <span className="text-danger">{t.mismatch}</span> : undefined}>
        {(id) => <input id={id} className="lc-input" type="password" required autoComplete="new-password" aria-invalid={differs}
          value={form.confirm} onChange={set('confirm')} />}
      </Field>
      <div className="flex items-center gap-3">
        <button type="submit" className="lc-btn lc-btn--primary" disabled={a.busy}>{a.busy ? t.saving : t.change}</button>
        {changed ? <span className="text-[13px] text-status-ok" role="status">{t.changed}</span> : null}
      </div>
    </form>
  )
}
