import { expect, test } from '@playwright/test'

// The tests run in a French browser: the front speaks French until
// another language is picked in the account menu, kept in the browser.
test('the language picked is kept, the browser decides again when asked', async ({ page }) => {
  await page.goto('acme')
  const nav = page.getByRole('navigation', { name: 'Sections' })
  await expect(nav.getByRole('link', { name: 'Historique' })).toBeVisible()

  await page.getByRole('button', { name: /^Compte de / }).click()
  await page.getByRole('group', { name: 'Langue' }).getByRole('button', { name: 'English' }).click()
  await expect(nav.getByRole('link', { name: 'History' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')

  await page.reload()
  await expect(nav.getByRole('link', { name: 'History' })).toBeVisible()

  await page.getByRole('button', { name: /^Account of / }).click()
  await page.getByRole('group', { name: 'Language' }).getByRole('button', { name: 'Browser' }).click()
  await expect(nav.getByRole('link', { name: 'Historique' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr')
})

test.describe('in an English browser', () => {
  test.use({ locale: 'en-US' })

  test('the front speaks English', async ({ page }) => {
    await page.goto('acme')
    await expect(page.getByRole('navigation', { name: 'Sections' }).getByRole('link', { name: 'History' })).toBeVisible()
  })
})
