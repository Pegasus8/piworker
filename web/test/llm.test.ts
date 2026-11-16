import { expect, test } from 'bun:test'
import {
  DEFAULT_ANTHROPIC_URL,
  DEFAULT_OPENAI_URL,
  LLM_PRESETS,
  conflictFlows,
  llmConnectionRequest,
  llmEndpoint,
  presetFor,
  validLLMName,
  type LLMConnectionForm
} from '../src/lib/llm'
import { apiErrorMessage } from '../src/lib/apiError'

const form: LLMConnectionForm = { name: 'ollama', provider: 'openai', baseUrl: ' http://localhost:11434/v1/ ', apiKey: '', clearApiKey: false, model: ' llama3.2 ', dailyTokenLimit: undefined }

test('the endpoint is the base URL or the provider default', () => {
  expect(llmEndpoint({ provider: 'anthropic', baseUrl: '' })).toBe(DEFAULT_ANTHROPIC_URL)
  expect(llmEndpoint({ provider: 'openai', baseUrl: '' })).toBe(DEFAULT_OPENAI_URL)
  expect(llmEndpoint({ provider: 'openai', baseUrl: 'http://pi.local:1234/v1' })).toBe('http://pi.local:1234/v1')
})

// The API key is write-only: an empty field keeps the stored one, unless
// clearing it was asked for.
test('the request keeps, replaces or clears the stored key', () => {
  expect(llmConnectionRequest(form)).toEqual({ provider: 'openai', baseUrl: 'http://localhost:11434/v1', model: 'llama3.2', dailyTokenLimit: 0 })
  expect(llmConnectionRequest({ ...form, apiKey: '{{secret.openai}}' }).apiKey).toBe('{{secret.openai}}')
  expect(llmConnectionRequest({ ...form, clearApiKey: true }).apiKey).toBe('')
  expect(llmConnectionRequest({ ...form, dailyTokenLimit: 50000.7 }).dailyTokenLimit).toBe(50000)
  expect(llmConnectionRequest({ ...form, dailyTokenLimit: -5 }).dailyTokenLimit).toBe(0)
})

test('connection names are 1-64 letters, digits, _ and -', () => {
  expect(validLLMName('claude_work-2')).toBe(true)
  expect(validLLMName('')).toBe(false)
  expect(validLLMName('a b')).toBe(false)
  expect(validLLMName('x'.repeat(65))).toBe(false)
})

test('presets fill provider and endpoint, and are recognised back', () => {
  const ollama = LLM_PRESETS.find(p => p.id === 'ollama')!
  expect(ollama).toMatchObject({ provider: 'openai', baseUrl: 'http://localhost:11434/v1', needsKey: false })
  expect(LLM_PRESETS.find(p => p.id === 'anthropic')).toMatchObject({ provider: 'anthropic', baseUrl: '', needsKey: true })
  expect(presetFor('openai', 'http://localhost:11434/v1')).toBe('ollama')
  expect(presetFor('openai', '')).toBe('openai')
  expect(presetFor('anthropic', '')).toBe('anthropic')
  expect(presetFor('openai', 'http://10.0.0.5:8000/v1')).toBe('custom')
})

test('a delete refused for running flows names them', () => {
  const refusal = { response: { status: 409, data: { success: false, error: 'used', data: { flows: [{ id: 'f1', name: 'Inbox' }, { id: 'f2' }, 'bad'] } } } }
  expect(conflictFlows(refusal)).toEqual([{ id: 'f1', name: 'Inbox' }, { id: 'f2', name: 'f2' }])
  expect(conflictFlows({ response: { status: 500 } })).toEqual([])
  expect(conflictFlows(new Error('network'))).toEqual([])
})

test('errors read as the server message, or the fallback', () => {
  expect(apiErrorMessage({ response: { data: { success: false, error: 'invalid x-api-key' } } }, 'failed')).toBe('invalid x-api-key')
  expect(apiErrorMessage({ response: { data: { error: '' } } }, 'failed')).toBe('failed')
  expect(apiErrorMessage(new Error('Network Error'), 'failed')).toBe('failed')
})
