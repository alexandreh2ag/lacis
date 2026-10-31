// What lacis-server writes into index.html: the base path the front is
// served under, its version, the ways to log in and the limits of the logo
// of a technology.
export interface LacisConfig {
  basePath: string
  version: string
  auth: { local: boolean; oidc: boolean }
  icons: IconLimits
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
