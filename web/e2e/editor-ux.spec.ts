import { test, expect } from '@playwright/test'
test.use({ hasTouch: true })
const flow = { id: 'f1', name: 'Morning lab', enabled: false, nodes: [], connections: [], updatedAt: '2026-09-12', createdAt: '2026-09-12' }
const nodeTypes = [
 { type: 'timer', name: 'Timer', description: 'Starts a flow', category: 'input', outputs: ['output'], config: { properties: { interval: { type: 'number', default: 10 } } } },
 { type: 'log', name: 'Log', description: 'Writes a message', category: 'output', inputs: ['input'], config: { properties: { message: { type: 'string' } } } }
]
test.beforeEach(async ({ page }) => {
 page.setDefaultTimeout(5000)
 await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname
  const data = path.endsWith('/auth/status') ? { enabled: false } : path.endsWith('/node-types') ? { nodeTypes } : path.endsWith('/flows') ? { flows: [flow] } : path.endsWith('/flows/f1') ? { flow, running: false } : []
  return route.fulfill({ json: { success: true, data } })
 })
})
test('flow deletion can be cancelled, reports failure, and allows retry', async ({ page }) => {
 let calls = 0
 await page.route('**/api/flows/f1', route => {
  if (route.request().method() !== 'DELETE') return route.fallback()
  calls++
  return route.fulfill(calls === 1 ? { status: 500, json: { error: 'Unavailable' } } : { json: { success: true } })
 })
 await page.goto('/')
 await page.getByTitle('Delete', { exact: true }).click()
 const dialog = page.getByRole('dialog', { name: 'Delete flow?' })
 await expect(dialog).toContainText('Morning lab')
 await dialog.getByRole('button', { name: 'Cancel' }).click()
 expect(calls).toBe(0)
 await page.getByTitle('Delete', { exact: true }).click()
 await dialog.getByRole('button', { name: 'Delete flow', exact: true }).click()
 await expect(page.getByRole('alert')).toContainText('Could not delete')
 await page.getByTitle('Delete', { exact: true }).click()
 await dialog.getByRole('button', { name: 'Delete flow', exact: true }).click()
 await expect(page.getByRole('status')).toContainText('Flow deleted')
 expect(calls).toBe(2)
})
test('failed save stops deployment and preserves editable changes', async ({ page }) => {
 let deployments = 0
 await page.route('**/api/flows/f1', route => route.request().method() === 'PUT' ? route.fulfill({ status: 500, json: { error: 'Offline' } }) : route.fallback())
 await page.route('**/api/flows/f1/deploy', route => { deployments++; return route.fulfill({ json: { success: true } }) })
 await page.goto('/editor/f1')
 await page.getByRole('textbox', { name: 'Flow name', exact: true }).fill('Changed name')
 await page.getByRole('button', { name: 'Deploy', exact: true }).click()
 await expect(page.getByRole('alert')).toContainText('Could not save')
 await expect(page.getByText('Unsaved', { exact: true })).toBeVisible()
 expect(deployments).toBe(0)
})
test('unsaved navigation offers keep editing or discard', async ({ page }) => {
 await page.goto('/editor/f1')
 await page.getByRole('textbox', { name: 'Flow name', exact: true }).fill('Draft')
 await page.getByRole('button', { name: 'Back to flows' }).click()
 const dialog = page.getByRole('dialog', { name: 'Discard unsaved changes?' })
 await dialog.getByRole('button', { name: 'Keep editing' }).click()
 await expect(page).toHaveURL(/editor\/f1/)
 await page.getByRole('button', { name: 'Back to flows' }).click()
 await dialog.getByRole('button', { name: 'Discard changes' }).click()
 await expect(page).toHaveURL(/\/$/)
})
test('mobile users add, configure, connect and save without dragging', async ({ page }) => {
 await page.setViewportSize({ width: 375, height: 812 })
 let saved: any
 await page.route('**/api/flows/f1', route => {
  if (route.request().method() !== 'PUT') return route.fallback()
  saved = route.request().postDataJSON()
  return route.fulfill({ json: { success: true, data: saved } })
 })
 await page.goto('/editor/f1')
 for (const name of ['Timer', 'Log']) {
  await page.getByRole('button', { name: 'Add nodes', exact: true }).click()
  await page.getByRole('button', { name: `Add ${name}`, exact: true }).click()
  const config = page.getByRole('dialog', { name: 'Node configuration' })
  await expect(config).toBeVisible()
  if (name === 'Log') await config.getByLabel('message', { exact: true }).fill('Hello lab')
  await config.getByRole('button', { name: 'Apply changes' }).click()
 }
 await page.getByRole('button', { name: 'Connections', exact: true }).click()
 const connections = page.getByRole('dialog', { name: 'Connections' })
 await connections.getByLabel('From node').selectOption({ label: 'Timer (1)' })
 await connections.getByLabel('To node').selectOption({ label: 'Log (2)' })
 await connections.getByRole('button', { name: 'Connect nodes' }).click()
 await expect(connections.getByText('Timer → Log')).toBeVisible()
 await connections.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('button', { name: 'Save', exact: true }).click()
 await expect(page.getByRole('status')).toContainText('Flow saved')
 expect(saved.nodes).toHaveLength(2)
 expect(saved.nodes[1].config.message).toBe('Hello lab')
 expect(saved.connections).toHaveLength(1)
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
 expect(await page.locator('.vue-flow').evaluate(el => el.getBoundingClientRect().width)).toBeGreaterThan(300)
 await page.getByRole('button', { name: 'Connections', exact: true }).click()
 await expect(connections.getByRole('button', { name: 'Connect nodes' })).toBeDisabled()
 await connections.getByRole('button', { name: 'Remove Timer to Log' }).click()
 const remove = page.getByRole('dialog', { name: 'Remove connection?' })
 await remove.getByRole('button', { name: 'Cancel', exact: true }).click()
 await expect(connections.getByText('Timer → Log')).toBeVisible()
 await connections.getByRole('button', { name: 'Remove Timer to Log' }).click()
 await remove.getByRole('button', { name: 'Remove connection', exact: true }).click()
 await expect(connections.getByText('No connections yet.')).toBeVisible()
 await connections.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('button', { name: 'Save', exact: true }).click()
 await expect(page.getByRole('status')).toContainText('Flow saved')
 expect(saved.connections).toHaveLength(0)
 await page.screenshot({ path: 'test-results/editor-mobile.png' })
})

test('node edits survive cancelled dismissal and node deletion is explicit', async ({ page }) => {
 await page.setViewportSize({ width: 375, height: 812 })
 await page.goto('/editor/f1')
 await page.getByRole('button', { name: 'Add nodes', exact: true }).click()
 await page.getByRole('button', { name: 'Add Log', exact: true }).click()
 const config = page.getByRole('dialog', { name: 'Node configuration' })
 await config.getByLabel('message', { exact: true }).fill('Keep me')
 await page.keyboard.press('Escape')
 const discard = page.getByRole('dialog', { name: 'Discard node changes?' })
 await expect(discard.getByRole('button', { name: 'Keep editing' })).toBeFocused()
 await discard.getByRole('button', { name: 'Keep editing' }).click()
 await expect(config).toBeVisible()
 await expect(config.getByLabel('message', { exact: true })).toHaveValue('Keep me')
 await config.getByRole('button', { name: 'Delete', exact: true }).click()
 const remove = page.getByRole('dialog', { name: 'Delete node?' })
 await remove.getByRole('button', { name: 'Cancel', exact: true }).click()
 await expect(config).toBeVisible()
 await config.getByRole('button', { name: 'Delete', exact: true }).click()
 await remove.getByRole('button', { name: 'Delete node', exact: true }).click()
 await expect(config).not.toBeVisible()
 await expect(page.locator('.vue-flow__node')).toHaveCount(0)
})

test('secrets use nested confirmations and recover after mutation failures', async ({ page }) => {
 let names = ['TOKEN']
 let attempts = 0
 await page.route('**/api/secrets', route => route.fulfill({ json: { success: true, data: { names } } }))
 await page.route('**/api/secrets/TOKEN', route => {
  attempts++
  if (attempts === 1) return route.fulfill({ status: 500, json: { error: 'Offline' } })
  names = []
  return route.fulfill({ json: { success: true } })
 })
 await page.goto('/')
 await page.getByRole('button', { name: 'Secrets', exact: true }).click()
 const secrets = page.getByRole('dialog', { name: 'Manage secrets' })
 await secrets.getByRole('button', { name: 'Delete secret TOKEN' }).click()
 const confirmation = page.getByRole('dialog', { name: 'Delete secret?' })
 await page.keyboard.press('Escape')
 await expect(confirmation).not.toBeVisible()
 expect(attempts).toBe(0)
 await secrets.getByRole('button', { name: 'Delete secret TOKEN' }).click()
 await confirmation.getByRole('button', { name: 'Delete secret', exact: true }).click()
 await expect(secrets.getByRole('alert')).toContainText('Could not delete the secret')
 await secrets.getByRole('button', { name: 'Delete secret TOKEN' }).click()
 await confirmation.getByRole('button', { name: 'Delete secret', exact: true }).click()
 await expect(secrets.getByRole('status')).toContainText('Secret deleted')
 await expect(secrets.getByText('No secrets yet.')).toBeVisible()
})
