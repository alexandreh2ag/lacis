import { expect, test } from '@playwright/test'

// The fixtures of testdata/snapshots: 25 hosts of the namespace acme
// (the 4 others are of the namespace globex).
test('the namespace opens on its projects and its last changes', async ({ page }) => {
  await page.goto('')
  await page.getByRole('link', { name: 'acme', exact: true }).click()
  await expect(page).toHaveURL(/\/acme$/)
  await expect(page.getByRole('heading', { name: 'Projets' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Derniers changements' })).toBeVisible()
})

test('the hosts are searched, then one is opened', async ({ page }) => {
  await page.goto('acme/hosts')
  await expect(page.getByText('25 hosts', { exact: true })).toBeVisible()

  await page.getByPlaceholder('Chercher un hostname, une IP, une version').fill('redis')
  await expect(page).toHaveURL(/q=redis/)
  // Every host left runs redis, the hosts are no longer all listed.
  await expect(page.getByText('25 hosts', { exact: true })).toBeHidden()
  const rows = page.locator('tbody tr')
  for (const row of await rows.all()) await expect(row).toContainText('redis')

  await page.getByRole('link', { name: 'acme-redis-prod-1' }).click()
  await expect(page.getByRole('heading', { name: 'acme-redis-prod-1' })).toBeVisible()
  await expect(page).toHaveURL(/\/acme\/hosts\/[0-9a-f-]{36}$/)
})

test('the graph draws the projects', async ({ page }) => {
  await page.goto('acme/graph')
  await expect(page.locator('.react-flow__node').first()).toBeVisible({ timeout: 15_000 })
  expect(await page.locator('.react-flow__node').count()).toBeGreaterThan(1)
})

test('the history is listed the latest or the oldest first', async ({ page }) => {
  await page.goto('acme/history')
  await expect(page.getByText("les plus récents d'abord")).toBeVisible()
  await expect(page.locator('section h2').first()).toBeVisible()

  await page.getByRole('button', { name: "Plus anciens d'abord" }).click()
  await expect(page).toHaveURL(/order=asc/)
  await expect(page.getByText("les plus anciens d'abord")).toBeVisible()
  await expect(page.getByRole('button', { name: "Plus anciens d'abord" })).toHaveAttribute('aria-pressed', 'true')
})

test('the repository of an app opens its page and is copied', async ({ page }) => {
  await page.goto('acme/apps?q=acme-site-staging-1')
  await page.locator('tbody tr a').first().click()
  const repository = page.getByRole('link', { name: 'https://git.acme.example/site/www.git' })
  await expect(repository).toHaveAttribute('href', 'https://git.acme.example/site/www')
  await expect(repository).toHaveAttribute('target', '_blank')
  await expect(page.getByRole('button', { name: 'Copier https://git.acme.example/site/www.git' })).toBeVisible()
})

test('the workers of an app and their scheduled tasks are listed', async ({ page }) => {
  await page.goto('acme/apps?q=acme-shop-staging-1')
  await page.locator('tbody tr a', { hasText: /api/ }).first().click()
  await page.getByRole('tab', { name: /^Workers/ }).click()
  await expect(page).toHaveURL(/tab=workers/)
  await expect(page.locator('tbody tr')).toHaveCount(3)
  await expect(page.getByRole('cell', { name: 'shop-api-cron' })).toBeVisible()

  await page.getByRole('tab', { name: /^Tâches planifiées/ }).click()
  await expect(page.locator('tbody tr')).toHaveCount(2)
  await expect(page.getByRole('row', { name: /sitemap.*0 3 \* \* \*.*shop-api-cron/ })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Commande' })).toBeHidden()
})

test('the whole commands of the workers and their tasks are shown when their agent sends them', async ({ page }) => {
  await page.goto('acme/apps?q=acme-crm-staging-1')
  await page.locator('tbody tr a', { hasText: /api/ }).first().click()
  await page.getByRole('tab', { name: /^Workers/ }).click()
  await expect(page.getByRole('cell', { name: '/usr/bin/php8.2 bin/console messenger:consume sync --time-limit=3600 --memory-limit=256M -vv' })).toBeVisible()
  await expect(page.locator('tbody tr')).toHaveCount(3)
  await page.getByRole('tab', { name: /^Tâches planifiées/ }).click()
  await expect(page.getByRole('columnheader', { name: 'Commande' })).toBeVisible()
  await expect(page.getByRole('row', { name: /sync-contacts.*\*\/10 \* \* \* \*.*app:contacts:sync --batch=500.*crm-api-cron/ })).toBeVisible()
})

test('an app uses another through its proxies, the route unfolding under the row', async ({ page }) => {
  await page.goto('acme/apps?q=shop-front')
  await page.getByRole('link', { name: 'shop-front' }).first().click()
  await page.getByRole('tab', { name: /Utilise/ }).click()
  // shop-front calls api.shop.acme.example: through the traefik then the
  // nginx of the api, it uses shop-api, not the traefik.
  const row = page.locator('tbody tr', { has: page.getByRole('link', { name: 'shop-api' }) }).first()
  await expect(row).toContainText('via traefik +1')
  await expect(page.locator('.lc-path')).toHaveCount(0)
  await row.getByRole('button', { name: 'Déplier le chemin' }).click()
  const route = page.locator('.lc-path__table tbody tr')
  await expect(route).toHaveCount(4)
  await expect(route.nth(1)).toContainText('traefik')
  await expect(route.nth(2)).toContainText('nginx')
  await expect(route.nth(3)).toContainText('shop-api')
  await page.getByRole('button', { name: 'Tout replier' }).click()
  await expect(page.locator('.lc-path')).toHaveCount(0)

  // shop-api is used by shop-front, not by the nginx serving it.
  await row.getByRole('link', { name: 'shop-api' }).click()
  await page.getByRole('tab', { name: /Utilisé par/ }).click()
  await expect(page.getByRole('heading', { name: 'Liens internes' })).toHaveCount(0)
  await expect(page.locator('tbody tr', { has: page.getByRole('link', { name: 'shop-front' }) })).toContainText('via traefik +1')
  await page.getByRole('button', { name: 'Tout déplier' }).click()
  await expect(page.locator('.lc-path')).toHaveCount(1)
})
