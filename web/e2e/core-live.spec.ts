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

test('global variables retain JSON types and are used by real node previews without persisting preview writes',async({request})=>{
 const response=await request.put('/api/variables/threshold',{data:{value:25}})
 expect(response.status()).toBe(200)
 const transform=await request.post('/api/nodes/process-transform/test',{data:{config:{expression:'payload + vars["threshold"]'},payload:5}})
 expect((await transform.json()).data.outputs[0].payload).toBe(30)
 const template=await request.post('/api/nodes/process-template/test',{data:{config:{template:'Limit: {{vars.threshold}}'},payload:{}}})
 expect((await template.json()).data.outputs[0].payload).toBe('Limit: 25')
 await request.post('/api/nodes/set-var/test',{data:{config:{key:'threshold',value:'payload'},payload:100}})
 expect((await (await request.get('/api/variables')).json()).data.variables.threshold).toBe(25)
 for(const value of [false,null,{nested:[1,'two']},'text']) {
  expect((await request.put('/api/variables/typed',{data:{value}})).status()).toBe(200)
  expect((await (await request.get('/api/variables')).json()).data.variables.typed).toEqual(value)
 }
 expect((await request.put('/api/variables/invalid',{data:{}})).status()).toBe(400)
 await request.put('/api/secrets/PRIVATE',{data:{value:'hidden-value'}})
 expect(JSON.stringify(await (await request.get('/api/variables')).json())).not.toContain('hidden-value')
 expect(JSON.stringify(await (await request.get('/api/secrets')).json())).not.toContain('hidden-value')
 expect((await request.delete('/api/variables/typed')).status()).toBe(200)
 expect((await (await request.get('/api/variables')).json()).data.variables).not.toHaveProperty('typed')
})

test('flow scope connects UI, previews and deployed nodes without touching global values',async({page,request})=>{
 const created=await request.post('/api/flows',{data:{name:'Scoped variables',nodes:[
  {id:'manual',type:'trigger-manual',category:'input',config:{payload:'11'},enabled:true,position:{x:100,y:150}},
  {id:'set',type:'set-var',category:'processing',config:{scope:'flow',key:'threshold',value:'payload.data'},enabled:true,position:{x:350,y:150}},
  {id:'transform',type:'process-transform',category:'processing',config:{expression:'flowVars.threshold + vars.threshold'},enabled:true,position:{x:600,y:150}}
 ],connections:[{id:'edge',sourceNode:'manual',sourcePort:'output',targetNode:'set',targetPort:'input'}]}})
 expect(created.status()).toBe(201)
 const id=(await created.json()).data.id
 const path=`/api/flows/${id}/variables`
 await request.put('/api/variables/threshold',{data:{value:25}})
 await page.goto(`/editor/${id}`)
 await page.getByRole('button',{name:'Variables',exact:true}).click()
 const variables=page.getByRole('dialog',{name:'Flow variables',exact:true})
 await variables.getByLabel('Variable name',{exact:true}).fill('threshold')
 await variables.getByLabel('Value format').selectOption('json')
 await variables.getByLabel('Variable value',{exact:true}).fill('7')
 await variables.getByRole('button',{name:'Save variable'}).click()
 await expect(variables.getByRole('status')).toContainText('Variable saved')
 await variables.getByText('How to use variables',{exact:true}).click()
 await page.screenshot({path:'test-results/flow-variables-help.png',fullPage:true})
 await variables.getByRole('button',{name:'Done',exact:true}).click()
 await page.locator('.vue-flow__node').filter({hasText:'Transform'}).click()
 const config=page.getByRole('dialog',{name:'Node configuration'})
 const preview=page.waitForResponse(r=>r.url().includes('/process-transform/test'))
 await config.getByRole('button',{name:'Test node',exact:true}).click()
 expect((await (await preview).json()).data.outputs[0].payload).toBe(32)
 await config.getByRole('button',{name:'Cancel',exact:true}).click()
 await request.post(`/api/flows/${id}/deploy`)
 await request.post(`/api/flows/${id}/inject/manual`,{data:{payload:11}})
 await expect.poll(async()=>(await (await request.get(path)).json()).data.variables.threshold).toBe(11)
 expect((await (await request.get('/api/variables')).json()).data.variables.threshold).toBe(25)
 await request.post(`/api/flows/${id}/stop`)
 expect((await (await request.get(path)).json()).data.variables.threshold).toBe(11)
 const duplicate=await request.post(`/api/flows/${id}/duplicate`)
 expect(duplicate.status()).toBe(201)
 const copy=(await duplicate.json()).data.id
 expect((await (await request.get(`/api/flows/${copy}/variables`)).json()).data.variables).toEqual({})
 await request.delete(`/api/flows/${id}`)
 expect((await request.get(path)).status()).toBe(404)
})
