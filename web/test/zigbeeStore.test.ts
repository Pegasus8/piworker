import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

type Call = { method: string; url: string; body?: unknown; headers?: Record<string, string> }
let calls: Call[] = []
let routes: Record<string, (call: Call) => unknown> = {}

function httpError(status: number, error = 'failed') {
  return Object.assign(new Error(error), { response: { status, data: { success: false, error } } })
}
async function respond(call: Call) {
  calls.push(call)
  const handler = routes[`${call.method} ${call.url}`]
  if (!handler) throw httpError(404, 'not found')
  return { data: { success: true, data: handler(call) } }
}
const apiClient = {
  get: (url: string, config?: { headers?: Record<string, string> }) => respond({ method: 'GET', url, ...(config?.headers ? { headers: config.headers } : {}) }),
  post: (url: string, body?: unknown) => respond({ method: 'POST', url, body }),
  put: (url: string, body?: unknown) => respond({ method: 'PUT', url, body }),
  delete: (url: string) => respond({ method: 'DELETE', url })
}
mock.module('../src/composables/useApi', () => ({ apiClient, useApi: () => ({}) }))
const { useZigbeeStore } = await import('../src/stores/zigbee')

const home = { name: 'home', broker: 'tcp://10.0.0.2:1883', username: 'pi', hasPassword: true, baseTopic: 'zigbee2mqtt' }
const network = { connection: 'home', baseTopic: 'zigbee2mqtt', connected: true, bridge: { online: true, info: { version: '2.1.0', permitJoin: false } }, devices: [] }

beforeEach(() => {
  setActivePinia(createPinia())
  calls = []
  routes = {}
})

test('lists connections from the envelope', async () => {
  routes['GET /connections'] = () => ({ connections: [home] })
  const store = useZigbeeStore()
  await store.fetchConnections()
  expect(store.connections).toEqual([home])
})

test('saving keeps the stored password unless one is typed or cleared, then reloads', async () => {
  routes['PUT /connections/my%20home'] = () => null
  routes['GET /connections'] = () => ({ connections: [home] })
  const store = useZigbeeStore()
  await store.saveConnection({ name: 'my home', broker: 'tcp://10.0.0.2:1883', username: 'pi', password: '', clearPassword: false, baseTopic: '' })
  expect(calls[0]).toEqual({ method: 'PUT', url: '/connections/my%20home', body: { broker: 'tcp://10.0.0.2:1883', username: 'pi', baseTopic: 'zigbee2mqtt' } })
  expect(calls[1].url).toBe('/connections')
})

test('testing a connection returns ok or the broker error and stores nothing', async () => {
  routes['POST /connections/home/test'] = call => (call.body as { password?: string }).password === 'right' ? { ok: true } : { ok: false, error: 'not authorized' }
  const store = useZigbeeStore()
  const form = { name: 'home', broker: 'tcp://10.0.0.2:1883', username: 'pi', password: 'wrong', clearPassword: false, baseTopic: 'zigbee2mqtt' }
  expect(await store.testConnection(form)).toEqual({ ok: false, error: 'not authorized' })
  expect(await store.testConnection({ ...form, password: 'right' })).toEqual({ ok: true })
  expect(calls.every(c => c.method === 'POST')).toBe(true)
})

test('deleting a connection reloads the list', async () => {
  routes['DELETE /connections/home'] = () => null
  routes['GET /connections'] = () => ({ connections: [] })
  const store = useZigbeeStore()
  store.connections = [home]
  await store.deleteConnection('home')
  expect(store.connections).toEqual([])
})

test('background network reads do not extend the session', async () => {
  routes['GET /zigbee'] = () => ({ networks: [network] })
  const store = useZigbeeStore()
  await store.fetchNetworks()
  expect(calls[0].headers).toBeUndefined()
  await store.fetchNetworks({ background: true })
  expect(calls[1].headers).toEqual({ 'X-PiWorker-Background': '1' })
  expect(store.networks[0].connection).toBe('home')
  expect(store.networksLoaded).toBe(true)
})

test('permit join sends the seconds and refreshes the network', async () => {
  routes['POST /zigbee/home/permit-join'] = () => null
  routes['GET /zigbee'] = () => ({ networks: [network] })
  const store = useZigbeeStore()
  await store.permitJoin('home', 240)
  expect(calls[0]).toEqual({ method: 'POST', url: '/zigbee/home/permit-join', body: { seconds: 240 } })
  expect(calls[1].url).toBe('/zigbee')
})

test('suggestions load once, and a failure leaves the lists empty without throwing', async () => {
  routes['GET /connections'] = () => ({ connections: [home] })
  const store = useZigbeeStore()
  expect(await store.loadSuggestions({ connections: true, networks: true })).toBe(false)
  expect(store.connections).toEqual([home])
  expect(store.networks).toEqual([])
  routes['GET /zigbee'] = () => ({ networks: [network] })
  expect(await store.loadSuggestions({ connections: true, networks: true })).toBe(true)
  const before = calls.length
  expect(await store.loadSuggestions({ connections: true, networks: true })).toBe(true)
  expect(calls.length).toBe(before)
  // force: read again (for example a connection the network list does not know yet).
  await store.loadSuggestions({ networks: true, force: true })
  expect(calls.length).toBe(before + 1)
})
