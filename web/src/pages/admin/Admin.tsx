import { Navigate } from 'react-router'
import { Page } from '@/components/Layout'
import { Tabs, useTab, type Tab } from '@/components/Tabs'
import { useT } from '@/i18n'
import { useSession } from '@/session'
import { Icons } from './Icons'
import { Roles } from './Roles'
import { Tasks } from './Tasks'
import { Agents, Tokens } from './Tokens'
import { Users } from './Users'

// Admin administers what belongs to no namespace: users, roles, enrollment
// tokens, agents, the logos of the technologies and the scheduled tasks, a
// tab each the user may read.
export function Admin() {
  const { access } = useSession()
  const t = useT()
  const l = t.admin.tabs
  const tabs: Tab[] = [
    ...(access.users.read ? [{ id: 'users', label: l.users }] : []),
    ...(access.roles.read ? [{ id: 'roles', label: l.roles }] : []),
    ...(access.tokens.read ? [{ id: 'tokens', label: l.tokens }, { id: 'agents', label: l.agents }] : []),
    ...(access.icons.read ? [{ id: 'icons', label: l.icons }] : []),
    ...(access.tasks.read ? [{ id: 'tasks', label: l.tasks }] : []),
  ]
  const [tab, setTab] = useTab(tabs, tabs[0]?.id ?? '')
  if (!tabs.length) return <Navigate to="/" replace />
  return (
    <Page title={t.admin.title}>
      <Tabs tabs={tabs} current={tab} onChange={setTab} label={t.admin.title}>
        {tab === 'users' ? <Users write={access.users.write} /> : null}
        {tab === 'roles' ? <Roles write={access.roles.write} /> : null}
        {tab === 'tokens' ? <Tokens write={access.tokens.write} /> : null}
        {tab === 'agents' ? <Agents write={access.tokens.write} /> : null}
        {tab === 'icons' ? <Icons write={access.icons.write} /> : null}
        {tab === 'tasks' ? <Tasks write={access.tasks.write} /> : null}
      </Tabs>
    </Page>
  )
}

// canAdminister tells whether the user may open the administration.
export function canAdminister(access: { users: { read: boolean }; roles: { read: boolean }; tokens: { read: boolean }; icons: { read: boolean }; tasks: { read: boolean } }): boolean {
  return access.users.read || access.roles.read || access.tokens.read || access.icons.read || access.tasks.read
}
