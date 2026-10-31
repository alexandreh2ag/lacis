import type { Messages } from '../fr'

export const common: Messages['common'] = {
  many: (n: number) => n !== 1,
  days: 'd',
  hours: 'h',
  minutes: 'min',
  confidence: { EXACT: 'exact', INFERRED: 'inferred', AMBIGUOUS: 'ambiguous' },
  status: { ACTIVE: 'Active', STALE: 'Stale', INACTIVE: 'Inactive' },
  loadingGraph: 'Loading the graph…',
  nav: {
    graph: 'Graph', projects: 'Projects', hosts: 'Hosts', apps: 'Apps', services: 'Services', history: 'History',
    settings: 'Settings', sections: 'Sections',
  },
  account: {
    of: (name: string) => `Account of ${name}`,
    theme: 'Theme',
    themes: { system: 'System', light: 'Light', dark: 'Dark' },
    language: 'Language',
    languages: { auto: 'Browser', fr: 'Français', en: 'English' },
    administration: 'Administration',
    logout: 'Log out',
  },
}
