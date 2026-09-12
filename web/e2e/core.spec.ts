import { test, expect } from '@playwright/test'
const port = (id: string) => ({ id, name: id, dataType: 'any', multiple: true })
const nodeTypes = [
 { type: 'trigger-manual', name: 'Manual Inject', category: 'input', outputs: [port('output')], config: { properties: {} } },
 { type: 'switch', name: 'Switch', category: 'processing', inputs: [port('input')], outputs: [port('true'), port('false')], config: { properties: {} } },
 { type: 'debug', name: 'Debug', category: 'output', inputs: [port('input')], outputs: [port('output')], config: { properties: {} } }
]
const makeFlow = (state = 'inactive') => ({ id: 'core', name: 'Branch lab', state, nodes: nodeTypes.map((type, i) => ({ id: `n${i}`, type: type.type, category: type.category, position: { x: i * 250, y: 150 }, config: {}, inputs: type.inputs || [], outputs: type.outputs, enabled: true })).concat([{ id: 'disabled', type: 'debug', category: 'output', position: { x: 500, y: 350 }, config: {}, inputs: [port('input')], outputs: [port('output')], enabled: false }]), connections: [{ id: 'branch', sourceNode: 'n1', sourcePort: 'false', targetNode: 'n2', targetPort: 'input' }], createdAt: '2026-09-12', updatedAt: '2026-09-12' })
test.beforeEach(async ({ page }) => {
 page.setDefaultTimeout(5000)
 await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname
  const flow = makeFlow()
  const data = path.endsWith('/auth/status') ? { enabled: false } : path.endsWith('/node-types') ? { nodeTypes } : path.endsWith('/flows') ? { flows: [flow] } : path.endsWith('/flows/core') ? { flow, running: flow.state === 'running' } : { runs: [] }
  return route.fulfill({ json: { success: true, data } })
 })
})
test('branch ports survive editing, parallel branches and saving; disabled nodes stay disabled', async ({ page }) => {
 let saved: any
 await page.route('**/api/flows/core', route => {
  if (route.request().method() !== 'PUT') return route.fallback()
  saved = route.request().postDataJSON()
  return route.fulfill({ json: { success: true, data: saved } })
 })
 await page.goto('/editor/core')
 await expect(page.locator('[data-nodeid="n1"][data-handleid="false"]')).toBeVisible()
 await expect(page.locator('[data-nodeid="n2"][data-handleid="output"]')).toBeVisible()
 await page.getByRole('button', { name: 'Connections', exact: true }).click()
 const dialog = page.getByRole('dialog', { name: 'Connections', exact: true })
 await dialog.getByLabel('From node').selectOption('n1')
 await dialog.getByLabel('From port').selectOption('true')
 await dialog.getByLabel('To node').selectOption('n2')
 await dialog.getByRole('button', { name: 'Connect nodes' }).click()
 await expect(dialog.getByRole('button', { name: 'Connect nodes' })).toBeDisabled()
 await dialog.getByLabel('From port').selectOption('false')
 await expect(dialog.getByRole('button', { name: 'Connect nodes' })).toBeDisabled()
 await dialog.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('button', { name: 'Save', exact: true }).click()
 await expect(page.getByRole('status')).toContainText('Flow saved')
 expect(saved.connections.map((e: any) => e.sourcePort)).toEqual(['false', 'true'])
 expect(saved.connections.every((e: any) => e.targetPort === 'input')).toBe(true)
 expect(saved.nodes[1].outputs.map((p: any) => p.id)).toEqual(['true', 'false'])
 expect(saved.nodes[3].enabled).toBe(false)
})
test('running state is consistent in list, editor and after stopping', async ({ page }) => {
 const flow = makeFlow('running')
 await page.route('**/api/flows', route => route.fulfill({ json: { success: true, data: { flows: [flow] } } }))
 await page.route('**/api/flows/core', route => route.fulfill({ json: { success: true, data: { flow, running: flow.state === 'running' } } }))
 await page.route('**/api/flows/core/stop', route => { flow.state = 'inactive'; return route.fulfill({ json: { success: true, data: { flow, running: false } } }) })
 await page.goto('/')
 await expect(page.getByRole('switch')).toBeChecked()
 await page.goto('/editor/core')
 await expect(page.getByText('Running', { exact: true })).toBeVisible()
 await expect(page.getByRole('switch')).toHaveCount(0)
 await page.getByRole('button', { name: 'Stop', exact: true }).click()
 await expect(page.getByText('Stopped', { exact: true })).toBeVisible()
 await page.getByRole('button', { name: 'Back to flows' }).click()
 await expect(page.getByRole('switch')).not.toBeChecked()
})
test('manual injection validates input and allows retry without changing the saved node', async ({ page }) => {
 const flow = makeFlow('running')
 await page.route('**/api/flows/core', route => route.fulfill({ json: { success: true, data: { flow, running: true } } }))
 const requests: unknown[] = []
 await page.route('**/api/flows/core/inject/n0', route => {
  requests.push(route.request().postDataJSON())
  return route.fulfill(requests.length === 1 ? { status: 500, json: { error: 'Busy' } } : { json: { success: true } })
 })
 await page.goto('/editor/core')
 await page.getByRole('button', { name: 'Run manually', exact: true }).click()
 const dialog = page.getByRole('dialog', { name: 'Run manually', exact: true })
 await dialog.getByLabel('Message source').selectOption('json')
 await dialog.getByLabel('Payload', { exact: true }).fill('{bad')
 await expect(dialog.getByRole('button', { name: 'Send message' })).toBeDisabled()
 await dialog.getByLabel('Payload', { exact: true }).fill('{"value":42}')
 await dialog.getByRole('button', { name: 'Send message' }).click()
 await expect(dialog.getByRole('alert')).toContainText('Could not send')
 await dialog.getByRole('button', { name: 'Send message' }).click()
 await expect(dialog.getByRole('status')).toContainText('Message sent')
 expect(requests).toEqual([{ payload: { value: 42 } }, { payload: { value: 42 } }])
 await dialog.getByLabel('Message source').selectOption('configured')
 await dialog.getByRole('button', { name: 'Send message' }).click()
 await expect.poll(() => requests.length).toBe(3)
 expect(requests[2]).toEqual({})
 await dialog.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('textbox', { name: 'Flow name', exact: true }).fill('Unsaved')
 await expect(page.getByRole('button', { name: 'Run manually', exact: true })).toBeDisabled()
})
test('stopped flows expose persisted runs and node errors with retry', async ({ page }) => {
 await page.route('**/api/flows/core/runs?*', route => route.fulfill({ json: { success: true, data: { runs: [{ id: 'run-1', flowId: 'core', status: 'error', nodeCount: 2, errorCount: 1, startedAt: '2026-09-12T12:00:00Z' }] } } }))
 let attempts = 0
 await page.route('**/api/flows/core/runs/run-1/events', route => {
  attempts++
  return route.fulfill(attempts === 1 ? { status: 500, json: { error: 'Offline' } } : { json: { success: true, data: { events: [{ id: 1, runId: 'run-1', flowId: 'core', nodeId: 'n1', nodeType: 'switch', phase: 'error', durationMs: 3, error: 'Invalid expression', createdAt: '2026-09-12T12:00:01Z' }] } } })
 })
 await page.goto('/editor/core')
 await page.getByRole('button', { name: /^Runs/ }).click()
 await page.getByRole('button', { name: 'View run run-1' }).click()
 const dialog = page.getByRole('dialog', { name: 'Run details' })
 await expect(dialog.getByRole('alert')).toContainText('Could not load')
 await dialog.getByRole('button', { name: 'Retry' }).click()
 await expect(dialog).toContainText('Invalid expression')
 await expect(dialog).toContainText('Switch')
 await expect(dialog).toContainText('3 ms')
})

test('run history errors remain retryable while stopped and closing details discards late results', async ({ page }) => {
 let calls = 0
 await page.route('**/api/flows/core/runs?*', route => {
  calls++
  return route.fulfill(calls === 1 ? { status: 500, json: { error: 'Unavailable' } } : { json: { success: true, data: { runs: ['one', 'two'].map(id => ({ id, flowId: 'core', status: 'success', nodeCount: 1, errorCount: 0, startedAt: '2026-09-12T12:00:00Z' })) } } })
 })
 let release!: () => void
 const waiting = new Promise<void>(resolve => { release = resolve })
 await page.route('**/api/flows/core/runs/*/events', async route => {
  const first = route.request().url().includes('/one/')
  if (first) await waiting
  await route.fulfill({ json: { success: true, data: { events: [{ id: 1, nodeId: 'n1', nodeType: 'switch', phase: 'error', error: first ? 'Stale error' : 'Current error', createdAt: '2026-09-12T12:00:00Z' }] } } })
 })
 await page.goto('/editor/core')
 await page.getByRole('button', { name: /^Runs/ }).click()
 await expect(page.getByRole('alert')).toContainText('Could not load run history')
 await page.getByRole('button', { name: 'Retry', exact: true }).click()
 await page.getByRole('button', { name: 'View run one' }).click()
 const dialog = page.getByRole('dialog', { name: 'Run details' })
 await expect(dialog.getByRole('status')).toContainText('Loading')
 await dialog.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('button', { name: 'View run two' }).click()
 await expect(dialog).toContainText('Current error')
 const response = page.waitForResponse('**/api/flows/core/runs/one/events')
 release()
 await response
 await expect(dialog).toContainText('Current error')
 await expect(dialog).not.toContainText('Stale error')
})

test('invalid legacy branch ports are identified before deployment', async ({ page }) => {
 const flow = makeFlow()
 flow.connections[0].sourcePort = 'output'
 await page.route('**/api/flows/core', route => route.fulfill({ json: { success: true, data: { flow, running: false } } }))
 await page.goto('/editor/core')
 await expect(page.getByRole('alert')).toContainText('invalid connection')
 await expect(page.getByRole('button', { name: 'Deploy', exact: true })).toBeDisabled()
 await page.getByRole('button', { name: 'Connections', exact: true }).click()
 await expect(page.getByRole('dialog', { name: 'Connections', exact: true })).toContainText('Choose valid ports')
})
