import { test, expect, type Page } from '@playwright/test'

const bytes = Array.from({ length: 32 }, (_, i) => (i * 7 % 256).toString(16).toUpperCase().padStart(2, '0'))
const fingerprint = 'SHA256:' + bytes.join(':')
const pem = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n'

async function mockServer(page: Page, opts: { auth: boolean; tls?: object | null; ca?: boolean }) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/auth/status')) return route.fulfill({ json: { success: true, data: { enabled: opts.auth, setupRequired: false } } })
    if (path.endsWith('/auth/session')) return route.fulfill({ status: 401, json: { success: false, error: 'Sign in to continue' } })
    if (path.endsWith('/tls')) return opts.tls ? route.fulfill({ json: { success: true, data: opts.tls } }) : route.fulfill({ status: 401, json: { success: false } })
    return route.fulfill({ json: { success: true, data: { flows: [] } } })
  })
  await page.route('**/piworker-ca.crt', route => opts.ca === false
    ? route.fulfill({ status: 404, contentType: 'text/plain', body: 'no local CA' })
    : route.fulfill({ status: 200, contentType: 'application/x-x509-ca-cert', body: pem }))
}

test('the HTTP warning and the sign-in page lead to the trust page, usable signed out on a phone', async ({ page }) => {
  await mockServer(page, { auth: true })
  await page.goto('/login')
  await expect(page.getByRole('link', { name: 'Trust this device' })).toBeVisible()
  const warning = page.getByRole('note', { name: 'Unencrypted connection' })
  await warning.getByRole('link', { name: 'Set up HTTPS' }).click()
  await expect(page).toHaveURL(/\/trust$/)
  await expect(page.getByRole('heading', { name: 'Trust this device' })).toBeVisible()
  await expect(warning).toBeVisible()

  const download = page.getByRole('link', { name: 'Download CA certificate' })
  await expect(download).toHaveAttribute('href', '/piworker-ca.crt')
  await expect(download).toHaveAttribute('download', 'piworker-ca.crt')
  await expect(page.getByText('Sign in to see the fingerprint here.')).toBeVisible()
  await expect(page.getByText('journalctl -u piworker')).toBeVisible()

  await page.setViewportSize({ width: 375, height: 812 })
  await page.getByRole('button', { name: 'Android' }).click()
  await expect(page.getByRole('heading', { name: 'Install on Android' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.mouse.move(0, 0)
  await page.getByRole('group', { name: 'Choose your system' }).screenshot({ path: 'test-results/trust-platforms.png', animations: 'disabled' })
  await page.screenshot({ path: 'test-results/trust-mobile.png', fullPage: true })
})

test('signed-in users see the CA fingerprint, certificate names and per-OS guides', async ({ page }) => {
  await mockServer(page, { auth: false, tls: { mode: 'auto', caFingerprint: fingerprint, sans: ['localhost', 'raspberrypi.local', '192.168.1.10'], notAfter: '2027-12-10T00:00:00Z', caCertUrl: '/piworker-ca.crt' } })
  await page.goto('/')
  await page.getByRole('button', { name: 'HTTPS' }).click()
  await expect(page).toHaveURL(/\/trust$/)
  const fp = page.getByLabel('CA fingerprint')
  await expect(fp).toContainText(bytes.slice(0, 8).join(':'))
  await expect(fp).toContainText(bytes.slice(24).join(':'))
  await expect(page.getByText('raspberrypi.local', { exact: true })).toBeVisible()
  await expect(page.getByText('This page arrived over HTTP')).toBeVisible()
  await page.screenshot({ path: 'test-results/trust-desktop.png' })

  for (const [label, text] of [['macOS', 'Always Trust'], ['Windows', 'Trusted Root Certification Authorities'], ['Linux', 'update-ca-certificates'], ['iPhone / iPad', 'Certificate Trust Settings'], ['Firefox', 'Trust this CA to identify websites']]) {
    await page.getByRole('button', { name: label, exact: true }).click()
    await expect(page.getByRole('heading', { name: `Install on ${label}` })).toBeVisible()
    await expect(page.getByText(text).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Remove the trust later' })).toBeVisible()
  }
  await expect(page.getByText('NODE_EXTRA_CA_CERTS')).toBeVisible()
})

test('with HTTPS off the page explains how to turn it on', async ({ page }) => {
  await mockServer(page, { auth: false, ca: false, tls: { mode: 'off', caFingerprint: '', sans: [], notAfter: null, caCertUrl: '' } })
  await page.goto('/trust')
  await expect(page.getByRole('heading', { name: 'HTTPS is turned off' })).toBeVisible()
  await expect(page.getByText('sudo piworkerctl tls on')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Download CA certificate' })).toHaveCount(0)
})

test('account settings link to the trust page', async ({ page }) => {
  await mockServer(page, { auth: false, tls: { mode: 'custom', caFingerprint: '', sans: ['pi.example.lan'], notAfter: '2027-01-01T00:00:00Z', caCertUrl: '' }, ca: false })
  await page.goto('/account')
  await page.getByRole('button', { name: 'Trust this device' }).click()
  await expect(page).toHaveURL(/\/trust$/)
  await expect(page.getByRole('heading', { name: 'Certificate provided by the administrator' })).toBeVisible()
})
