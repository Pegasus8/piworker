import { test, expect } from '@playwright/test'
import { spawn, execFileSync, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
let server: ChildProcess
let directory: string
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('PIWORKER_')))
test.beforeAll(async ({ request }) => {
 directory = mkdtempSync(join(tmpdir(), 'piworker-core-'))
 const binary = join(directory, 'piworker')
 execFileSync('go', ['build', '-o', binary, '.'], { cwd: resolve(process.cwd(), '..'), env })
 server = spawn(binary, ['-auth=false', '-db', join(directory, 'core.db'), '-addr', '127.0.0.1:18095'], { cwd: directory, env, stdio: 'ignore' })
 await expect.poll(async () => { try { return (await request.get('/api/health')).status() } catch { return 0 } }, { timeout: 15000 }).toBe(200)
})
test.afterAll(async () => {
 if (server && server.exitCode === null) await new Promise<void>(resolve => { server.once('exit', () => resolve()); server.kill('SIGTERM') })
 if (directory) rmSync(directory, { recursive: true, force: true })
})
test('saved branches execute from the editor and errors remain inspectable after stopping', async ({ page, request }) => {
 const types = (await (await request.get('/api/node-types')).json()).data.nodeTypes
 const specs = [['manual', 'trigger-manual', { payload: 'true' }], ['switch', 'process-switch', { condition: 'payload.data == true' }], ['yes', 'process-debug', {}], ['no', 'process-transform', { expression: 'payload.missing.value' }]] as const
 const nodes = specs.map(([id, type, config], i) => ({ id, type, config, category: types.find((t: any) => t.type === type).category, enabled: true, position: { x: i * 250, y: 150 } }))
 const connections = [ ['manual', 'output', 'switch'], ['switch', 'true', 'yes'], ['switch', 'false', 'no'] ].map(([sourceNode, sourcePort, targetNode], i) => ({ id: `edge${i}`, sourceNode, sourcePort, targetNode, targetPort: 'input' }))
 const created = await request.post('/api/flows', { data: { name: 'Live branch lab', nodes, connections } })
 expect(created.status()).toBe(201)
 const flowId = (await created.json()).data.id
 await page.goto(`/editor/${flowId}`)
 await page.getByRole('textbox', { name: 'Flow name', exact: true }).fill('Live core verified')
 await page.getByRole('button', { name: 'Save', exact: true }).click()
 await expect(page.getByRole('status')).toContainText('Flow saved')
 await page.reload()
 await expect(page.locator('[data-nodeid="switch"][data-handleid="false"]')).toBeVisible()
 await page.getByRole('button', { name: 'Deploy', exact: true }).click()
 await expect(page.getByText('Running', { exact: true })).toBeVisible()
 await page.getByRole('button', { name: 'Run manually', exact: true }).click()
 const manual = page.getByRole('dialog', { name: 'Run manually', exact: true })
 await manual.getByRole('button', { name: 'Send message' }).click()
 await expect(manual.getByRole('status')).toContainText('Message sent')
 await expect.poll(async () => (await (await request.get(`/api/flows/${flowId}/runs`)).json()).data.runs[0]?.status).toBe('success')
 await manual.getByLabel('Message source').selectOption('json')
 await manual.getByLabel('Payload', { exact: true }).fill('false')
 await manual.getByRole('button', { name: 'Send message' }).click()
 await expect.poll(async () => (await (await request.get(`/api/flows/${flowId}/runs`)).json()).data.runs[0]?.status, { timeout: 15000 }).toBe('error')
 await manual.getByRole('button', { name: 'Done' }).click()
 await page.getByRole('button', { name: 'Stop', exact: true }).click()
 await page.reload()
 await expect(page.getByText('Stopped', { exact: true })).toBeVisible()
 await page.getByRole('button', { name: /^Runs/ }).click()
 const runs = (await (await request.get(`/api/flows/${flowId}/runs`)).json()).data.runs
 expect(runs).toHaveLength(2)
 await page.getByRole('button', { name: `View run ${runs[0].id}` }).click()
 const details = page.getByRole('dialog', { name: 'Run details' })
 await expect(details).toContainText('expression evaluation failed')
 await expect(details).toContainText('Transform')
 await page.screenshot({ path: 'test-results/core-run-details.png', fullPage: true })
})
