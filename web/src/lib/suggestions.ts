// Where node settings find suggestions (ConfigProperty.Options; ADR-0026,
// docs/design/llm.md §5): broker connections and Zigbee devices and
// properties (lib/zigbee.ts), LLM connections and their models, published
// tools and knowledge collections (stores/llm.ts). Suggestions never
// restrict what may be typed. Pure helpers; the requests live in the stores.
import { msg } from '@/i18n'
import * as zigbee from '@/lib/zigbee'
import type { AgentTool } from '@/lib/agentAccess'
import type { KnowledgeCollection, LLMConnection, LLMModel } from '@/types'
import { connectionKind, connectionLabel, llmEndpoint } from '@/lib/llm'

const LLM_SOURCES = ['llm-connections', 'llm-models', 'tools', 'knowledge-collections'] as const
export type LLMSuggestSource = typeof LLM_SOURCES[number]
export type SuggestSource = zigbee.SuggestSource | LLMSuggestSource

export function suggestSource(value: unknown): SuggestSource | undefined {
  return zigbee.suggestSource(value) ?? (LLM_SOURCES.includes(value as LLMSuggestSource) ? value as LLMSuggestSource : undefined)
}

function isZigbeeSource(source: SuggestSource): source is zigbee.SuggestSource {
  return zigbee.suggestSource(source) !== undefined
}

/** One suggestion: the value typed in, and what the list shows beside it. */
export interface Suggestion { value: string; label?: string }

/** A model as a suggestion: its ID, with its name when that says more. */
export function modelSuggestion(m: LLMModel): Suggestion {
  return { value: m.id, ...(m.name && m.name !== m.id ? { label: m.name } : {}) }
}

export interface SuggestionContext extends zigbee.SuggestionContext {
  llmConnections: LLMConnection[]
  /** The models of each LLM connection, as loaded (absent: not loaded). */
  models: Record<string, LLMModel[]>
  /** Connections whose models could not be listed, with why. */
  modelErrors: Record<string, string>
  /** The published tools (GET /api/agent/tools). */
  tools: AgentTool[]
  collections: KnowledgeCollection[]
  /** Which lists did load: a hint never claims a name is unknown from a list that failed. */
  loaded: { llmConnections: boolean; tools: boolean; collections: boolean }
  /** The flow being edited ('' when new): an agent node cannot call its own flow's tools. */
  flowId: string
}

/** What a form whose fields have these sources needs loaded. */
export interface SuggestionNeeds {
  connections: boolean
  networks: boolean
  llmConnections: boolean
  /** The models of the node's LLM connection. */
  models: boolean
  tools: boolean
  collections: boolean
}

export function suggestionNeeds(sources: readonly (SuggestSource | undefined)[]): SuggestionNeeds {
  const has = (s: SuggestSource) => sources.includes(s)
  return {
    connections: has('connections'),
    networks: has('zigbee-devices') || has('zigbee-properties') || has('zigbee-settable'),
    llmConnections: has('llm-connections') || has('llm-models'),
    models: has('llm-models'),
    tools: has('tools'),
    collections: has('knowledge-collections')
  }
}

const text = (v: unknown) => typeof v === 'string' ? v : ''

/** Whether an agent node may call the tool at all: not its own flow's, and no confirmation to wait for. */
function callable(tool: AgentTool, flowId: string): boolean {
  return (!flowId || tool.flowId !== flowId) && tool.confirm === 'none'
}

function unique(list: Suggestion[]): Suggestion[] {
  const seen = new Set<string>()
  return list.filter(s => s.value && !seen.has(s.value) && seen.add(s.value))
}

/** The suggestions of a setting with this source, for the node being edited. */
export function suggestions(source: SuggestSource, ctx: SuggestionContext): Suggestion[] {
  if (isZigbeeSource(source)) return zigbee.suggestions(source, ctx).map(value => ({ value }))
  const t = msg().editor.llm
  switch (source) {
    case 'llm-connections':
      return unique(ctx.llmConnections.map(c => ({ value: c.name, label: connectionLabel(c) })))
    case 'llm-models':
      return unique((ctx.models[text(ctx.config.connection)] ?? []).map(modelSuggestion))
    case 'tools':
      return unique(ctx.tools.filter(tool => callable(tool, ctx.flowId)).map(tool => ({ value: tool.toolName, label: tool.title ? `${tool.title} (${tool.flowName})` : tool.flowName })))
    case 'knowledge-collections':
      return unique(ctx.collections.map(c => ({ value: c.name, label: t.documents(c.documents) })))
  }
}

/** What the editor says under a setting: information, or a warning (shown as one). */
export interface SuggestionHint { text: string; warning: boolean }
const info = (text: string): SuggestionHint => ({ text, warning: false })
const warn = (text: string): SuggestionHint => ({ text, warning: !!text })

/**
 * What is known of the value a setting has (text '' when nothing): the
 * Zigbee device or property, the LLM connection's endpoint, the model a
 * node without one uses, the tools an agent node lists but cannot call,
 * the collection's embedding model.
 */
export function suggestionHint(source: SuggestSource, ctx: SuggestionContext, value: unknown): SuggestionHint {
  if (isZigbeeSource(source)) return info(zigbee.fieldHint(source, ctx))
  const t = msg().editor.llm
  switch (source) {
    case 'llm-connections': {
      const name = text(value).trim()
      if (!name) return info('')
      const c = ctx.llmConnections.find(c => c.name === name)
      if (c) return info(t.connectionHint(connectionKind(c), llmEndpoint(c), c.model))
      return warn(ctx.loaded.llmConnections ? t.undefinedConnection(name) : '')
    }
    case 'llm-models': {
      const name = text(ctx.config.connection).trim()
      const c = ctx.llmConnections.find(c => c.name === name)
      if (!c) return info('')
      const model = text(value).trim()
      if (!model) return c.model ? info(t.connectionModel(c.model)) : warn(t.noModel)
      if (ctx.modelErrors[name]) return warn(t.modelsUnavailable)
      const models = ctx.models[name]
      return warn(models?.length && !model.includes('{{') && !models.some(m => m.id === model) ? t.unknownModel : '')
    }
    case 'tools': {
      if (!ctx.loaded.tools || !Array.isArray(value)) return info('')
      const names = [...new Set(value.filter((v): v is string => typeof v === 'string' && v.trim() !== '').map(v => v.trim()))]
      const missing: string[] = [], own: string[] = [], confirm: string[] = [], privileged: string[] = []
      for (const name of names) {
        const published = ctx.tools.filter(tool => tool.toolName === name)
        const tool = published.find(tool => callable(tool, ctx.flowId))
        if (tool) {
          if (tool.risk === 'privileged' && ctx.config.allowPrivileged !== true) privileged.push(name)
        } else if (!published.length) missing.push(name)
        else if (published.some(tool => ctx.flowId && tool.flowId === ctx.flowId)) own.push(name)
        else confirm.push(name)
      }
      return warn([
        missing.length ? t.toolsNotPublished(missing) : '',
        own.length ? t.toolsOwnFlow(own) : '',
        confirm.length ? t.toolsNeedConfirmation(confirm) : '',
        privileged.length ? t.toolsPrivileged(privileged) : ''
      ].filter(Boolean).join(' '))
    }
    case 'knowledge-collections': {
      const name = text(value).trim()
      if (!name) return info('')
      const c = ctx.collections.find(c => c.name === name)
      if (c) return info(t.collectionHint(c.documents, c.model))
      return info(ctx.loaded.collections && !name.includes('{{') ? t.newCollection : '')
    }
  }
}
