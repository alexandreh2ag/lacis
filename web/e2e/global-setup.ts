import { execFileSync, spawn } from 'node:child_process'
import { mkdirSync, openSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { admin, baseURL, dsn, port, root, runDir } from './env'

// globalSetup builds lacis-server with the front of web/dist, gives it a
// database of its own, new, filled with the snapshots of
// testdata/snapshots, and starts it; the function it returns stops it.
// LACIS_E2E_SERVER names a server already built.
export default async function globalSetup() {
  mkdirSync(runDir, { recursive: true })
  const server = resolve(process.env.LACIS_E2E_SERVER ?? `${runDir}/lacis-server`)
  if (!process.env.LACIS_E2E_SERVER) {
    execFileSync('go', ['build', '-o', server, '.'], { cwd: root, stdio: 'inherit' })
  }

  // The database is dropped and created again from the postgres database
  // of the same server.
  const url = new URL(dsn)
  const name = url.pathname.slice(1)
  if (!/^[a-z0-9_]+$/.test(name)) throw new Error(`unexpected database name ${name}`)
  url.pathname = '/postgres'
  execFileSync('psql', [url.toString(), '-q', '-c', `drop database if exists ${name} with (force)`, '-c', `create database ${name}`], { stdio: 'inherit' })

  const config = `${runDir}/server.yaml`
  writeFileSync(config, `listen:
  address: 127.0.0.1:${port}
  public_url: ${baseURL}
database:
  dsn: ${dsn}
  migrate_on_start: true
auth:
  session:
    secret: e2e-only-session-secret-0123456789abcdef
  local:
    enabled: true
    bootstrap_admin_username: ${admin.username}
    bootstrap_admin_password: ${admin.password}
workers:
  enabled: true
linking:
  cdn_ranges: ["104.16.0.0/13"]
log:
  level: warn
`)
  const run = (...args: string[]) => execFileSync(server, [...args, '-c', runDir], { cwd: root, stdio: ['ignore', 'ignore', 'inherit'] })
  run('db', 'migrate')
  run('snapshot', 'import', 'testdata/snapshots')
  run('snapshot', 'normalize')

  const log = openSync(`${runDir}/server.log`, 'w')
  const child = spawn(server, ['start', '-c', runDir], { cwd: root, stdio: ['ignore', log, log] })
  let exited = false
  child.on('exit', () => { exited = true })
  for (let i = 0; ; i++) {
    if (exited) throw new Error(`lacis-server stopped, see ${runDir}/server.log`)
    try {
      if ((await fetch(`${baseURL}healthz`)).ok) break
    } catch { /* not listening yet */ }
    if (i > 100) throw new Error(`lacis-server does not answer on ${baseURL}, see ${runDir}/server.log`)
    await new Promise((r) => setTimeout(r, 200))
  }
  return () => { child.kill('SIGTERM') }
}
