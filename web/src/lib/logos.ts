import {
  siAngular, siApache, siApachekafka, siApachetomcat, siCaddy, siClickhouse, siCloudflare, siDebian, siDjango, siDocker, siDotnet,
  siDrupal, siElasticsearch, siExpress, siGo, siGrafana, siHaproxy, siInfluxdb, siKeycloak, siLaravel, siMariadb, siMeilisearch,
  siMinio, siMongodb, siMysql, siNestjs, siNextdotjs, siNginx, siNodedotjs, siNuxt, siOpenjdk, siOpensearch, siPhp,
  siPostgresql, siPrestashop, siPrometheus, siPython, siRabbitmq, siReact, siRedis, siRubyonrails, siSentry, siSpring, siStrapi,
  siSymfony, siTraefikproxy, siTypo3, siUbuntu, siVuedotjs, siWordpress, siZend,
} from 'simple-icons'

interface Icon {
  path: string
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
  traefik: siTraefikproxy, typo3: siTypo3, ubuntu: siUbuntu, vue: siVuedotjs, wordpress: siWordpress, zend: siZend,
  zend1: siZend,
}

// Targets known by their domain: the external targets show their service.
const domains: [RegExp, string][] = [
  [/(^|\.)sentry\.io$/, 'sentry'],
  [/(^|\.)ingest\.sentry\.io$/, 'sentry'],
  [/(^|\.)cloudflare\.com$/, 'cloudflare'],
]

export function logoOf(tech: string | null | undefined): Icon | undefined {
  return tech ? logos[tech.toLowerCase()] : undefined
}

// defaultTechs are the technologies the front has a logo of.
export const defaultTechs = Object.keys(logos).sort()

// custom are the logos uploaded in the administration, by technology:
// they show in place of the ones above. The session loads them.
let custom = new Map<string, string>()

export function setCustomIcons(icons: { tech: string; url: string }[]) {
  custom = new Map(icons.map((i) => [i.tech, i.url]))
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
