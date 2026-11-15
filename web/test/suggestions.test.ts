import { beforeAll, expect, test } from 'bun:test'
import { setLocale } from '../src/i18n'
import { suggestSource, suggestionHint as fullHint, suggestionNeeds, suggestions, type SuggestionContext } from '../src/lib/suggestions'
import type { AgentTool } from '../src/lib/agentAccess'
import type { LLMConnection } from '../src/types'

beforeAll(() => setLocale('en'))

const usage = { inputTokens: 0, outputTokens: 0, requests: 0 }
const claude: LLMConnection = { name: 'claude', provider: 'anthropic', baseUrl: '', hasApiKey: true, model: 'claude-opus-4-5', dailyTokenLimit: 0, usageToday: usage }
const ollama: LLMConnection = { name: 'ollama', provider: 'openai', baseUrl: 'http://localhost:11434/v1', hasApiKey: false, model: '', dailyTokenLimit: 0, usageToday: usage }
const tool = (toolName: string, extra: Partial<AgentTool> = {}): AgentTool => ({
  flowId: 'other', flowName: 'Home', triggerNodeId: 't', toolName, title: '', description: '', risk: 'safe',
  annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }, fingerprint: '', confirm: 'none', inputSchema: {},
  ...extra
})

function ctx(config: Record<string, unknown> = {}, extra: Partial<SuggestionContext> = {}): SuggestionContext {
  return {
    connections: [{ name: 'broker', broker: 'tcp://h:1883', username: '', hasPassword: false, baseTopic: 'zigbee2mqtt' }],
    networks: [],
    llmConnections: [claude, ollama],
    models: { claude: [{ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' }, { id: 'claude-haiku-4-5' }], ollama: [{ id: 'llama3.2', name: 'llama3.2' }] },
    modelErrors: {},
    tools: [tool('lights_on', { title: 'Lights on' }), tool('own_tool', { flowId: 'this' }), tool('door_open', { confirm: 'admin' }), tool('reboot', { risk: 'privileged' })],
    collections: [{ name: 'manuals', documents: 12, model: 'nomic-embed-text', dimensions: 768, updatedAt: '2026-11-15T10:00:00Z' }],
    loaded: { llmConnections: true, tools: true, collections: true },
    flowId: 'this',
    config,
    ...extra
  }
}
const values = (list: { value: string }[]) => list.map(s => s.value)
const suggestionHint = (...args: Parameters<typeof fullHint>) => fullHint(...args).text

test('the LLM sources are known besides the broker and Zigbee ones', () => {
  for (const source of ['llm-connections', 'llm-models', 'tools', 'knowledge-collections', 'connections', 'zigbee-devices', 'zigbee-properties', 'zigbee-settable']) {
    expect(suggestSource(source)).toBe(source as any)
  }
  expect(suggestSource('models')).toBeUndefined()
  expect(suggestSource(undefined)).toBeUndefined()
})

test('broker connections keep their suggestions', () => {
  expect(values(suggestions('connections', ctx()))).toEqual(['broker'])
  expect(suggestionHint('connections', ctx(), 'broker')).toBe('')
})

test('LLM connections are suggested with their provider and model', () => {
  const list = suggestions('llm-connections', ctx())
  expect(values(list)).toEqual(['claude', 'ollama'])
  expect(list[0].label).toContain('Anthropic')
  expect(list[0].label).toContain('claude-opus-4-5')
})

test('models are those of the node connection', () => {
  expect(values(suggestions('llm-models', ctx({ connection: 'claude' })))).toEqual(['claude-opus-4-5', 'claude-haiku-4-5'])
  expect(suggestions('llm-models', ctx({ connection: 'claude' }))[0].label).toBe('Claude Opus 4.5')
  // A display name equal to the ID adds nothing.
  expect(suggestions('llm-models', ctx({ connection: 'ollama' }))[0].label).toBeUndefined()
  // No connection, or one whose models are not loaded: none.
  expect(suggestions('llm-models', ctx())).toEqual([])
  expect(suggestions('llm-models', ctx({ connection: 'gpt' }))).toEqual([])
})

// Agent nodes cannot call their own flow's tools, nor tools that wait for a
// confirmation: they are not offered.
test('tools are the published ones an agent node may call', () => {
  const list = suggestions('tools', ctx())
  expect(values(list)).toEqual(['lights_on', 'reboot'])
  expect(list[0].label).toBe('Lights on (Home)')
})

test('knowledge collections are suggested with their size', () => {
  const list = suggestions('knowledge-collections', ctx())
  expect(values(list)).toEqual(['manuals'])
  expect(list[0].label).toContain('12')
})

test('the hint of an LLM connection says what it is, or that it is not defined', () => {
  expect(suggestionHint('llm-connections', ctx(), 'claude')).toContain('https://api.anthropic.com')
  expect(suggestionHint('llm-connections', ctx(), 'ollama')).toContain('http://localhost:11434/v1')
  expect(suggestionHint('llm-connections', ctx(), 'gpt')).toContain('gpt')
  // Not loaded (or failed): nothing is claimed.
  expect(suggestionHint('llm-connections', ctx({}, { loaded: { llmConnections: false, tools: true, collections: true } }), 'gpt')).toBe('')
  expect(suggestionHint('llm-connections', ctx(), '')).toBe('')
})

test('the hint of a model names the connection default and flags unknown ones', () => {
  expect(suggestionHint('llm-models', ctx({ connection: 'claude' }), '')).toContain('claude-opus-4-5')
  expect(suggestionHint('llm-models', ctx({ connection: 'ollama' }), '')).not.toBe('')
  expect(suggestionHint('llm-models', ctx({ connection: 'claude' }), 'claude-haiku-4-5')).toBe('')
  expect(suggestionHint('llm-models', ctx({ connection: 'claude' }), 'claude-typo')).not.toBe('')
  expect(suggestionHint('llm-models', ctx({ connection: 'claude' }, { models: {}, modelErrors: { claude: 'unreachable' } }), 'x')).not.toBe('')
  expect(suggestionHint('llm-models', ctx(), '')).toBe('')
})

test('the hint of a tools list names the ones the agent cannot call', () => {
  const hint = suggestionHint('tools', ctx(), ['lights_on', 'gone', 'own_tool', 'door_open', 'reboot'])
  expect(hint).toContain('gone')
  expect(hint).toContain('own_tool')
  expect(hint).toContain('door_open')
  expect(hint).toContain('reboot')
  expect(hint).not.toContain('lights_on')
  // With privileged tools allowed, reboot is fine.
  expect(suggestionHint('tools', ctx({ allowPrivileged: true }), ['reboot'])).toBe('')
  expect(suggestionHint('tools', ctx(), ['lights_on'])).toBe('')
  expect(suggestionHint('tools', ctx({}, { loaded: { llmConnections: true, tools: false, collections: true } }), ['gone'])).toBe('')
})

test('the hint of a collection describes it or says a new one is created', () => {
  expect(suggestionHint('knowledge-collections', ctx(), 'manuals')).toContain('nomic-embed-text')
  expect(suggestionHint('knowledge-collections', ctx(), 'recipes')).not.toBe('')
  expect(suggestionHint('knowledge-collections', ctx(), '')).toBe('')
})

test('a form loads only what its sources need', () => {
  expect(suggestionNeeds(['llm-connections', 'llm-models', undefined])).toEqual({ connections: false, networks: false, llmConnections: true, models: true, tools: false, collections: false })
  expect(suggestionNeeds(['tools'])).toMatchObject({ tools: true, llmConnections: false })
  expect(suggestionNeeds(['knowledge-collections'])).toMatchObject({ collections: true })
  expect(suggestionNeeds(['connections', 'zigbee-devices'])).toMatchObject({ connections: true, networks: true, llmConnections: false })
  expect(suggestionNeeds([])).toEqual({ connections: false, networks: false, llmConnections: false, models: false, tools: false, collections: false })
})

// Problems read as warnings; what is known of a choice reads as information.
test('hints that need action are warnings', () => {
  expect(fullHint('llm-connections', ctx(), 'gpt').warning).toBe(true)
  expect(fullHint('llm-connections', ctx(), 'claude').warning).toBe(false)
  expect(fullHint('llm-models', ctx({ connection: 'ollama' }), '').warning).toBe(true)
  expect(fullHint('llm-models', ctx({ connection: 'claude' }), '').warning).toBe(false)
  expect(fullHint('tools', ctx(), ['gone']).warning).toBe(true)
  expect(fullHint('tools', ctx(), ['lights_on'])).toEqual({ text: '', warning: false })
  expect(fullHint('knowledge-collections', ctx(), 'recipes').warning).toBe(false)
  expect(fullHint('zigbee-devices', ctx(), 'x')).toEqual({ text: '', warning: false })
})
