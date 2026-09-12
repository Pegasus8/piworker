import { test, expect } from '@playwright/test'
const nodeTypes = [{ type: 'process-transform', name: 'Transform', category: 'processing', description: 'Transform payload using expressions', config: { properties: { expression: { type: 'string', title: 'Expression', default: 'payload', description: 'An Expr expression over payload' } }, required: ['expression'] } }]
test.beforeEach(async ({ page }) => {
 page.setDefaultTimeout(5000)
 await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname
  return route.fulfill({ json: { success: true, data: path.endsWith('/auth/status') ? { enabled: false } : path.endsWith('/node-types') ? { nodeTypes } : [] } })
 })
 await page.setViewportSize({ width: 1280, height: 900 })
 await page.goto('/editor')
 await page.getByRole('button', { name: 'Add Transform', exact: true }).click()
})

test('JSON validation prevents accidental text tests and preserves JSON scalar values', async ({ page }) => {
 const requests: any[] = []
 await page.route('**/api/nodes/process-transform/test', route => {
  const request = route.request().postDataJSON(); requests.push(request)
  return route.fulfill({ json: { success: true, data: { outputs: [{ payload: request.payload, sourcePort: 'output' }], durMs: 1 } } })
 })
 const input = page.getByRole('textbox', { name: 'Input payload', exact: true })
 const run = page.getByRole('button', { name: 'Test node', exact: true })
 await input.fill('{bad json')
 await expect(page.getByRole('alert')).toContainText('Invalid JSON')
 await expect(run).toBeDisabled()
 expect(requests).toHaveLength(0)
 for (const value of ['false', '0', 'null', '[1,2]']) {
  await input.fill(value); await run.click()
  await expect(page.getByRole('status')).toContainText('1 output message')
  expect(requests.at(-1).payload).toEqual(JSON.parse(value))
 }
 await page.getByLabel('Payload format').selectOption('text')
 await input.fill('42'); await run.click()
 await expect(page.getByRole('status')).toContainText('1 output message')
 expect(requests.at(-1).payload).toBe('42')
})

test('test output separates payload, marks stale results and can be reused as input', async ({ page }) => {
 await page.route('**/api/nodes/process-transform/test', route => route.fulfill({ json: { success: true, data: { outputs: [{ payload: { temperature: 77 }, sourcePort: 'output', topic: 'room', meta: { transformed: true } }], durMs: 2 } } }))
 await page.getByRole('textbox', { name: 'Input payload', exact: true }).fill('{"temperature":25}')
 await page.getByRole('button', { name: 'Test node', exact: true }).click()
 await expect(page.getByRole('heading', { name: 'Output 1' })).toBeVisible()
 await expect(page.getByText('Port: output', { exact: true })).toBeVisible()
 await page.getByRole('textbox', { name: 'Expression', exact: true }).fill('payload.temperature * 1.8 + 32')
 await expect(page.getByRole('status')).toContainText('Result is out of date')
 await page.getByRole('button', { name: 'Use output 1 as input' }).click()
 await expect(page.getByRole('textbox', { name: 'Input payload', exact: true })).toHaveValue(JSON.stringify({ temperature: 77 }, null, 2))
})

test('message context is validated and sent with draft configuration', async ({ page }) => {
 let request: any
 await page.route('**/api/nodes/process-transform/test', route => { request = route.request().postDataJSON(); return route.fulfill({ json: { success: true, data: { outputs: null, durMs: 0.2 } } }) })
 await page.getByText('Message context', { exact: true }).click()
 await page.getByLabel('Topic', { exact: true }).fill('sensors/office')
 await page.getByLabel('Metadata (JSON object)').fill('[]')
 await expect(page.getByRole('button', { name: 'Test node', exact: true })).toBeDisabled()
 await page.getByLabel('Metadata (JSON object)').fill('{"unit":"C"}')
 await page.getByRole('textbox', { name: 'Expression', exact: true }).fill('meta.unit')
 await page.getByRole('button', { name: 'Test node', exact: true }).click()
 await expect(page.getByText('No output messages', { exact: true })).toBeVisible()
 expect(request.topic).toBe('sensors/office')
 expect(request.meta).toEqual({ unit: 'C' })
 expect(request.config.expression).toBe('meta.unit')
})

test('a late response is marked stale and cancellation never shows it as current', async ({ page }) => {
 let release!: () => void
 const gate = new Promise<void>(resolve => { release = resolve })
 await page.route('**/api/nodes/process-transform/test', async route => {
  await gate
  await route.fulfill({ json: { success: true, data: { outputs: [{ payload: 42 }], durMs: 3 } } }).catch(() => {})
 })
 await page.getByRole('button', { name: 'Test node', exact: true }).click()
 await expect(page.getByRole('button', { name: 'Cancel test' })).toBeVisible()
 await page.getByRole('textbox', { name: 'Input payload', exact: true }).fill('99')
 release()
 await expect(page.getByRole('status')).toContainText('Result is out of date')
 let releaseCancelled!: () => void
 const cancelledGate = new Promise<void>(resolve => { releaseCancelled = resolve })
 await page.route('**/api/nodes/process-transform/test', async route => {
  await cancelledGate
  await route.fulfill({ json: { success: true, data: { outputs: [{ payload: 'old' }] } } }).catch(() => {})
 })
 await page.getByRole('button', { name: 'Test node', exact: true }).click()
 await page.getByRole('button', { name: 'Cancel test' }).click()
 releaseCancelled()
 await expect(page.getByRole('status')).toContainText('Test cancelled')
 await expect(page.getByRole('heading', { name: 'Output 1' })).toHaveCount(0)
})

test('mobile playground handles failures and keeps its sample after closing the node', async ({ page }) => {
 await page.setViewportSize({ width: 375, height: 812 })
 await page.route('**/api/nodes/process-transform/test', route => route.fulfill({ status: 400, json: { success: false, error: 'Invalid expression near line 1' } }))
 await page.getByRole('textbox', { name: 'Input payload', exact: true }).fill('{"hello":"lab"}')
 await page.getByRole('button', { name: 'Test node', exact: true }).click()
 await expect(page.getByRole('alert')).toContainText('Invalid expression')
 const modal = page.getByRole('dialog', { name: 'Node configuration' })
 expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
 await page.getByRole('button', { name: 'Close configuration' }).click()
 await page.locator('.vue-flow__node').click()
 await expect(page.getByRole('textbox', { name: 'Input payload', exact: true })).toHaveValue('{"hello":"lab"}')
 await expect(page.getByRole('alert')).toHaveCount(0)
})
