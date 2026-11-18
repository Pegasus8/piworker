import { expect, mock, test } from 'bun:test'

const posts: { url: string; body: unknown; config?: unknown }[] = []
const gets: { url: string; config: unknown }[] = []
const patches: { url: string; body: unknown }[] = []
const puts: { url: string; body: unknown }[] = []
const deletes: string[] = []
let getData: unknown = null
let postData: unknown = null
let putData: unknown = null
const instance = {
  interceptors: { response: { use: () => 0 } },
  post: async (url: string, body: unknown, config?: unknown) => { posts.push({ url, body, ...(config ? { config } : {}) }); return { data: { success: true, data: postData } } },
  put: async (url: string, body: unknown) => { puts.push({ url, body }); return { data: { success: true, data: putData } } },
  delete: async (url: string) => { deletes.push(url); return { data: { success: true, data: null } } },
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
  await api.getSecrets(true)
  expect(gets.at(-1)).toEqual({ url: '/secrets', config: { params: { usage: 1 } } })
  getData = null
  expect(await api.getSecrets()).toEqual({ secrets: [], undefined: [], shortBelow: 16 })
})

// LLM connections, memory, knowledge and the assistant (docs/design/llm.md §6).
const usage = { inputTokens: 10, outputTokens: 5, requests: 1 }
const stored = { name: 'my claude', provider: 'anthropic', baseUrl: '', hasApiKey: true, model: 'claude-opus-4-5', dailyTokenLimit: 0, usageToday: usage }

test('LLM connections use their routes with encoded names', async () => {
  const api = useApi()
  getData = { connections: [stored] }
  expect(await api.getLLMConnections()).toEqual([stored])
  expect(gets.at(-1)?.url).toBe('/llm/connections')
  getData = { connections: null }
  expect(await api.getLLMConnections()).toEqual([])

  const body = { provider: 'anthropic' as const, baseUrl: '', model: 'claude-opus-4-5', dailyTokenLimit: 0 }
  putData = stored
  expect(await api.saveLLMConnection('my claude', body)).toEqual(stored)
  expect(puts.at(-1)).toEqual({ url: '/llm/connections/my%20claude', body })

  await api.deleteLLMConnection('my claude')
  expect(deletes.at(-1)).toBe('/llm/connections/my%20claude')

  postData = { ok: true, models: [{ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' }] }
  expect(await api.testLLMConnection('my claude', { ...body, apiKey: '{{secret.claude}}' })).toEqual({ ok: true, models: [{ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' }] })
  expect(posts.at(-1)).toMatchObject({ url: '/llm/connections/my%20claude/test', body: { ...body, apiKey: '{{secret.claude}}' } })
  postData = { ok: false, error: 'invalid x-api-key' }
  expect(await api.testLLMConnection('my claude', body)).toEqual({ ok: false, error: 'invalid x-api-key' })

  getData = { models: [{ id: 'llama3.2' }] }
  expect(await api.getLLMModels('ollama')).toEqual([{ id: 'llama3.2' }])
  expect(gets.at(-1)?.url).toBe('/llm/connections/ollama/models')
  getData = {}
  expect(await api.getLLMModels('ollama')).toEqual([])
})

test('conversations are read and cleared by their escaped key', async () => {
  const api = useApi()
  getData = { conversations: [{ key: 'telegram:42', turns: 4, updatedAt: '2026-11-15T10:00:00Z' }] }
  expect(await api.getLLMConversations()).toEqual([{ key: 'telegram:42', turns: 4, updatedAt: '2026-11-15T10:00:00Z' }])
  expect(gets.at(-1)?.url).toBe('/llm/conversations')
  getData = { key: 'chat/a b', turns: [{ role: 'user', text: 'hi', at: '2026-11-15T10:00:00Z' }] }
  expect((await api.getLLMConversation('chat/a b')).turns).toHaveLength(1)
  expect(gets.at(-1)?.url).toBe('/llm/conversations/chat%2Fa%20b')
  getData = { key: 'empty', turns: null }
  expect(await api.getLLMConversation('empty')).toEqual({ key: 'empty', turns: [] })
  await api.deleteLLMConversation('telegram:42')
  expect(deletes.at(-1)).toBe('/llm/conversations/telegram%3A42')
})

test('knowledge collections and documents are browsed by page and deleted', async () => {
  const api = useApi()
  getData = { collections: [{ name: 'manuals', documents: 2, model: 'nomic-embed-text', dimensions: 768, updatedAt: '2026-11-15T10:00:00Z' }] }
  expect((await api.getKnowledgeCollections())[0].name).toBe('manuals')
  expect(gets.at(-1)?.url).toBe('/llm/knowledge')
  getData = { documents: [{ id: 'a#0', text: 'x', model: 'nomic-embed-text', createdAt: '2026-11-15T10:00:00Z' }], total: 7 }
  expect(await api.getKnowledgeDocuments('manuals', 50, 25)).toEqual({ documents: [{ id: 'a#0', text: 'x', model: 'nomic-embed-text', createdAt: '2026-11-15T10:00:00Z' }], total: 7 })
  expect(gets.at(-1)).toEqual({ url: '/llm/knowledge/manuals', config: { params: { offset: 50, limit: 25 } } })
  getData = { documents: null }
  expect(await api.getKnowledgeDocuments('manuals')).toEqual({ documents: [], total: 0 })
  await api.deleteKnowledgeCollection('manuals')
  expect(deletes.at(-1)).toBe('/llm/knowledge/manuals')
  await api.deleteKnowledgeDocument('manuals', 'a#0')
  expect(deletes.at(-1)).toBe('/llm/knowledge/manuals/documents/a%230')
})

test('the flow assistant sends the description and the current flow', async () => {
  const api = useApi()
  const flow = { name: 'Inbox', description: '', nodes: [], connections: [] }
  postData = { flow: { ...flow, nodes: null, connections: null }, notes: 'Set the chat ID.', issues: null, usage }
  const signal = new AbortController().signal
  const result = await api.assistFlow({ connection: 'claude', description: 'Summarize my RSS feeds', flow }, signal)
  expect(posts.at(-1)).toEqual({ url: '/llm/assistant/flow', body: { connection: 'claude', description: 'Summarize my RSS feeds', flow }, config: { signal } })
  expect(result).toEqual({ flow, notes: 'Set the chat ID.', issues: [], usage })
})

test('the published tools come from the agent tools route', async () => {
  const api = useApi()
  getData = { version: 3, tools: null }
  expect(await api.getAgentTools()).toEqual({ version: 3, tools: [] })
  expect(gets.at(-1)?.url).toBe('/agent/tools')
})
