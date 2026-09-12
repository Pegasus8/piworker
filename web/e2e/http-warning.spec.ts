import { test, expect } from '@playwright/test'

test('HTTP warning remains visible with auth disabled, on mobile, and after navigating', async ({ page }) => {
  await page.route('**/api/**', route => route.fulfill({ json: { success: true, data: route.request().url().endsWith('/auth/status') ? { enabled: false } : [] } }))
  await page.goto('/')
  const warning = page.getByRole('note', { name: 'Unencrypted connection' })
  await expect(warning).toBeVisible()
  await expect(warning).toContainText('Passwords and sessions can be intercepted, even on a local network.')
  await page.goto('/editor')
  await expect(page.locator('.vue-flow')).toBeVisible()
  expect(await page.evaluate(() => document.querySelector('.vue-flow')!.getBoundingClientRect().bottom <= innerHeight)).toBe(true)
  await page.goto('/account')
  await expect(warning).toBeVisible()
  await page.setViewportSize({ width: 375, height: 812 })
  await expect(warning).toBeInViewport()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: 'test-results/http-warning-mobile.png', fullPage: true })
})

test('HTTPS page does not display the HTTP warning', async ({ page }) => {
  // Simulate a TLS-terminating proxy: HTTPS browser URLs, HTTP Vite upstream.
  await page.route('https://piworker.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.startsWith('/api/')) {
      await route.fulfill({ json: { success: true, data: url.pathname.endsWith('/auth/status') ? { enabled: true, setupRequired: false } : null }, status: url.pathname.endsWith('/auth/session') ? 401 : 200 })
    } else {
      const response = await route.fetch({ url: 'http://127.0.0.1:18096' + url.pathname + url.search })
      await route.fulfill({ response })
    }
  })
  await page.goto('https://piworker.test/login')
  await expect(page.getByRole('heading', { name: 'Sign in to continue' })).toBeVisible()
  await expect(page.getByRole('note', { name: 'Unencrypted connection' })).toHaveCount(0)
})
