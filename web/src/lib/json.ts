// Guards on the JSON the agents send, which the server passes on as is:
// a field of the wrong type is left out instead of breaking the page.

export type JsonObject = Record<string, unknown>

export function isObject(v: unknown): v is JsonObject {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

// field is the value under key of an object, undefined for anything else.
export function field(v: unknown, key: string): unknown {
  return isObject(v) ? v[key] : undefined
}

export function str(v: unknown): string | undefined {
  return typeof v === 'string' ? v : undefined
}

// num is a finite number.
export function num(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

// strings are the strings of an array, the rest left out.
export function strings(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

export interface Resources {
  cores?: number
  memoryMb?: number
  disks: { mount: string; sizeGb: number }[]
}

// resourcesOf reads the resources of a host: its CPU, memory and disks.
export function resourcesOf(v: unknown): Resources {
  const disks = field(v, 'disks')
  return {
    cores: num(field(field(v, 'cpu'), 'effective_cores')),
    memoryMb: num(field(field(v, 'memory_mb'), 'effective')),
    disks: (Array.isArray(disks) ? disks : []).flatMap((d: unknown) => {
      const mount = str(field(d, 'mount'))
      const sizeGb = num(field(d, 'size_gb'))
      return mount !== undefined && sizeGb !== undefined ? [{ mount, sizeGb }] : []
    }),
  }
}

export interface LauncherTask {
  id: string
  schedule?: string
  // command is sent only by an agent sending whole commands.
  command?: string
}

export interface Launcher {
  type: string
  name: string
  role: string
  command?: string
  directory?: string
  user?: string
  tasks: LauncherTask[]
}

// launchersOf reads the launchers of an app: the programs starting it,
// such as supervisor ones, and the tasks they schedule. A launcher without
// a name, or a task without an id, is left out.
export function launchersOf(v: unknown): Launcher[] {
  return (Array.isArray(v) ? v : []).flatMap((l: unknown) => {
    const name = str(field(l, 'name'))
    if (!name) return []
    const tasks = field(l, 'tasks')
    return [{
      type: str(field(l, 'type')) ?? '',
      name,
      role: str(field(l, 'role')) ?? '',
      command: str(field(l, 'command')),
      directory: str(field(l, 'directory')),
      user: str(field(l, 'user')),
      tasks: (Array.isArray(tasks) ? tasks : []).flatMap((t: unknown) => {
        const id = str(field(t, 'id'))
        return id ? [{ id, schedule: str(field(t, 'schedule')), command: str(field(t, 'command')) }] : []
      }),
    }]
  })
}
