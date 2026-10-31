import { expect, test } from '@playwright/test'

// The fixtures of testdata/snapshots: 25 hosts of the namespace acme.
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
