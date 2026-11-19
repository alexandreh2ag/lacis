import {
  siAngular, siApache, siApachekafka, siApachetomcat, siCaddy, siClickhouse, siCloudflare, siDebian, siDjango, siDocker, siDotnet,
  siDrupal, siElasticsearch, siExpress, siGo, siGrafana, siHaproxy, siInfluxdb, siKeycloak, siLaravel, siMariadb, siMeilisearch,
  siMinio, siMongodb, siMysql, siNestjs, siNextdotjs, siNginx, siNodedotjs, siNuxt, siOpenjdk, siOpensearch, siPhp,
  siPostgresql, siPrestashop, siPrometheus, siPython, siRabbitmq, siReact, siRedis, siRubyonrails, siSentry, siSpring, siStrapi,
  siSymfony, siTraefikproxy, siTypo3, siUbuntu, siVault, siVuedotjs, siWordpress, siZend,
} from 'simple-icons'

interface Icon {
  path: string
  // viewBox is the grid of the path, 24 × 24 when not given.
  viewBox?: string
  // layers, when given, are drawn in place of path: the facets of a logo
  // shaded in several tones, each drawn in the colour of the logo at its
  // opacity.
  layers?: { d: string; opacity: number }[]
}

// extraLogos are technologies Simple Icons has no logo of, drawn for
// Lacis as plain marks on the same 24 × 24 grid: a V for Varnish, a
// bucket for S3 and the stores compatible with it, a grain silo for Silo
// (the community fork of MinIO). Sulu and Akeneo are their own logos, on
// their own grid; the facets of Akeneo keep their tones as opacities.
const extraLogos: Record<string, Icon> = {
  silo: { path: 'M6 8.5a6 6 0 0 1 12 0V9H6zM6 10.5h12v4H6zM6 16h12v5.5H6z' },
  sulu: {
    viewBox: '0 0 128 128',
    path: 'M87.219 16.403a3.6 3.6 0 0 0-1.415.183L2.748 42.803C.512 43.443-.444 45.68.195 47.6l15.97 44.123c.64 1.918 3.196 4.156 5.113 4.476l75.39 15.347c2.237.32 4.791-.635 6.069-2.553l24.598-38.37c1.278-1.919.64-4.16-1.277-5.438l-30.03-20.78c-1.917-1.278-3.51-3.84-3.83-6.078L89.96 19.144c-.24-1.678-1.379-2.636-2.741-2.74zm-9.622 9.263c1.063.075 1.18.912.221 2.11L58.973 51.12c-1.277 1.599-4.152 2.878-6.388 2.238l-35.143-7.035c-2.236-.32-2.236-1.278 0-1.917l58.78-18.547c.558-.16 1.02-.218 1.375-.193m6.72 2.702c.715-.128 1.287.448 1.487 1.647l7.03 65.228c.319 2.238-.64 2.554-1.918.635l-27.151-35.49c-1.278-1.599-1.278-4.475 0-6.074l19.165-24.94c.48-.6.959-.93 1.387-1.006M10.275 50.114q.36-.014.779.045l41.21 8.632c2.236.32 5.111 2.236 6.389 3.835l29.71 38.69c1.278 1.6.639 2.878-1.597 2.239L25.749 91.402c-2.237-.32-4.473-2.558-5.112-4.476L8.5 53.038c-.56-1.679.101-2.867 1.774-2.924Zm84.874.995c.489-.043 1.122.167 1.84.647l21.72 15.031c1.916 1.28 2.24 3.834 1.281 5.433l-18.21 28.135c-1.277 1.918-2.238 1.602-2.558-.636L94.11 53.36c-.2-1.4.225-2.178 1.04-2.25z',
  },
  akeneo: {
    viewBox: '-5 .5 331 331',
    path: 'm320.2 137.8c0-.9-.1-1.8-.1-2.8v-.1c-.1-1-.2-1.9-.3-2.9 0-.2-.1-.5-.1-.7-.1-.9-.3-1.8-.4-2.7v-.2c-.2-1-.4-1.9-.6-2.9-.1-.3-.1-.6-.2-.8-.2-.8-.4-1.6-.6-2.5-.1-.2-.1-.4-.2-.6-.3-.9-.6-1.9-.9-2.8-.1-.4-.2-.8-.4-1.1-.2-.7-.4-1.3-.7-2-.2-.6-.4-1.1-.6-1.7s-.5-1.3-.8-1.9c-.2-.5-.5-1.1-.7-1.7-.2-.4-.3-.8-.5-1.1-13.9-31.8-46.4-67.7-83.2-98.4-.6-.5-1.2-1-1.9-1.6-.6-.5-1.1-.9-1.7-1.4-1.2-1-2.5-2-3.7-3-.3-.2-.6-.4-.8-.7-1.6-1.3-4 0-3.8 2 2 30.8 1.2 60.7-2.6 85.2-4.7 31-17.1 66.9-33.7 99.3-.1.2-.2.3-.3.5.1-.2.2-.3.2-.5-31.6-3.7-63.6-11.5-90.2-22.3-3.8-1.5-7.5-3.1-11-4.8-10.2-4.8-21-10.7-32.1-17.5-.2-.1-.4-.2-.6-.4-.2-.1-.3-.2-.5-.3-12.9-8-26.2-17.1-39.2-27-1.6-1.3-4 0-3.8 2.1 0 .4.1.7.1 1.1s.1.7.1 1.1c.1.8.1 1.6.2 2.4 0 .2 0 .3.1.5 0 .4.1.7.1 1.1.2 1.7.3 3.3.5 5 .1.8.2 1.5.3 2.3.2 1.9.4 3.8.7 5.8l.3 2.1v.4c.3 2.4.6 4.7 1 7.1 0 .2 0 .5.1.7l.6 3.9c.1.3.1.7.2 1 .2 1.1.4 2.3.6 3.4 0 .2.1.4.1.6.4 2.5.8 4.9 1.3 7.4l.3 1.5c.4 1.9.7 3.9 1.1 5.8.1.5.2 1 .3 1.4.1.3.1.5.2.8.5 2.3 1 4.7 1.5 7 .1.4.2.9.3 1.3 1.8 7.9 3.9 15.6 6.1 22.9v.1c.3.9.6 1.9.9 2.8.1.4.3.8.4 1.3.2.7.4 1.3.7 2 .3.9.6 1.7.9 2.6.1.4.3.9.5 1.3.4 1.2.9 2.5 1.3 3.7 0 .1.1.2.1.3 1.6 4.3 3.3 8.5 5 12.5.1.1.1.3.2.4.5 1.2 1.1 2.4 1.6 3.5.1.1.1.3.2.4.6 1.3 1.2 2.5 1.8 3.8.8 1.5 1.5 3 2.3 4.4 0 .1 0 .1.1.2.1.3.3.5.4.8.6 1 1.1 2 1.7 3 .1.2.2.4.3.5.7 1.2 1.4 2.3 2.1 3.4 0 .1.1.1.1.2.1.2.2.3.3.5.6.9 1.2 1.8 1.7 2.7.4.5.7 1 1 1.5.3.4.7.9 1 1.3.1.2.2.3.4.5.3.4.6.8.9 1.1.1.1.2.3.3.4.3.4.7.8 1 1.2l1.5 1.8c1.1 1.3 2.3 2.6 3.5 3.8l.6.6c.6.6 1.2 1.1 1.8 1.7l.1.1c.4.4.8.7 1.3 1.1.6.5 1.2 1 1.7 1.5.5.4.9.7 1.4 1.1.6.5 1.2.9 1.8 1.4.5.3.9.6 1.4.9.6.4 1.3.8 2 1.2.4.3.9.5 1.4.8.8.4 1.5.7 2.3 1.1.4.1.8.4 1.1.5.6.3 1.1.5 1.7.7l1.8.6c.7.2 1.4.5 2 .8.1 0 .2.1.3.1 55.1 16.5 110-3.8 175.5 42.8 1.8 1.3 6.1 4.5 6.1 4.5-6-86.1 43.7-120.3 61.3-176.7 0-.1.1-.2.1-.4.1-.8.3-1.7.6-2.6.4-1.3.7-2.6.9-3.9v-.2c.1-.8.3-1.6.4-2.4 0-.2.1-.5.1-.7.1-.9.2-1.7.2-2.6v-2.9c-.2 0-.2-.2-.2-.4z',
    layers: [
    { opacity: 0.81, d: 'm255.8 328.3c-4.5-13.5-22.3-72.9-12.8-136.2-19 1.7-40 1.1-61.3-1.4-9.8 19.1-21 37-33.1 51.9 61.9 29.1 103.2 81.8 108 88.1-.3-.9-1.4-3.6-2.6-7.6z' },
    { opacity: 0.5, d: 'm243 192.1c.9-6.1 2.1-12.2 3.5-18.3 23.2-97.8-28.9-172.8-28.9-172.8 2.4 32.7 1.8 64.6-2.2 90.5-4.7 31-17.1 66.9-33.7 99.3 21.3 2.4 42.3 3 61.3 1.3z' },
    { opacity: 0.57, d: 'm131.4 235.4c5.9 2.2 11.7 4.6 17.2 7.3 12-14.9 23.3-32.8 33.1-51.9-31.6-3.7-63.6-11.5-90.2-22.3-3.8-1.5-7.5-3.1-11-4.8-10.2-4.8-21-10.7-32.1-17.5-.2-.1-.4-.2-.6-.4-.2-.1-.3-.2-.5-.3-14.3-8.8-29.1-19.1-43.4-30.3-.1 0 33.2 85.2 127.5 120.2z' },
    { opacity: 1, d: 'm148.7 242.7c-20.1 25-42.4 41.6-63.6 41.8-1.8 0-3.6-.1-5.3-.3-1.2-.1-2.3-.3-3.5-.6s-2.3-.6-3.5-.9c.8.3 1.5.5 2.1.8 56.8 17.1 113.4-5.1 181.7 47.3-4.7-6.3-46-59-107.9-88.1z' },
    { opacity: 0.96, d: 'm79.8 284.2c1.8.2 3.5.3 5.3.3 21.2-.2 43.5-16.8 63.6-41.8-5.6-2.6-11.3-5.1-17.2-7.3-94.3-35-127.6-120.2-127.6-120.2.2 2.2.3 4.3.5 6.5 0 .4.1.7.1 1.1.1.8.1 1.6.2 2.4 0 .2 0 .3.1.5 0 .4.1.7.1 1.1.2 1.7.3 3.3.5 5 .1.8.2 1.5.3 2.3.2 1.9.4 3.8.7 5.8l.3 2.1v.4c.3 2.4.6 4.7 1 7.1 0 .2 0 .5.1.7l.6 3.9c.1.3.1.7.2 1 .2 1.1.4 2.3.6 3.4 0 .2.1.4.1.6.4 2.5.8 4.9 1.3 7.4l.3 1.5c.4 1.9.7 3.9 1.1 5.8.1.5.2 1 .3 1.4.1.3.1.5.2.8.5 2.3 1 4.7 1.5 7 .1.4.2.9.3 1.3 1.8 7.9 3.9 15.6 6.1 22.9v.1c.3.9.6 1.9.9 2.8.1.4.3.8.4 1.3.2.7.4 1.3.7 2 .3.9.6 1.7.9 2.6.1.4.3.9.5 1.3.4 1.2.9 2.5 1.3 3.7 0 .1.1.2.1.3 1.6 4.3 3.3 8.5 5 12.5.1.1.1.3.2.4.5 1.2 1.1 2.4 1.6 3.5.1.1.1.3.2.4.6 1.3 1.2 2.5 1.8 3.8.8 1.5 1.5 3 2.3 4.4 0 .1 0 .1.1.2.1.3.3.5.4.8.6 1 1.1 2 1.7 3 .1.2.2.4.3.5.7 1.2 1.4 2.3 2.1 3.4 0 .1.1.1.1.2.1.2.2.3.3.5.6.9 1.2 1.8 1.7 2.7.4.5.7 1 1 1.5.3.4.7.9 1 1.3.1.2.2.3.4.5.3.4.6.8.9 1.1.1.1.2.3.3.4.3.4.7.8 1 1.2l1.5 1.8c1.1 1.3 2.3 2.6 3.5 3.8l.6.6c.6.6 1.2 1.1 1.8 1.7l.1.1c.4.4.8.7 1.3 1.1.6.5 1.2 1 1.7 1.5.5.4.9.7 1.4 1.1.6.5 1.2.9 1.8 1.4.5.3.9.6 1.4.9.6.4 1.3.8 2 1.2.4.3.9.5 1.4.8.8.4 1.5.7 2.3 1.1.4.1.8.4 1.1.5.6.3 1.1.5 1.7.7l1.8.6c1.2.4 2.3.7 3.5.9.9.5 2.1.7 3.3.8z' },
    { opacity: 0.93, d: 'm243 192.1c-9.5 63.3 8.3 122.7 12.8 136.2l.9 2.5c-6-86.2 43.8-120.3 61.4-176.9.1-.8.3-1.8.6-2.8-7.2 25-37.4 37.6-75.7 41z' },
    { opacity: 0.91, d: 'm319.6 147.1c.1-.8.3-1.6.4-2.4 0-.2.1-.5.1-.7.1-.9.2-1.7.2-2.6v-3.6c0-.9-.1-1.8-.1-2.8v-.1c-.1-1-.2-1.9-.3-2.9 0-.2-.1-.5-.1-.7-.1-.9-.3-1.8-.4-2.7v-.2c-.2-1-.4-1.9-.6-2.9-.1-.3-.1-.6-.2-.8-.2-.8-.4-1.6-.6-2.5-.1-.2-.1-.4-.2-.6-.3-.9-.6-1.9-.9-2.8-.1-.4-.2-.8-.4-1.1-.2-.7-.4-1.3-.7-2-.2-.6-.4-1.1-.6-1.7s-.5-1.3-.8-1.9c-.2-.5-.5-1.1-.7-1.7-.2-.4-.3-.8-.5-1.1-13.9-31.8-46.4-67.7-83.2-98.4-.6-.5-1.2-1-1.9-1.6-.6-.5-1.1-.9-1.7-1.4-1.2-1-2.5-2-3.7-3-.3-.2-.6-.4-.8-.7-1.4-1.1-2.8-2.3-4.3-3.4 0 0 52.1 75 28.9 172.9-1.4 6.1-2.6 12.2-3.5 18.3 38.3-3.4 68.5-16 75.6-41 .4-1.3.7-2.6.9-3.9.1.1.1 0 .1 0z' },
    ],
  },
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
