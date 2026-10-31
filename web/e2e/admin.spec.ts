import { expect, test } from '@playwright/test'

test('a user is created, can log in, then is deleted', async ({ page, browser }) => {
  await page.goto('admin')
  await page.getByRole('button', { name: 'Nouvel utilisateur' }).click()
  const dialog = page.getByRole('dialog', { name: 'Nouvel utilisateur' })
  await dialog.getByLabel("Nom d'utilisateur").fill('e2e.reader')
  await dialog.getByLabel('Prénom').fill('Lina')
  await dialog.getByLabel('Nom', { exact: true }).fill('Reader')
  await dialog.getByLabel('Mot de passe', { exact: true }).fill('reader-password')
  await dialog.getByLabel('Confirmer le mot de passe').fill('reader-password')
  await dialog.getByRole('button', { name: 'Créer' }).click()
  await expect(dialog).toBeHidden()
  const row = page.locator('tr', { has: page.getByText('e2e.reader', { exact: true }) })
  await expect(row).toContainText('Lina Reader')

  // Without a role, the new user sees no namespace.
  const other = await browser.newContext({ storageState: undefined })
  const p = await other.newPage()
  await p.goto('login')
  await p.getByLabel("Nom d'utilisateur").fill('e2e.reader')
  await p.getByLabel('Mot de passe').fill('reader-password')
  await p.getByRole('button', { name: 'Se connecter' }).click()
  await expect(p.getByRole('heading', { name: 'Namespaces' })).toBeVisible()
  await expect(p.getByRole('link', { name: 'acme', exact: true })).toHaveCount(0)
  await other.close()

  await row.getByRole('button', { name: 'Supprimer' }).click()
  const confirm = page.getByRole('dialog', { name: 'Supprimer e2e.reader ?' })
  await confirm.getByRole('button', { name: 'Supprimer' }).click()
  await expect(row).toHaveCount(0)
})
