export const common = {
  // plural writes the word of a count: in French, 0 and 1 take the singular.
  many: (n: number) => n > 1,
  days: 'j',
  hours: 'h',
  minutes: 'min',
  confidence: { EXACT: 'exact', INFERRED: 'déduit', AMBIGUOUS: 'ambigu' } as Record<string, string>,
  status: { ACTIVE: 'Actif', STALE: 'Stale', INACTIVE: 'Inactif' } as Record<string, string>,
  loadingGraph: 'Chargement du graphe…',
  nav: {
    graph: 'Graphe', projects: 'Projets', hosts: 'Hosts', apps: 'Apps', services: 'Services', history: 'Historique',
    settings: 'Paramètres', sections: 'Sections',
  },
  account: {
    of: (name: string) => `Compte de ${name}`,
    theme: 'Thème',
    themes: { system: 'Système', light: 'Clair', dark: 'Sombre' },
    language: 'Langue',
    languages: { auto: 'Navigateur', fr: 'Français', en: 'English' },
    administration: 'Administration',
    logout: 'Se déconnecter',
  },
}
