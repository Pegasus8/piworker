// LLM connections, their models, the conversation memory, the knowledge
// base and the published tools (docs/design/llm.md §6, §8): the LLM
// dialogs, the node settings' suggestions and the flow assistant share these
// lists. Admin session only; API keys never reach the browser.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { useApi } from '@/composables/useApi'
import { msg } from '@/i18n'
import { errorMessage } from '@/lib/llm'
import type { AgentTool } from '@/lib/agentAccess'
import type {
  KnowledgeCollection,
  KnowledgeDocumentPage,
  LLMConnection,
  LLMConnectionRequest,
  LLMConnectionTest,
  LLMConversation,
  LLMConversationInfo,
  LLMModel
} from '@/types'

function without<T>(record: Record<string, T>, key: string): Record<string, T> {
  const copy = { ...record }
  delete copy[key]
  return copy
}

const status = (e: unknown) => (e as { response?: { status?: number } })?.response?.status

/** What a form's suggestions need: lists, and the models of one connection. */
export interface LLMSuggestionNeeds {
  llmConnections?: boolean
  /** The connection whose models are suggested, when it is defined ('' or omitted: none). */
  models?: string
  tools?: boolean
  collections?: boolean
  /** Read the lists again even when loaded (listed models stay cached; failed ones are always listed again). */
  force?: boolean
}

export const useLLMStore = defineStore('llm', () => {
  const api = useApi()

  const connections = ref<LLMConnection[]>([])
  const connectionsLoaded = ref(false)
  /** The models of each connection, as last listed (absent: not listed, or the listing failed). */
  const models = ref<Record<string, LLMModel[]>>({})
  /** Why listing a connection's models failed, until a listing succeeds. */
  const modelErrors = ref<Record<string, string>>({})
  const conversations = ref<LLMConversationInfo[]>([])
  const collections = ref<KnowledgeCollection[]>([])
  const collectionsLoaded = ref(false)
  const tools = ref<AgentTool[]>([])
  const toolsLoaded = ref(false)
  const toolNames = computed(() => [...new Set(tools.value.map(t => t.toolName))])

  async function fetchConnections() {
    connections.value = await api.getLLMConnections()
    connectionsLoaded.value = true
  }

  // One listing per connection at a time, and how many times each
  // connection's models were forgotten: a listing started before is
  // dropped when it ends (it listed settings that no longer exist).
  const modelReads = new Map<string, Promise<LLMModel[]>>()
  const modelGenerations = new Map<string, number>()

  // A connection's settings changed or it is gone: its models are listed
  // again the next time they are needed, and a listing still running with
  // the old settings is not kept.
  function forgetModels(name: string) {
    modelGenerations.set(name, (modelGenerations.get(name) ?? 0) + 1)
    modelReads.delete(name)
    models.value = without(models.value, name)
    modelErrors.value = without(modelErrors.value, name)
  }

  /** Creates or replaces a connection (no apiKey keeps the stored one) and reads the list again. */
  async function saveConnection(name: string, body: LLMConnectionRequest): Promise<LLMConnection> {
    const stored = await api.saveLLMConnection(name, body)
    forgetModels(name)
    await fetchConnections()
    return stored
  }

  /**
   * Deletes a connection. The server refuses (409) while a running flow
   * uses it and names them: the error is thrown as it is, for
   * conflictFlows (lib/llm.ts); the list stays as it was.
   */
  async function deleteConnection(name: string) {
    await api.deleteLLMConnection(name)
    forgetModels(name)
    await fetchConnections()
  }

  /** Lists the models with these settings (the stored key when none is typed); spends no tokens, stores nothing. */
  async function testConnection(name: string, body: LLMConnectionRequest): Promise<LLMConnectionTest> {
    const result = await api.testLLMConnection(name, body)
    if (result?.ok === true) return { ok: true, models: result.models ?? [] }
    return { ok: false, error: typeof result?.error === 'string' && result.error ? result.error : msg().editor.llm.testFailed }
  }

  /**
   * The models of a stored connection, listed once and cached. Never
   * throws: a failure (a local server that is down, a wrong key) is kept in
   * modelErrors and answers none, so the field stays free text. A failure
   * is not cached: asking again lists them again. A listing that ends after
   * the connection was saved or deleted keeps nothing and answers none.
   */
  function loadModels(name: string, opts: { force?: boolean } = {}): Promise<LLMModel[]> {
    if (!name) return Promise.resolve([])
    const cached = models.value[name]
    if (cached && !opts.force) return Promise.resolve(cached)
    const pending = modelReads.get(name)
    if (pending) return pending
    const generation = modelGenerations.get(name) ?? 0
    const current = () => (modelGenerations.get(name) ?? 0) === generation
    const read = (async (): Promise<LLMModel[]> => {
      try {
        const list = await api.getLLMModels(name)
        if (!current()) return []
        models.value = { ...models.value, [name]: list }
        modelErrors.value = without(modelErrors.value, name)
        return list
      } catch (e) {
        if (!current()) return []
        modelErrors.value = { ...modelErrors.value, [name]: errorMessage(e, msg().editor.llm.modelsUnavailable) }
        return []
      }
    })()
    modelReads.set(name, read)
    // Never rejects; the listing is shared until it ends (unless a newer
    // one replaced it), even when it ends at once.
    void read.finally(() => { if (modelReads.get(name) === read) modelReads.delete(name) })
    return read
  }

  // --- Conversation memory ---------------------------------------------------

  async function fetchConversations() {
    conversations.value = await api.getLLMConversations()
  }

  function getConversation(key: string): Promise<LLMConversation> {
    return api.getLLMConversation(key)
  }

  async function clearConversation(key: string) {
    await api.deleteLLMConversation(key)
    conversations.value = conversations.value.filter(c => c.key !== key)
  }

  // --- Knowledge base -----------------------------------------------------

  async function fetchCollections() {
    collections.value = await api.getKnowledgeCollections()
    collectionsLoaded.value = true
  }

  function fetchDocuments(collection: string, offset = 0, limit = 50): Promise<KnowledgeDocumentPage> {
    return api.getKnowledgeDocuments(collection, offset, limit)
  }

  /** Deletes one document, then reads the collections again (their counts changed; an emptied one is gone). */
  async function deleteDocument(collection: string, id: string) {
    await api.deleteKnowledgeDocument(collection, id)
    await fetchCollections()
  }

  async function deleteCollection(collection: string) {
    await api.deleteKnowledgeCollection(collection)
    await fetchCollections()
  }

  // --- Published tools ------------------------------------------------------

  /** The tools running flows publish. A server without the route (404) has none. */
  async function fetchTools() {
    try {
      tools.value = (await api.getAgentTools()).tools
    } catch (e) {
      if (status(e) !== 404) throw e
      tools.value = []
    }
    toolsLoaded.value = true
  }

  /**
   * Loads what the node settings suggest, each list once unless forced.
   * Never throws: a list that fails stays as it was and the fields stay
   * free text. Returns whether every list asked for loaded; the models of
   * a connection do not count (the field says when they could not be
   * listed).
   */
  async function loadSuggestions(need: LLMSuggestionNeeds): Promise<boolean> {
    const reads: Promise<unknown>[] = []
    if (need.llmConnections && (need.force || !connectionsLoaded.value)) reads.push(fetchConnections())
    if (need.tools && (need.force || !toolsLoaded.value)) reads.push(fetchTools())
    if (need.collections && (need.force || !collectionsLoaded.value)) reads.push(fetchCollections())
    const results = await Promise.allSettled(reads)
    // Only a defined connection has models to list (a name being typed has none).
    if (need.models && connections.value.some(c => c.name === need.models)) await loadModels(need.models)
    return results.every(r => r.status === 'fulfilled')
  }

  return {
    connections,
    connectionsLoaded,
    models,
    modelErrors,
    conversations,
    collections,
    collectionsLoaded,
    tools,
    toolsLoaded,
    toolNames,
    fetchConnections,
    saveConnection,
    deleteConnection,
    testConnection,
    loadModels,
    fetchConversations,
    getConversation,
    clearConversation,
    fetchCollections,
    fetchDocuments,
    deleteDocument,
    deleteCollection,
    fetchTools,
    loadSuggestions
  }
})
