// LLM connections as the editor handles them (docs/design/llm.md §1.1, §8):
// endpoints, the provider presets of the connections form, the request
// body of a save or test and the flows a refused delete names. Pure
// helpers; the requests live in stores/llm.ts.
import { msg } from '@/i18n'
import type { LLMConnection, LLMConnectionRequest, LLMProvider } from '@/types'
import { flowRef, validConnectionName, type FlowRef } from '@/lib/zigbee'
import { apiStatus } from '@/lib/apiError'

/** The endpoints a connection without a base URL uses (llm.DefaultAnthropicURL, llm.DefaultOpenAIURL). */
export const DEFAULT_ANTHROPIC_URL = 'https://api.anthropic.com'
export const DEFAULT_OPENAI_URL = 'https://api.openai.com/v1'

export function llmEndpoint(c: Pick<LLMConnection, 'provider' | 'baseUrl'>): string {
  return c.baseUrl || (c.provider === 'anthropic' ? DEFAULT_ANTHROPIC_URL : DEFAULT_OPENAI_URL)
}

/** Connection (and knowledge collection) names: as broker connection names. */
export function validLLMName(name: string): boolean {
  return validConnectionName(name)
}

/** A base URL as it is checked and sent: trimmed, without trailing slashes. */
export function normalizeBaseUrl(raw: string): string {
  return raw.trim().replace(/\/+$/, '')
}

export type LLMPresetId = 'anthropic' | 'openai' | 'ollama' | 'lmstudio' | 'openrouter'

/** A starting point of the connections form: brand names, not translated. */
export interface LLMPreset {
  id: LLMPresetId
  label: string
  provider: LLMProvider
  /** '' is the provider's default endpoint. */
  baseUrl: string
  /** Local servers need no key. */
  needsKey: boolean
}

export const LLM_PRESETS: readonly LLMPreset[] = [
  { id: 'anthropic', label: 'Anthropic', provider: 'anthropic', baseUrl: '', needsKey: true },
  { id: 'openai', label: 'OpenAI', provider: 'openai', baseUrl: '', needsKey: true },
  { id: 'ollama', label: 'Ollama', provider: 'openai', baseUrl: 'http://localhost:11434/v1', needsKey: false },
  { id: 'lmstudio', label: 'LM Studio', provider: 'openai', baseUrl: 'http://localhost:1234/v1', needsKey: false },
  { id: 'openrouter', label: 'OpenRouter', provider: 'openai', baseUrl: 'https://openrouter.ai/api/v1', needsKey: true }
]

/** The preset a stored connection matches, or 'custom' (another server). */
export function presetFor(provider: string, baseUrl: string): LLMPresetId | 'custom' {
  return LLM_PRESETS.find(p => p.provider === provider && p.baseUrl === baseUrl)?.id ?? 'custom'
}

/** How a connection is named: its preset (OpenAI, Ollama, OpenRouter…), else its API. */
export function connectionKind(c: Pick<LLMConnection, 'provider' | 'baseUrl'>): string {
  const preset = LLM_PRESETS.find(p => p.id === presetFor(c.provider, c.baseUrl))
  return preset?.label ?? msg().editor.llm.providers[c.provider] ?? c.provider
}

/** A connection in a list: how it is named and its default model ("Ollama · llama3.2"). */
export function connectionLabel(c: Pick<LLMConnection, 'provider' | 'baseUrl' | 'model'>): string {
  return [connectionKind(c), c.model].filter(Boolean).join(' · ')
}

export interface LLMConnectionForm {
  name: string
  provider: LLMProvider
  baseUrl: string
  /** Empty keeps the stored key. May hold {{secret.NAME}} references. */
  apiKey: string
  /** Remove the stored key (when no new one is typed). */
  clearApiKey: boolean
  model: string
  /** Tokens per day; unset or 0 is no limit. */
  dailyTokenLimit: number | undefined
}

/** The body of PUT /api/llm/connections/{name} and of its test; the server checks it again. */
export function llmConnectionRequest(form: LLMConnectionForm): LLMConnectionRequest {
  const limit = form.dailyTokenLimit
  const body: LLMConnectionRequest = {
    provider: form.provider,
    baseUrl: normalizeBaseUrl(form.baseUrl),
    model: form.model.trim(),
    dailyTokenLimit: typeof limit === 'number' && Number.isFinite(limit) && limit > 0 ? Math.trunc(limit) : 0
  }
  if (form.apiKey) body.apiKey = form.apiKey
  else if (form.clearApiKey) body.apiKey = ''
  return body
}

/** The running flows a refused delete (409, data.flows) names, a flow without a name by its ID; none for any other error. */
export function conflictFlows(e: unknown): FlowRef[] {
  if (apiStatus(e) !== 409) return []
  const flows = (e as { response: { data?: { data?: { flows?: unknown } } } }).response.data?.data?.flows
  if (!Array.isArray(flows)) return []
  return flows.flatMap(f => {
    const ref = flowRef(f)
    return ref ? [{ id: ref.id, name: ref.name || ref.id }] : []
  })
}
