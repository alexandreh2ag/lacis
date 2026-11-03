import { expect, test, type Page } from '@playwright/test'

async function login(p: Page, username: string, password: string) {
  await p.goto('login')
  await p.getByLabel("Nom d'utilisateur").fill(username)
  await p.getByLabel('Mot de passe').fill(password)
  await p.getByRole('button', { name: 'Se connecter' }).click()
  await expect(p.getByRole('heading', { name: 'Namespaces' })).toBeVisible()
}

test('a user without any role changes their names and password in their profile', async ({ page, browser }) => {
  await page.goto('admin')
  await page.getByRole('button', { name: 'Nouvel utilisateur' }).click()
  const dialog = page.getByRole('dialog', { name: 'Nouvel utilisateur' })
  await dialog.getByLabel("Nom d'utilisateur").fill('e2e.profile')
  await dialog.getByLabel('Mot de passe', { exact: true }).fill('profile-password')
  await dialog.getByLabel('Confirmer le mot de passe').fill('profile-password')
  await dialog.getByRole('button', { name: 'Créer' }).click()
  await expect(dialog).toBeHidden()

  const other = await browser.newContext({ storageState: undefined })
  const p = await other.newPage()
  await login(p, 'e2e.profile', 'profile-password')
  await p.getByRole('button', { name: /^Compte de/ }).click()
  await p.getByRole('menuitem', { name: 'Profil' }).click()
  await expect(p.getByRole('heading', { name: 'Profil' })).toBeVisible()

  await p.getByLabel('Prénom').fill('Paula')
  await p.getByLabel('Nom', { exact: true }).fill('Profile')
  await p.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(p.getByText('Enregistré.')).toBeVisible()
  await expect(p.getByRole('button', { name: 'Compte de Paula Profile' })).toBeVisible()

  await p.getByRole('tab', { name: 'Mot de passe' }).click()
  await p.getByLabel('Mot de passe actuel').fill('wrong-password')
  await p.getByLabel('Nouveau mot de passe', { exact: true }).fill('profile-new-password')
  await p.getByLabel('Confirmer le nouveau mot de passe').fill('profile-new-password')
  await p.getByRole('button', { name: 'Changer le mot de passe' }).click()
  await expect(p.getByRole('alert')).toBeVisible()
  await p.getByLabel('Mot de passe actuel').fill('profile-password')
  await p.getByRole('button', { name: 'Changer le mot de passe' }).click()
  await expect(p.getByText(/Mot de passe changé/)).toBeVisible()
  await other.close()

  const again = await browser.newContext({ storageState: undefined })
  await login(await again.newPage(), 'e2e.profile', 'profile-new-password')
  await again.close()

  await page.reload()
  const row = page.locator('tr', { has: page.getByText('e2e.profile', { exact: true }) })
  await expect(row).toContainText('Paula Profile')
  await row.getByRole('button', { name: 'Supprimer' }).click()
  await page.getByRole('dialog', { name: 'Supprimer e2e.profile ?' }).getByRole('button', { name: 'Supprimer' }).click()
  await expect(row).toHaveCount(0)
})
