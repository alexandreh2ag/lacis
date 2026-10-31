import { expect, test } from '@playwright/test'

test('a project is renamed from the settings', async ({ page }) => {
  await page.goto('acme/settings')
  await page.getByRole('tab', { name: /^Projets/ }).click()
  const row = page.locator('tr', { has: page.getByText('crm', { exact: true }) })
  await row.getByRole('button', { name: 'Modifier' }).click()

  const dialog = page.getByRole('dialog', { name: 'Projet crm' })
  await dialog.getByLabel('Nom').fill('CRM e2e')
  await dialog.getByLabel('Description').fill('Renommé par les tests de bout en bout')
  await dialog.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(dialog).toBeHidden()
  await expect(row).toContainText('CRM e2e')
  await expect(row).toContainText('Renommé par les tests de bout en bout')
})
