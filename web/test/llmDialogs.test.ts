import { beforeAll, expect, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '../src/i18n'
import en from '../src/i18n/en'
import es from '../src/i18n/es'
import {
  applyPreset,
  baseUrlError,
  blankConnectionForm,
  connectionFormError,
  connectionFormFrom,
  isSecretReference,
  modelSuggestions,
  usageSummary,
  type ConnectionFormState
} from '../src/components/llm/connections'
import { connectionKind } from '../src/lib/llm'
import { DOCUMENT_PAGE, EXAMPLE_MEMORY_KEY, filterConversations, focusCandidates, focusRowAfter, metadataText, offsetAfterDelete, pageInfo, rowControlId, textPreview } from '../src/components/llm/memory'
import { useShellStore } from '../src/stores/shell'
import type { LLMConnection } from '../src/types'

beforeAll(() => setLocale('en'))

const usage = { inputTokens: 0, outputTokens: 0, requests: 0 }
const stored = (c: Partial<LLMConnection>): LLMConnection =>
  ({ name: 'c', provider: 'openai', baseUrl: '', hasApiKey: false, model: '', dailyTokenLimit: 0, usageToday: usage, ...c })
const t = () => en.llm.connections.form

// --- The connections form ----------------------------------------------------

test('a new connection starts from the Anthropic preset with nothing typed', () => {
  expect(blankConnectionForm()).toEqual({ preset: 'anthropic', name: '', provider: 'anthropic', baseUrl: '', apiKey: '', clearApiKey: false, model: '', dailyTokenLimit: undefined })
  expect(blankConnectionForm('ollama')).toMatchObject({ preset: 'ollama', provider: 'openai', baseUrl: 'http://localhost:11434/v1' })
})

test('a preset fills the provider and endpoint and keeps what was typed', () => {
  const typed: ConnectionFormState = { ...blankConnectionForm(), name: 'local', apiKey: 'k', model: 'm', dailyTokenLimit: 10 }
  expect(applyPreset(typed, 'lmstudio')).toEqual({ ...typed, preset: 'lmstudio', provider: 'openai', baseUrl: 'http://localhost:1234/v1' })
  expect(applyPreset(typed, 'openrouter')).toMatchObject({ provider: 'openai', baseUrl: 'https://openrouter.ai/api/v1' })
  // Another server keeps the provider and the address, to be edited.
  const ollama = applyPreset(typed, 'ollama')
  expect(applyPreset(ollama, 'custom')).toEqual({ ...ollama, preset: 'custom' })
})

test('editing starts from the stored settings, never the key', () => {
  expect(connectionFormFrom(stored({ name: 'claude', provider: 'anthropic', hasApiKey: true, model: 'claude-x', dailyTokenLimit: 50000 })))
    .toEqual({ preset: 'anthropic', name: 'claude', provider: 'anthropic', baseUrl: '', apiKey: '', clearApiKey: false, model: 'claude-x', dailyTokenLimit: 50000 })
  // No limit is an empty field, a server elsewhere on the LAN another server.
  expect(connectionFormFrom(stored({ baseUrl: 'http://192.168.1.20:11434/v1' }))).toMatchObject({ preset: 'custom', dailyTokenLimit: undefined })
  expect(connectionFormFrom(stored({ baseUrl: 'http://localhost:11434/v1' })).preset).toBe('ollama')
})

test('the base URL is checked as the server checks it', () => {
  expect(baseUrlError('')).toBe('')
  expect(baseUrlError('http://192.168.1.20:11434/v1')).toBe('')
  expect(baseUrlError(' https://openrouter.ai/api/v1/ ')).toBe('')
  expect(baseUrlError('localhost:11434')).toBe(t().baseUrlFormat)
  expect(baseUrlError('ftp://host/v1')).toBe(t().baseUrlFormat)
  expect(baseUrlError('not a url')).toBe(t().baseUrlFormat)
  expect(baseUrlError('http://user:pw@host/v1')).toBe(t().baseUrlCredentials)
  expect(baseUrlError('http://host/v1?key=1')).toBe(t().baseUrlQuery)
  expect(baseUrlError('http://host/v1#x')).toBe(t().baseUrlQuery)
})

test('what is wrong with the form comes first, before anything is sent', () => {
  const ok: ConnectionFormState = { ...blankConnectionForm('ollama'), name: 'ollama' }
  expect(connectionFormError(ok)).toBe('')
  expect(connectionFormError({ ...ok, name: ' ' })).toBe(t().nameRequired)
  expect(connectionFormError({ ...ok, name: 'my model' })).toBe(t().nameChars)
  expect(connectionFormError({ ...ok, name: 'x'.repeat(65) })).toBe(t().nameChars)
  expect(connectionFormError({ ...ok, baseUrl: 'ftp://x' })).toBe(t().baseUrlFormat)
  // Another server needs its address (an empty one would be OpenAI's).
  expect(connectionFormError({ ...ok, preset: 'custom', baseUrl: '' })).toBe(t().baseUrlRequired)
  expect(connectionFormError({ ...ok, dailyTokenLimit: -1 })).toBe(t().limitInvalid)
  expect(connectionFormError({ ...ok, dailyTokenLimit: 1.5 })).toBe(t().limitInvalid)
  expect(connectionFormError({ ...ok, dailyTokenLimit: 0 })).toBe('')
})

test('an Anthropic connection needs a key: typed, or stored and kept', () => {
  const claude: ConnectionFormState = { ...blankConnectionForm(), name: 'claude' }
  expect(connectionFormError(claude)).toBe(t().keyRequired)
  expect(connectionFormError({ ...claude, apiKey: '{{secret.anthropic}}' })).toBe('')
  expect(connectionFormError(claude, { hasApiKey: true })).toBe('')
  expect(connectionFormError({ ...claude, clearApiKey: true }, { hasApiKey: true })).toBe(t().keyRequired)
  // OpenAI-compatible servers may need none.
  expect(connectionFormError({ ...claude, provider: 'openai', preset: 'openai' })).toBe('')
})

// Saving is an upsert that keeps the stored key: a new connection under a
// name in use would silently replace that connection (and send its key to
// the new endpoint). Only editing it changes it.
test('a new connection cannot take the name of one already defined', () => {
  const ollama: ConnectionFormState = { ...blankConnectionForm('ollama'), name: 'claude' }
  expect(connectionFormError(ollama, null, ['claude', 'openai'])).toBe(t().nameTaken)
  expect(connectionFormError({ ...ollama, name: ' claude ' }, null, ['claude'])).toBe(t().nameTaken)
  expect(connectionFormError({ ...ollama, name: 'local' }, null, ['claude'])).toBe('')
  // Names are compared as the server stores them: case matters.
  expect(connectionFormError({ ...ollama, name: 'Claude' }, null, ['claude'])).toBe('')
  // Editing a connection keeps its own name.
  expect(connectionFormError(ollama, { hasApiKey: true }, ['claude'])).toBe('')
  expect(es.llm.connections.form.nameTaken).toBeTruthy()
})

test('model suggestions merge the lists without repeats', () => {
  expect(modelSuggestions([{ id: 'a' }, { id: 'b', name: 'B' }], undefined, [{ id: 'b' }, { id: 'c' }]))
    .toEqual([{ id: 'a' }, { id: 'b', name: 'B' }, { id: 'c' }])
  expect(modelSuggestions()).toEqual([])
})

test('a connection is shown by its preset, else by its API', () => {
  expect(connectionKind({ provider: 'anthropic', baseUrl: '' })).toBe('Anthropic')
  expect(connectionKind({ provider: 'openai', baseUrl: 'http://localhost:11434/v1' })).toBe('Ollama')
  expect(connectionKind({ provider: 'openai', baseUrl: 'http://10.0.0.5:8000/v1' })).toBe(en.editor.llm.providers.openai)
})

test('today’s usage says the tokens, the requests and how close the limit is', () => {
  expect(usageSummary(stored({ usageToday: { inputTokens: 1000, outputTokens: 234, requests: 1 } })))
    .toEqual({ text: 'Today: 1,234 tokens in 1 request', level: 'unlimited', percent: 0, notice: '' })
  expect(usageSummary(stored({ dailyTokenLimit: 100000, usageToday: { inputTokens: 40000, outputTokens: 10000, requests: 8 } })))
    .toEqual({ text: 'Today: 50,000 of 100,000 tokens in 8 requests', level: 'ok', percent: 50, notice: '' })
  const near = usageSummary(stored({ dailyTokenLimit: 1000, usageToday: { inputTokens: 800, outputTokens: 50, requests: 3 } }))
  expect(near).toMatchObject({ level: 'high', percent: 85, notice: en.llm.connections.nearLimit })
  const spent = usageSummary(stored({ dailyTokenLimit: 1000, usageToday: { inputTokens: 900, outputTokens: 300, requests: 3 } }))
  expect(spent).toMatchObject({ level: 'exhausted', percent: 100, notice: en.llm.connections.limitReached })
})

test('a key made only of secret references can be shown; a literal key stays hidden', () => {
  expect(isSecretReference('{{secret.anthropic}}')).toBe(true)
  expect(isSecretReference(' {{ secret.openai_key }} ')).toBe(true)
  expect(isSecretReference('sk-ant-123')).toBe(false)
  expect(isSecretReference('sk-{{secret.part}}')).toBe(false)
  expect(isSecretReference('')).toBe(false)
})

// --- The memory dialog ---------------------------------------------------------

test('a page of documents says where it is and where it can go', () => {
  expect(pageInfo(0, 50, 120)).toEqual({ from: 1, to: 50, total: 120, hasPrev: false, hasNext: true })
  expect(pageInfo(100, 20, 120)).toEqual({ from: 101, to: 120, total: 120, hasPrev: true, hasNext: false })
  expect(pageInfo(0, 0, 0)).toEqual({ from: 0, to: 0, total: 0, hasPrev: false, hasNext: false })
})

test('deleting the last document of a page goes back a page', () => {
  expect(offsetAfterDelete(100, 101)).toBe(100)
  expect(offsetAfterDelete(100, 100)).toBe(100 - DOCUMENT_PAGE)
  expect(offsetAfterDelete(0, 0)).toBe(0)
})

test('conversations filter by key and show a bounded list', () => {
  const list = ['telegram:1', 'telegram:2', 'mail:ana'].map(key => ({ key, turns: 2, updatedAt: '2026-11-15T10:00:00Z' }))
  expect(filterConversations(list, 'TELE').items.map(c => c.key)).toEqual(['telegram:1', 'telegram:2'])
  expect(filterConversations(list, '  ')).toEqual({ items: list, more: 0 })
  expect(filterConversations(list, '', 2)).toEqual({ items: list.slice(0, 2), more: 1 })
})

test('documents preview on one line, and their metadata as JSON', () => {
  expect(textPreview('First line\n\n  second   line')).toBe('First line second line')
  expect(textPreview('x'.repeat(10), 4)).toBe('xxxx…')
  expect(metadataText(undefined)).toBe('')
  expect(metadataText({})).toBe('')
  expect(metadataText({ source: 'rss', page: 2 })).toBe('{"source":"rss","page":2}')
})

// --- Shell and catalogs --------------------------------------------------------

test('the shell opens the LLM dialogs', () => {
  setActivePinia(createPinia())
  const shell = useShellStore()
  expect(shell.showLLMConnections).toBe(false)
  expect(shell.showAIMemory).toBe(false)
})

test('the dialogs keep the names the editor’s hints use', () => {
  expect(en.llm.connections.title).toBe('LLM connections')
  expect(es.llm.connections.title).toBe('Conexiones LLM')
  expect(en.llm.nav.connections).toBe(en.llm.connections.title)
  expect(es.llm.nav.connections).toBe(es.llm.connections.title)
})

// Leaving a conversation or a collection, or clearing or deleting one,
// removes the focused button: the focus goes to its row in the list, the
// row that took its place, or (none left) the list's own controls.
test('the focus returns to the row that was shown, or to its neighbour', () => {
  const keys = ['a', 'b', 'c']
  expect(focusRowAfter('b', keys, keys)).toBe('b')
  // Gone: the next row, else the previous one.
  expect(focusRowAfter('b', keys, ['a', 'c'])).toBe('c')
  expect(focusRowAfter('c', keys, ['a', 'b'])).toBe('b')
  expect(focusRowAfter('a', ['a'], [])).toBeNull()
  // Not listed (filtered out): the list's own controls.
  expect(focusRowAfter('x', keys, keys)).toBeNull()
  expect(focusRowAfter(null, keys, keys)).toBeNull()
})

test('a row\'s control has an element ID whatever its key holds', () => {
  const id = rowControlId('ai-memory-open', 'chat:a b/../c\t"x"')
  expect(id).not.toMatch(/\s/)
  expect(id.startsWith('ai-memory-open-')).toBe(true)
  expect(rowControlId('ai-memory-open', 'a b')).not.toBe(rowControlId('ai-memory-open', 'a%20b'))
  expect(rowControlId('ai-memory-open', 'k')).toBe(rowControlId('ai-memory-open', 'k'))
})

// The panels try these element IDs in order: the row's control, then the
// list's own controls, so the focus never falls back to the page.
test('the focus goes to a row\'s control, else to the list\'s own controls', () => {
  const keys = ['a', 'b']
  expect(focusCandidates('b', keys, ['a'], 'ai-memory-open', ['ai-memory-filter', 'ai-memory-refresh']))
    .toEqual([rowControlId('ai-memory-open', 'a'), 'ai-memory-filter', 'ai-memory-refresh'])
  expect(focusCandidates('a', ['a'], [], 'ai-memory-open', ['ai-memory-refresh'])).toEqual(['ai-memory-refresh'])
})

// Telegram updates reach a flow through the Webhook trigger, which puts the
// request body under payload.body: a key without it renders the same for
// every chat ("telegram:"), and all chats would share one conversation.
test('the example memory key reads the chat from the webhook body', () => {
  expect(EXAMPLE_MEMORY_KEY).toBe('telegram:{{payload.body.message.chat.id}}')
  expect(en.llm.memory.conversations.empty.join('')).not.toContain('payload.message')
  expect(es.llm.memory.conversations.empty.join('')).not.toContain('payload.message')
})
