import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

type Call = { name: string; args: unknown[] }
let calls: Call[] = []
let handlers: Record<string, (...args: any[]) => unknown> = {}

function httpError(status: number, error = 'failed', data?: unknown) {
  return Object.assign(new Error(error), { response: { status, data: { success: false, error, ...(data ? { data } : {}) } } })
}
// Every API method the store uses records its call and answers with its
// handler (a missing one is a 404).
const api = new Proxy({}, {
  get: (_target, name) => typeof name !== 'string' || name === 'then' ? undefined : async (...args: unknown[]) => {
    calls.push({ name, args })
    const handler = handlers[name]
    if (!handler) throw httpError(404, 'not found')
    return handler(...args)
  }
})
// bun keeps a mocked module's export names for the whole run: export the
// same names as the other mocks of useApi.
mock.module('../src/composables/useApi', () => ({ apiClient: {}, useApi: () => api }))
const { useLLMStore } = await import('../src/stores/llm')
const { conflictFlows } = await import('../src/lib/llm')

const usage = { inputTokens: 0, outputTokens: 0, requests: 0 }
const claude = { name: 'claude', provider: 'anthropic', baseUrl: '', hasApiKey: true, model: 'claude-opus-4-5', dailyTokenLimit: 0, usageToday: usage }
const ollama = { name: 'ollama', provider: 'openai', baseUrl: 'http://localhost:11434/v1', hasApiKey: false, model: '', dailyTokenLimit: 0, usageToday: usage }
const body = { provider: 'openai' as const, baseUrl: 'http://localhost:11434/v1', model: '', dailyTokenLimit: 0 }

beforeEach(() => {
  setActivePinia(createPinia())
  calls = []
  handlers = {}
})

test('connections are listed and saved, and a save drops the cached models', async () => {
  handlers.getLLMConnections = () => [claude, ollama]
  handlers.saveLLMConnection = () => ollama
  handlers.getLLMModels = () => [{ id: 'llama3.2' }]
  const store = useLLMStore()
  await store.fetchConnections()
  expect(store.connections.map(c => c.name)).toEqual(['claude', 'ollama'])
  expect(store.connectionsLoaded).toBe(true)
  await store.loadModels('ollama')
  expect(store.models.ollama).toEqual([{ id: 'llama3.2' }])

  expect(await store.saveConnection('ollama', body)).toEqual(ollama)
  expect(calls.find(c => c.name === 'saveLLMConnection')?.args).toEqual(['ollama', body])
  expect(store.models.ollama).toBeUndefined()
  // The list is read again after the save.
  expect(calls.filter(c => c.name === 'getLLMConnections')).toHaveLength(2)
})

test('a delete refused for running flows throws with the flows, and keeps the list', async () => {
  handlers.deleteLLMConnection = () => { throw httpError(409, 'used by running flows', { flows: [{ id: 'f1', name: 'Inbox' }] }) }
  const store = useLLMStore()
  store.connections = [claude]
  let error: unknown
  try { await store.deleteConnection('claude') } catch (e) { error = e }
  expect(conflictFlows(error)).toEqual([{ id: 'f1', name: 'Inbox' }])
  expect(store.connections.map(c => c.name)).toEqual(['claude'])

  handlers.deleteLLMConnection = () => undefined
  handlers.getLLMConnections = () => []
  await store.deleteConnection('claude')
  expect(store.connections).toEqual([])
})

test('a test answers the models found or the error, storing nothing', async () => {
  handlers.testLLMConnection = (_name: string, b: { apiKey?: string }) => b.apiKey === 'right' ? { ok: true, models: [{ id: 'gpt-5' }] } : { ok: false, error: 'invalid api key' }
  const store = useLLMStore()
  expect(await store.testConnection('openai', { ...body, apiKey: 'wrong' })).toEqual({ ok: false, error: 'invalid api key' })
  expect(await store.testConnection('openai', { ...body, apiKey: 'right' })).toEqual({ ok: true, models: [{ id: 'gpt-5' }] })
  handlers.testLLMConnection = () => ({ ok: false })
  const result = await store.testConnection('openai', body)
  expect(result.ok).toBe(false)
  expect(result.error).toBeTruthy()
  expect(store.models.openai).toBeUndefined()
})

// The models of a connection are read on demand, once, and a failure (a
// local server that is down) only leaves the field free text.
test('models load once per connection and tolerate failure', async () => {
  let reads = 0
  handlers.getLLMModels = (name: string) => {
    reads++
    if (name === 'down') throw httpError(502, 'connection refused')
    return [{ id: 'claude-opus-4-5' }]
  }
  const store = useLLMStore()
  const [a, b] = await Promise.all([store.loadModels('claude'), store.loadModels('claude')])
  expect(a).toEqual([{ id: 'claude-opus-4-5' }])
  expect(b).toEqual(a)
  await store.loadModels('claude')
  expect(reads).toBe(1)
  await store.loadModels('claude', { force: true })
  expect(reads).toBe(2)

  expect(await store.loadModels('down')).toEqual([])
  expect(store.modelErrors.down).toBe('connection refused')
  expect(store.models.down).toBeUndefined()
  // A failure is not cached: asking again lists them again; a success
  // clears the error.
  expect(reads).toBe(3)
  await store.loadModels('down')
  expect(reads).toBe(4)
  handlers.getLLMModels = () => [{ id: 'llama3.2' }]
  await store.loadModels('down', { force: true })
  expect(store.modelErrors.down).toBeUndefined()
  expect(store.models.down).toEqual([{ id: 'llama3.2' }])
  expect(await store.loadModels('')).toEqual([])
})

test('conversations are listed, read and cleared', async () => {
  const info = { key: 'telegram:42', turns: 2, updatedAt: '2026-11-15T10:00:00Z' }
  handlers.getLLMConversations = () => [info, { ...info, key: 'other' }]
  handlers.getLLMConversation = (key: string) => ({ key, turns: [{ role: 'user', text: 'hi', at: info.updatedAt }] })
  handlers.deleteLLMConversation = () => undefined
  const store = useLLMStore()
  await store.fetchConversations()
  expect(store.conversations).toHaveLength(2)
  expect((await store.getConversation('telegram:42')).turns[0].text).toBe('hi')
  await store.clearConversation('telegram:42')
  expect(calls.at(-1)).toEqual({ name: 'deleteLLMConversation', args: ['telegram:42'] })
  expect(store.conversations.map(c => c.key)).toEqual(['other'])
})

test('knowledge collections and documents are browsed and deleted', async () => {
  const manuals = { name: 'manuals', documents: 2, model: 'nomic-embed-text', dimensions: 768, updatedAt: '2026-11-15T10:00:00Z' }
  handlers.getKnowledgeCollections = () => [manuals]
  handlers.getKnowledgeDocuments = (_c: string, offset: number, limit: number) => ({ documents: [{ id: `d${offset}`, text: 'x', model: 'm', createdAt: '' }], total: 2, limit })
  handlers.deleteKnowledgeDocument = () => undefined
  handlers.deleteKnowledgeCollection = () => undefined
  const store = useLLMStore()
  await store.fetchCollections()
  expect(store.collections).toEqual([manuals])
  expect(store.collectionsLoaded).toBe(true)
  const page = await store.fetchDocuments('manuals', 1, 1)
  expect(page.documents[0].id).toBe('d1')
  expect(calls.at(-1)).toEqual({ name: 'getKnowledgeDocuments', args: ['manuals', 1, 1] })

  await store.deleteDocument('manuals', 'd1')
  expect(calls.some(c => c.name === 'deleteKnowledgeDocument' && c.args[1] === 'd1')).toBe(true)
  // The counts are read again.
  expect(calls.at(-1)?.name).toBe('getKnowledgeCollections')
  handlers.getKnowledgeCollections = () => []
  await store.deleteCollection('manuals')
  expect(store.collections).toEqual([])
})

test('published tools are listed; a server without them lists none', async () => {
  handlers.getAgentTools = () => ({ version: 2, tools: [{ toolName: 'lights_on' }] })
  const store = useLLMStore()
  await store.fetchTools()
  expect(store.tools.map(t => t.toolName)).toEqual(['lights_on'])
  expect(store.toolNames).toEqual(['lights_on'])
  expect(store.toolsLoaded).toBe(true)
  delete handlers.getAgentTools
  await store.fetchTools()
  expect(store.tools).toEqual([])
  expect(store.toolsLoaded).toBe(true)
})

test('suggestions load what the form needs once, and report failures', async () => {
  handlers.getLLMConnections = () => [claude]
  handlers.getLLMModels = () => [{ id: 'claude-opus-4-5' }]
  handlers.getKnowledgeCollections = () => { throw httpError(500) }
  const store = useLLMStore()
  expect(await store.loadSuggestions({ llmConnections: true, models: 'claude' })).toBe(true)
  expect(store.models.claude).toEqual([{ id: 'claude-opus-4-5' }])
  // Already loaded: nothing is read again unless forced.
  const before = calls.length
  expect(await store.loadSuggestions({ llmConnections: true, models: 'claude' })).toBe(true)
  expect(calls.length).toBe(before)
  await store.loadSuggestions({ llmConnections: true, force: true })
  expect(calls.length).toBe(before + 1)
  // A failed list is reported; a failed model list is not (the field says it).
  expect(await store.loadSuggestions({ collections: true })).toBe(false)
  handlers.getLLMModels = () => { throw httpError(502) }
  expect(await store.loadSuggestions({ models: 'claude', force: true })).toBe(true)
  expect(store.modelErrors.claude).toBeUndefined()
  store.models = {}
  expect(await store.loadSuggestions({ models: 'claude' })).toBe(true)
  expect(store.modelErrors.claude).toBeTruthy()
  // A name that is no defined connection (being typed) lists nothing.
  const reads = calls.filter(c => c.name === 'getLLMModels').length
  await store.loadSuggestions({ models: 'cla' })
  expect(calls.filter(c => c.name === 'getLLMModels').length).toBe(reads)
})
