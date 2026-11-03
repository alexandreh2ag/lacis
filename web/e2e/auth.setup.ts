import { expect, test as setup } from '@playwright/test'
import { admin, adminState } from './env'

// The other tests run in the session of the bootstrap admin.
setup('log in as the admin', async ({ page }) => {
  await page.goto('login')
  await page.getByLabel("Nom d'utilisateur").fill(admin.username)
  await page.getByLabel('Mot de passe').fill(admin.password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('heading', { name: 'Namespaces' })).toBeVisible()
  await page.context().storageState({ path: adminState })
})
