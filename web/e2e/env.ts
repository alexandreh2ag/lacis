import { fileURLToPath } from 'node:url'

// The repository, and the directory where the tests keep what they run:
// the server, its configuration, its log, the sessions.
export const root = fileURLToPath(new URL('../..', import.meta.url))
export const runDir = fileURLToPath(new URL('./.run', import.meta.url))

export const port = Number(process.env.LACIS_E2E_PORT ?? 8643)
export const baseURL = `http://127.0.0.1:${port}/`

// The database the tests drop and create again on each run: never the one
// of a server in use.
export const dsn = process.env.LACIS_E2E_DATABASE_DSN ?? 'postgres://lacis:lacis@localhost:5432/lacis_e2e?sslmode=disable'

export const admin = { username: 'admin', password: 'e2e-admin-password' }
export const adminState = `${runDir}/admin.json`
