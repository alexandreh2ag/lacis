import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { admin, root, runDir } from './env'

// Takes the screenshots of the documentation landing page: see
// playwright.screenshots.config.ts.

const out = `${root}docs/static/img/screenshots`
const themes = ['light', 'dark'] as const

type Shot = { name: string; open: (page: Page) => Promise<void> }

const shots: Shot[] = [
  {
    name: 'namespaces',
    open: async (page) => {
      await page.goto('')
      await expect(page.getByRole('link', { name: 'globex', exact: true })).toBeVisible()
    },
  },
  {
    name: 'graph-zone',
    open: async (page) => {
      await page.goto('acme/graph?env=production')
      await page.locator('.react-flow__node', { hasText: 'shop' }).first().click()
      await expect(page.locator('.lc-zone').first()).toBeVisible({ timeout: 15_000 })
    },
  },
  {
    name: 'graph-infra',
    open: async (page) => {
      await page.goto('acme/graph?lens=infra&env=production')
      await expect(page.locator('.react-flow__node').first()).toBeVisible({ timeout: 15_000 })
    },
  },
  {
    name: 'host',
    open: async (page) => {
      await page.goto('acme/hosts?q=acme-shop-prod-1')
      await page.getByRole('link', { name: 'acme-shop-prod-1', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'acme-shop-prod-1' })).toBeVisible()
      await page.goto(`${page.url()}?tab=apps`)
      await expect(page.getByText('shop-front').first()).toBeVisible()
    },
  },
  {
    name: 'project',
    open: async (page) => {
      await page.goto('acme/projects/shop?tab=apps')
      await expect(page.getByRole('heading', { name: 'shop' }).first()).toBeVisible()
    },
  },
]

test.beforeAll(() => mkdirSync(out, { recursive: true }))

for (const theme of themes) {
  for (const shot of shots) {
    test(`${shot.name} (${theme})`, async ({ page }) => {
      await page.addInitScript((t) => {
        localStorage.setItem('lacis-lang', 'en')
        localStorage.setItem('lacis-theme', t)
      }, theme)
      await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
      const login = await page.request.post('auth/login', { data: admin })
      expect(login.ok()).toBeTruthy()

      await shot.open(page)
      await page.waitForLoadState('networkidle')
      await page.waitForTimeout(500) // the graph settles its layout
      await page.mouse.move(0, 0) // no hover state
      const png = `${runDir}/${shot.name}-${theme}.png`
      await page.screenshot({ path: png })
      execFileSync('convert', [png, '-quality', '85', `${out}/${shot.name}-${theme}.webp`])
    })
  }
}
