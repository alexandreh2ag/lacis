import { expect, test } from '@playwright/test'
import { admin } from './env'

test('a wrong password is refused', async ({ page }) => {
  await page.goto('login')
  await page.getByLabel("Nom d'utilisateur").fill(admin.username)
  await page.getByLabel('Mot de passe').fill('not-the-password')
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('alert')).toHaveText("Nom d'utilisateur ou mot de passe incorrect.")
  await expect(page).toHaveURL(/\/login$/)
})

test('the login comes back to the page asked, the logout leaves it', async ({ page }) => {
  await page.goto('acme/hosts?q=redis')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await page.getByLabel("Nom d'utilisateur").fill(admin.username)
  await page.getByLabel('Mot de passe').fill(admin.password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('heading', { name: 'Hosts' })).toBeVisible()
  await expect(page).toHaveURL(/\/acme\/hosts\?q=redis$/)

  await page.getByRole('button', { name: /^Compte de/ }).click()
  await page.getByRole('menuitem', { name: 'Se déconnecter' }).click()
  await expect(page).toHaveURL(/\/login/)
  await page.goto('acme')
  await expect(page).toHaveURL(/\/login\?redirect=%2Facme$/)
})
