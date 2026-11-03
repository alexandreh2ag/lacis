// What lacis-server writes into index.html: the base path the front is
// served under, its version, the ways to log in, the limits of the logo
// of a technology and the MCP endpoint.
export interface LacisConfig {
  basePath: string
  version: string
  auth: { local: boolean; oidc: boolean }
  icons: IconLimits
  // mcp is the MCP endpoint assistants such as Claude connect to; url is
  // empty when mcp is off.
  mcp: { enabled: boolean; url: string }
}

export interface IconLimits {
  maxSize: number
  minSide: number
  maxSide: number
  extensions: string[]
}

const defaults: LacisConfig = {
  basePath: '/', version: 'dev', auth: { local: true, oidc: false },
  icons: { maxSize: 256 * 1024, minSide: 16, maxSide: 512, extensions: ['.jpeg', '.jpg', '.png', '.svg', '.webp'] },
  mcp: { enabled: false, url: '' },
}

function read(): LacisConfig {
  const el = document.getElementById('lacis-config')
  try {
    return { ...defaults, ...(JSON.parse(el?.textContent ?? '{}') as Partial<LacisConfig>) }
  } catch {
    return defaults
  }
}

export const config = read()

// basename is the base path as React Router takes it: "" or "/lacis".
export const basename = config.basePath.replace(/\/$/, '')

// REFRESH_MS is how often the graph and the pages of a host, an app or a
// project read the inventory again, while the tab is shown.
export const REFRESH_MS = 60_000

// refresh are the options of a query kept fresh that way.
export const refresh = { pollInterval: REFRESH_MS, skipPollAttempt: () => document.hidden }
