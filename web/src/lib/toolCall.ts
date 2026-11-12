// Helpers of the editor's "Test tool" (the REST twin of an MCP tool call):
// arguments prefilled from the tool's input schema, and the call's outcome
// rendered as plain text. Nothing here produces HTML: results come from the
// flow and are shown as text only.
import type { CallResult } from '@/types'
import { isPlainObject, parseJsonObject } from './configValues'
import { msg } from '@/i18n'

// The schema setting may be stored parsed or as text (older definitions).
function schemaObject(schema: unknown): Record<string, unknown> | undefined {
  if (typeof schema === 'string') {
    const parsed = parseJsonObject(schema)
    return parsed.value
  }
  return isPlainObject(schema) ? schema : undefined
}

// The arguments the schema's defaults describe: every property with a
// default, and nested objects that end up with at least one.
export function defaultArgs(schema: unknown): Record<string, unknown> {
  const root = schemaObject(schema)
  const out: Record<string, unknown> = {}
  const props = root?.properties
  if (!isPlainObject(props)) return out
  for (const [name, prop] of Object.entries(props)) {
    if (!isPlainObject(prop)) continue
    if ('default' in prop) {
      out[name] = prop.default
    } else if (prop.type === 'object') {
      const nested = defaultArgs(prop)
      if (Object.keys(nested).length) out[name] = nested
    }
  }
  return out
}

// The text the arguments box starts with.
export function argsTextFromSchema(schema: unknown): string {
  return JSON.stringify(defaultArgs(schema), null, 2)
}

// Parses the arguments box: one JSON object; blank means no arguments.
export function parseToolArgs(text: string): { value?: Record<string, unknown>; error?: string } {
  if (text.trim() === '') return { value: {} }
  const parsed = parseJsonObject(text)
  return parsed.error ? { error: parsed.error } : { value: parsed.value ?? {} }
}

// Read when a result is shown, in the interface language.
function statusNote(status: CallResult['status']): string | undefined {
  const t = msg().agents.call
  const notes: Partial<Record<CallResult['status'], string>> = { completed_no_reply: t.noReply, running: t.stillRunning, expired: t.expired }
  return notes[status]
}

// A call's outcome as plain text.
export function callResultText(res: CallResult): string {
  const t = msg().agents.call
  const lines = [t.status(res.status), t.run(res.runId), t.duration(res.durationMs)]
  const note = statusNote(res.status)
  if (note) lines.push(note)
  if (res.error) {
    const where = res.error.nodeId ? t.errorIn(res.error.nodeId, res.error.nodeType ?? '') : ''
    lines.push(t.error(where, res.error.message))
  }
  if (res.result !== undefined) {
    lines.push(t.result, JSON.stringify(res.result, null, 2))
  }
  return lines.join('\n')
}

interface Issue { pointer: string; message: string }

// Why a call was refused (invalid arguments, busy, cooldown, …), from the
// API's error envelope, as plain text.
export function callErrorText(err: unknown): string {
  const body = (err as { response?: { data?: unknown } })?.response?.data
  const t = msg().agents.call
  if (isPlainObject(body) && typeof body.error === 'string') {
    const lines = [body.error]
    const data = isPlainObject(body.data) ? body.data : {}
    if (Array.isArray(data.issues)) {
      for (const issue of data.issues as Issue[]) lines.push(`${issue.pointer || t.root}: ${issue.message}`)
    }
    if (typeof data.retryAfterMs === 'number' && data.retryAfterMs > 0) {
      lines.push(t.retryIn(Math.ceil(data.retryAfterMs / 1000)))
    }
    return lines.join('\n')
  }
  if (err instanceof Error) return t.failedWith(err.message)
  return t.failed
}
