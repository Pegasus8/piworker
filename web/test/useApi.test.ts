import { expect, mock, test } from 'bun:test'

const posts: { url: string; body: unknown }[] = []
const gets: { url: string; config: unknown }[] = []
const patches: { url: string; body: unknown }[] = []
let getData: unknown = null
const instance = {
  interceptors: { response: { use: () => 0 } },
  post: async (url: string, body: unknown) => { posts.push({ url, body }); return { data: { success: true, data: null } } },
  get: async (url: string, config?: unknown) => { gets.push({ url, config }); return { data: { success: true, data: getData } } },
  patch: async (url: string, body: unknown) => { patches.push({ url, body }); return { data: { success: true, data: { running: true } } } }
}
mock.module('axios', () => ({ default: { create: () => instance } }))
// Other test files replace this module with fakes; load the real one.
const { useApi } = await import('../src/composables/useApi.ts?real')

test('a password change revokes agent tokens unless asked to keep them', async () => {
  const api = useApi()
  await api.changePassword('old-password', 'new-password')
  expect(posts.at(-1)).toEqual({ url: '/auth/password', body: { currentPassword: 'old-password', password: 'new-password', keepAgentTokens: false } })
  await api.changePassword('old-password', 'new-password', true)
  expect(posts.at(-1)).toEqual({ url: '/auth/password', body: { currentPassword: 'old-password', password: 'new-password', keepAgentTokens: true } })
})

test('the flow list keeps each flow version and its agent marks', async () => {
  const api = useApi()
  const deployment = { version: 4, actorKind: 'agent', tokenName: 'laptop', deployedAt: '2026-11-08T12:00:00Z' }
  getData = { flows: [
    { id: 'f1', name: 'Garage', state: 'running', nodes: [{}], version: 4, deployment, agentChanged: true },
    { id: 'f2', name: 'Old', state: 'inactive', nodes: [] }
  ], total: 2 }
  const flows = await api.getFlows()
  expect(flows[0]).toMatchObject({ id: 'f1', running: true, nodeCount: 1, version: 4, deployment, agentChanged: true })
  // An older server without marks: none.
  expect(flows[1]).toMatchObject({ version: undefined, deployment: null, agentChanged: false })
})

test('turning a flow on sends the version the list shows', async () => {
  const api = useApi()
  await api.toggleFlow('f1', true, 4)
  expect(patches.at(-1)).toEqual({ url: '/flows/f1/toggle', body: { enabled: true, version: 4 } })
  await api.toggleFlow('f1', false, 4)
  expect(patches.at(-1)).toEqual({ url: '/flows/f1/toggle', body: { enabled: false } })
  await api.toggleFlow('f1', true)
  expect(patches.at(-1)).toEqual({ url: '/flows/f1/toggle', body: { enabled: true } })
})

test('versions and rollback use the flow routes with encoded IDs', async () => {
  const api = useApi()
  getData = { current: 5, lastAdminApproved: 3, versions: null }
  const versions = await api.getFlowVersions('f/1', 20)
  expect(gets.at(-1)).toEqual({ url: '/flows/f%2F1/versions', config: { params: { limit: 20 } } })
  expect(versions.versions).toEqual([])
  await api.rollbackFlow('f/1')
  expect(posts.at(-1)).toEqual({ url: '/flows/f%2F1/rollback', body: undefined })
})

test('the secrets listing reads names and short marks, never values', async () => {
  const api = useApi()
  getData = { secrets: [{ name: 'pin', short: true }, { name: 'api_key', short: false, value: 'leak' }], shortBelow: 16 }
  expect(await api.getSecrets()).toEqual({ secrets: [{ name: 'pin', short: true, uses: [] }, { name: 'api_key', short: false, uses: [] }], undefined: [], shortBelow: 16 })
  expect(gets.at(-1)).toEqual({ url: '/secrets', config: undefined })
  getData = null
  expect(await api.getSecrets()).toEqual({ secrets: [], undefined: [], shortBelow: 16 })
})
