import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await page.route('**/api/**', route => route.fulfill({ json: { success: true, data: route.request().url().endsWith('/auth/status') ? { enabled: false } : route.request().url().endsWith('/flows') ? { flows: [] } : [] } }))
})

test('search distinguishes no matches and offers a reset', async ({ page }) => {
  await page.route('**/api/flows', route => route.fulfill({ json: { success: true, data: { flows: [{ id: 'f1', name: 'Morning lab', running: false, nodeCount: 2, createdAt: '2026-09-12', updatedAt: '2026-09-12' }] } } }))
  await page.goto('/')
  await page.getByRole('textbox', { name: 'Search flows' }).fill('weather')
  await expect(page.getByRole('heading', { name: 'No matching flows' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear search' }).click()
  await expect(page.getByRole('link', { name: 'Morning lab' })).toBeVisible()
})

test('an empty list explains flows, and a new flow is named first', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'No flows yet' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Start from an example' })).toBeVisible()
  await page.getByRole('button', { name: 'Create Flow' }).click()
  const dialog = page.getByRole('dialog', { name: 'New Flow' })
  await expect(dialog.getByRole('button', { name: 'Create', exact: true })).toBeDisabled()
  await dialog.getByLabel('Name').fill('Water the plants')
  await dialog.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page).toHaveURL(/\/editor\?name=Water/)
  await expect(page.getByRole('textbox', { name: 'Flow name' })).toHaveValue('Water the plants')
})

test('the interface follows the chosen language and remembers it', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('combobox', { name: 'Language' }).selectOption('es')
  await expect(page.getByRole('heading', { name: 'Flujos', level: 1 })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'es')
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Todavía no hay flujos' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Flujos' })).toHaveAttribute('aria-current', 'page')
})

test('dialog supports keyboard dismissal and restores focus', async ({ page }) => {
  await page.goto('/editor')
  const settings = page.getByRole('button', { name: 'Settings', exact: true })
  await settings.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect.poll(() => dialog.evaluate(el => el.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await expect(settings).toBeFocused()
})

test('mobile navigation fits and reduced motion removes decorative animation', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'New Flow' })).toBeInViewport()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(await page.locator('main').evaluate(el => getComputedStyle(el).animationName)).toBe('none')
})
