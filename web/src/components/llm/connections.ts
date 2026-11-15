// The LLM connections dialog's form and list (docs/design/llm.md §8): the
// preset a form starts from, what is wrong before anything is sent (the
// server checks again: llm.Connection.Validate), the models to suggest and
// today's usage against the limit. Endpoints, presets and the request body
// are lib/llm.ts's.
import { formatNumber, msg } from '@/i18n'
import { LLM_PRESETS, connectionKind, presetFor, validLLMName, type LLMConnectionForm, type LLMPresetId } from '@/lib/llm'
import type { LLMConnection, LLMModel } from '@/types'

/** A named preset, or another server ('custom': the provider and address are typed). */
export type PresetChoice = LLMPresetId | 'custom'

export interface ConnectionFormState extends LLMConnectionForm {
  preset: PresetChoice
}

/** A new connection: the preset's provider and endpoint, nothing typed. */
export function blankConnectionForm(preset: LLMPresetId = 'anthropic'): ConnectionFormState {
  return applyPreset({ preset, name: '', provider: 'anthropic', baseUrl: '', apiKey: '', clearApiKey: false, model: '', dailyTokenLimit: undefined }, preset)
}

/** Switches the preset: a named one sets the provider and endpoint; another server keeps them to be edited. */
export function applyPreset(form: ConnectionFormState, preset: PresetChoice): ConnectionFormState {
  const p = LLM_PRESETS.find(x => x.id === preset)
  return p ? { ...form, preset, provider: p.provider, baseUrl: p.baseUrl } : { ...form, preset: 'custom' }
}

/** Editing a stored connection: its settings, never its key (write-only: empty keeps it). */
export function connectionFormFrom(c: LLMConnection): ConnectionFormState {
  return {
    preset: presetFor(c.provider, c.baseUrl),
    name: c.name,
    provider: c.provider,
    baseUrl: c.baseUrl,
    apiKey: '',
    clearApiKey: false,
    model: c.model,
    dailyTokenLimit: c.dailyTokenLimit > 0 ? c.dailyTokenLimit : undefined
  }
}

/** What is wrong with a base URL ('' when nothing or empty), as the server checks it. */
export function baseUrlError(raw: string): string {
  const t = msg().llm.connections.form
  const value = raw.trim().replace(/\/+$/, '')
  if (!value) return ''
  let url: URL
  try { url = new URL(value) } catch { return t.baseUrlFormat }
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || !url.hostname) return t.baseUrlFormat
  if (url.username || url.password) return t.baseUrlCredentials
  if (url.search || url.hash || /[?#]/.test(value)) return t.baseUrlQuery
  return ''
}

/**
 * What is wrong with the form before it is saved or tested ('' when
 * nothing). stored: the connection being edited (its key is kept unless
 * cleared). taken: the names already defined, which a new connection may
 * not use: saving is an upsert that keeps the stored key, so it would
 * silently replace that connection and send its key to the new endpoint.
 */
export function connectionFormError(form: ConnectionFormState, stored?: Pick<LLMConnection, 'hasApiKey'> | null, taken: readonly string[] = []): string {
  const t = msg().llm.connections.form
  const name = form.name.trim()
  if (!name) return t.nameRequired
  if (!validLLMName(name)) return t.nameChars
  if (!stored && taken.includes(name)) return t.nameTaken
  // Another server with no address would silently be OpenAI's.
  if (form.preset === 'custom' && !form.baseUrl.trim()) return t.baseUrlRequired
  const urlError = baseUrlError(form.baseUrl)
  if (urlError) return urlError
  const keepsKey = !!stored?.hasApiKey && !form.clearApiKey
  if (form.provider === 'anthropic' && !form.apiKey && !keepsKey) return t.keyRequired
  const limit = form.dailyTokenLimit
  if (limit !== undefined && (!Number.isInteger(limit) || limit < 0)) return t.limitInvalid
  return ''
}

/** Models from several listings (a test, the stored connection's), first listing first, without repeats. */
export function modelSuggestions(...lists: (readonly LLMModel[] | undefined)[]): LLMModel[] {
  const seen = new Set<string>()
  const merged: LLMModel[] = []
  for (const model of lists.flatMap(list => list ?? [])) {
    if (!model.id || seen.has(model.id)) continue
    seen.add(model.id)
    merged.push(model)
  }
  return merged
}

/** How a listed connection is named (lib/llm.ts). */
export { connectionKind }

export type UsageLevel = 'unlimited' | 'ok' | 'high' | 'exhausted'

export interface UsageSummary {
  text: string
  level: UsageLevel
  /** Share of the daily limit used, 0-100 (0 without a limit). */
  percent: number
  /** Close to or at the limit, in words ('' otherwise): never shown by color alone. */
  notice: string
}

/** From this share of the limit on, the list warns (the limit is the connection's budget: llm.Store.CheckBudget). */
const NEAR_LIMIT = 0.8

/** Today's tokens and requests, against the daily limit when there is one. */
export function usageSummary(c: Pick<LLMConnection, 'dailyTokenLimit' | 'usageToday'>): UsageSummary {
  const t = msg().llm.connections
  const tokens = (c.usageToday?.inputTokens ?? 0) + (c.usageToday?.outputTokens ?? 0)
  const requests = c.usageToday?.requests ?? 0
  const limit = c.dailyTokenLimit
  if (!(limit > 0)) return { text: t.usage(tokens, formatNumber(tokens), requests, formatNumber(requests)), level: 'unlimited', percent: 0, notice: '' }
  const text = t.usageLimit(formatNumber(tokens), formatNumber(limit), requests, formatNumber(requests))
  const percent = Math.min(100, Math.round(tokens / limit * 100))
  if (tokens >= limit) return { text, level: 'exhausted', percent, notice: t.limitReached }
  if (tokens >= limit * NEAR_LIMIT) return { text, level: 'high', percent, notice: t.nearLimit }
  return { text, level: 'ok', percent, notice: '' }
}

const SECRET_REFERENCES = /^(?:\{\{\s*secret\.[^{}\s]+\s*\}\}\s*)+$/

/** The key is only {{secret.NAME}} references: nothing secret to hide while typing it. */
export function isSecretReference(value: string): boolean {
  return SECRET_REFERENCES.test(value.trim())
}
