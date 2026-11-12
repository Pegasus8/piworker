import { test, expect } from '@playwright/test'
const nodeTypes = [
 { type: 'timer', name: 'Timer', description: 'Starts a flow', category: 'input', outputs: ['output'], config: { properties: { interval: { type: 'number', default: 10 } } } },
 { type: 'log', name: 'Log', description: 'Writes a message', category: 'output', inputs: [{ id: 'input', name: 'Input', multiple: false }], config: { properties: { message: { type: 'string' } } } }
]
const flow = {
 id: 'f1', name: 'Connected', enabled: false, updatedAt: '2026-09-12', createdAt: '2026-09-12', version: 1,
 nodes: [
  { id: 't', type: 'timer', category: 'input', position: { x: 0, y: 0 }, config: { interval: 10 } },
  { id: 'l', type: 'log', category: 'output', position: { x: 300, y: 0 }, config: { message: 'hi' } }
 ],
 connections: [{ id: 'e1', sourceNode: 't', sourcePort: 'output', targetNode: 'l', targetPort: 'input' }]
}
test.beforeEach(async ({ page }) => {
 page.setDefaultTimeout(5000)
 await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname
  const data = path.endsWith('/auth/status') ? { enabled: false } : path.endsWith('/node-types') ? { nodeTypes } : path.endsWith('/flows') ? { flows: [flow] } : path.endsWith('/flows/f1') ? { flow, running: false } : []
  return route.fulfill({ json: { success: true, data } })
 })
})
test('saved connections are drawn on the canvas', async ({ page }) => {
 await page.goto('/editor/f1')
 await expect(page.locator('.vue-flow__node')).toHaveCount(2)
 await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
})
