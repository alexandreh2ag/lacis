import {
  siAngular, siApache, siApachekafka, siApachetomcat, siCaddy, siClickhouse, siCloudflare, siDebian, siDjango, siDocker, siDotnet,
  siDrupal, siElasticsearch, siExpress, siGo, siGrafana, siHaproxy, siInfluxdb, siKeycloak, siLaravel, siMariadb, siMeilisearch,
  siMinio, siMongodb, siMysql, siNestjs, siNextdotjs, siNginx, siNodedotjs, siNuxt, siOpenjdk, siOpensearch, siPhp,
  siPostgresql, siPrestashop, siPrometheus, siPython, siRabbitmq, siReact, siRedis, siRubyonrails, siSentry, siSpring, siStrapi,
  siSymfony, siTraefikproxy, siTypo3, siUbuntu, siVault, siVuedotjs, siWordpress, siZend,
} from 'simple-icons'

interface Icon {
  path: string
}

// extraLogos are technologies Simple Icons has no logo of, drawn for
// Lacis as plain marks on the same 24 × 24 grid: a V for Varnish, a
// bucket for S3 and the stores compatible with it, a grain silo for Silo
// (the community fork of MinIO).
const extraLogos: Record<string, Icon> = {
  silo: { path: 'M6 8.5a6 6 0 0 1 12 0V9H6zM6 10.5h12v4H6zM6 16h12v5.5H6z' },
  varnish: { path: 'M2.5 3h4.6L12 15.2 16.9 3h4.6L14.3 21H9.7z' },
  s3: { path: 'M3 5.5C3 3.57 7.03 2 12 2s9 1.57 9 3.5c0 .14-.02.28-.06.42L18.9 19.6C18.6 21 15.6 22 12 22s-6.6-1-6.9-2.4L3.06 5.92A1.6 1.6 0 0 1 3 5.5Zm2.3.3c.7.9 3.3 1.7 6.7 1.7s6-.8 6.7-1.7C18 4.9 15.4 4.1 12 4.1s-6 .8-6.7 1.7Z' },
}

// The logos of the technologies Lacis reads, by the names the agent gives
// them (Simple Icons, CC0). A technology without logo shows its initials.
const logos: Record<string, Icon> = {
  angular: siAngular, apache: siApache, httpd: siApache, kafka: siApachekafka, tomcat: siApachetomcat, caddy: siCaddy,
  clickhouse: siClickhouse, cloudflare: siCloudflare, debian: siDebian, django: siDjango, docker: siDocker, dotnet: siDotnet,
  drupal: siDrupal, elasticsearch: siElasticsearch, express: siExpress, go: siGo, grafana: siGrafana, haproxy: siHaproxy,
  influxdb: siInfluxdb, keycloak: siKeycloak, laravel: siLaravel, mariadb: siMariadb, meilisearch: siMeilisearch, minio: siMinio,
  mongodb: siMongodb, mysql: siMysql, nestjs: siNestjs, next: siNextdotjs, nextjs: siNextdotjs, nginx: siNginx, node: siNodedotjs,
  nodejs: siNodedotjs, nuxt: siNuxt, java: siOpenjdk, opensearch: siOpensearch, php: siPhp, 'php-fpm': siPhp, postgresql: siPostgresql,
  postgres: siPostgresql, prestashop: siPrestashop, prometheus: siPrometheus, python: siPython, rabbitmq: siRabbitmq, react: siReact,
  redis: siRedis, rails: siRubyonrails, sentry: siSentry, spring: siSpring, strapi: siStrapi, symfony: siSymfony,
  traefik: siTraefikproxy, typo3: siTypo3, ubuntu: siUbuntu, vault: siVault, vue: siVuedotjs, wordpress: siWordpress, zend: siZend,
  zend1: siZend, ...extraLogos,
}

// Targets known by their domain: the external targets show their service.
const domains: [RegExp, string][] = [
  [/(^|\.)sentry\.io$/, 'sentry'],
  [/(^|\.)ingest\.sentry\.io$/, 'sentry'],
  [/(^|\.)cloudflare\.com$/, 'cloudflare'],
  // S3 and the stores compatible with it: AWS, OVHcloud, Scaleway.
  [/(^|\.)s3[.-]([a-z0-9-]+\.)*amazonaws\.com$/, 's3'],
  [/(^|\.)s3\.[a-z0-9-]+\.io\.cloud\.ovh\.net$/, 's3'],
  [/(^|\.)s3\.[a-z0-9-]+\.scw\.cloud$/, 's3'],
]

export function logoOf(tech: string | null | undefined): Icon | undefined {
  return tech ? logos[tech.toLowerCase()] : undefined
}

// defaultTechs are the technologies the front has a logo of.
export const defaultTechs = Object.keys(logos).sort()

// custom are the logos uploaded in the administration, by technology:
// they show in place of the ones above. The session loads them.
let custom = new Map<string, string>()

let version = 0
const listeners = new Set<() => void>()

export function setCustomIcons(icons: { tech: string; url: string }[]) {
  custom = new Map(icons.map((i) => [i.tech, i.url]))
  version++
  listeners.forEach((fn) => fn())
}

// subscribeIcons and iconsVersion let a logo render again when the
// uploaded logos change.
export function subscribeIcons(fn: () => void): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

export function iconsVersion(): number {
  return version
}

// customIconOf is the URL of the logo uploaded for the technology.
export function customIconOf(tech: string | null | undefined): string | undefined {
  return tech ? custom.get(tech.toLowerCase()) : undefined
}

// hasLogo tells a technology with a logo, uploaded or embedded.
export function hasLogo(tech: string | null | undefined): boolean {
  return !!customIconOf(tech) || !!logoOf(tech)
}

export function techOfDomain(target: string): string | undefined {
  return domains.find(([re]) => re.test(target))?.[1]
}

// initials of a technology without logo: haproxy without its logo gives HA.
export function initials(tech: string): string {
  return tech.replace(/[^a-z0-9]/gi, '').slice(0, 2).toUpperCase()
}
