import type { ConfigField } from '@/types'
import { suggestSource } from '@/lib/suggestions'
import { msg } from '@/i18n'

// A name/value pair of an object-typed setting such as HTTP headers.
export interface ObjectRow { key: string; value: string }

// The number typed into a number input, or undefined for a cleared (or
// invalid) input: "unset", so the node uses its default. Never 0, which the
// server would check against the setting's minimum.
export function numberFromInput(raw: string): number | undefined {
  if (raw.trim() === '') return undefined
  const n = Number(raw)
  return Number.isFinite(n) ? n : undefined
}

// The integer typed into an integer input, or undefined (unset) for a cleared
// or invalid input. Fractions are cut toward zero, as the node would read them.
export function integerFromInput(raw: string): number | undefined {
  const n = numberFromInput(raw)
  return n === undefined ? undefined : Math.trunc(n) + 0
}

// The text to edit for a JSON-object setting: indented JSON, the saved text
// itself when it is a string (hand-written or older definitions), '' when unset.
export function jsonText(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

export interface ParsedJsonObject { value?: Record<string, unknown>; error?: string }

// Parses the text of a JSON-object setting. Blank text unsets the setting;
// anything but one JSON object is an error and must not be stored.
export function parseJsonObject(text: string): ParsedJsonObject {
  if (text.trim() === '') return { value: undefined }
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (e) {
    return { error: msg().flowTools.json.invalid(e instanceof Error ? e.message : String(e)) }
  }
  if (!isPlainObject(parsed)) return { error: msg().flowTools.json.notObject }
  return { value: parsed }
}

// Stores value under key, or removes the key when value is undefined.
export function setConfigValue(config: Record<string, unknown>, key: string, value: unknown): void {
  if (value === undefined) delete config[key]
  else config[key] = value
}

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

// The rows to edit for an object-typed setting. Anything but an object (for
// example free text saved by older editors) yields no rows.
export function objectRows(value: unknown): ObjectRow[] {
  if (!isPlainObject(value)) return []
  return Object.entries(value).map(([key, v]) => ({ key, value: typeof v === 'string' ? v : JSON.stringify(v) }))
}

// The object to store for edited rows. Rows without a name are left out; the
// result is undefined (unset) when no row has one.
export function objectFromRows(rows: ObjectRow[]): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) out[key] = row.value
  }
  return Object.keys(out).length ? out : undefined
}

// The rows to edit for a list setting (ConfigProperty type "array"): its
// items as text. Anything but a list (text saved by hand) yields no rows.
export function listRows(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map(item => typeof item === 'string' ? item : item === null || item === undefined ? '' : typeof item === 'object' ? JSON.stringify(item) : String(item))
}

// The list to store for edited rows: trimmed, blank rows left out (they are
// kept on screen while editing); undefined (unset) when none is left.
export function listFromRows(rows: string[]): string[] | undefined {
  const items = rows.map(row => row.trim()).filter(Boolean)
  return items.length ? items : undefined
}

// A list setting holding something else (free text written by hand or by an
// agent): the node ignores it and deploying refuses it.
export function unreadableList(value: unknown): boolean {
  return value !== undefined && value !== null && value !== '' && !Array.isArray(value)
}

// The items that appear more than once (trimmed), each named once: list
// settings that become ports or names must be unique.
export function listDuplicates(items: string[]): string[] {
  const seen = new Set<string>(), repeated = new Set<string>()
  for (const raw of items) {
    const item = raw.trim()
    if (!item) continue
    if (seen.has(item)) repeated.add(item)
    seen.add(item)
  }
  return [...repeated]
}

// A copy of list with the item at from moved to to (unchanged when either is out of range).
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
  const out = [...list]
  if (from < 0 || from >= out.length || to < 0 || to >= out.length) return out
  const [item] = out.splice(from, 1)
  out.splice(to, 0, item)
  return out
}

// Converts a backend JSON config schema ({ properties, required }) into the
// fields the config panel renders.
export function configFieldsFromSchema(schema: any): ConfigField[] {
  const required: string[] = schema?.required || []
  const props = schema?.properties || {}
  return Object.keys(props).map((key): ConfigField => {
    const p = props[key]
    const hasEnum = Array.isArray(p.enum) && p.enum.length > 0
    let type: ConfigField['type'] = 'text'
    if (p.type === 'number') type = 'number'
    else if (p.type === 'integer') type = 'integer'
    else if (p.type === 'boolean') type = 'boolean'
    else if (p.type === 'object') type = 'keyvalue'
    else if (p.type === 'json') type = 'json'
    else if (p.type === 'array') type = 'list'
    else if (hasEnum) type = 'select'
    return {
      key,
      label: p.title || key,
      type,
      required: required.includes(key),
      default: p.default,
      placeholder: p.description || '',
      unit: typeof p.unit === 'string' && p.unit ? p.unit : undefined,
      sensitive: p.sensitive === true || undefined,
      options: hasEnum ? p.enum.map((v: string) => ({ label: v, value: v })) : undefined,
      // A suggestion source of a free-text setting, or of each item of a
      // list (ConfigProperty.Options).
      suggest: type === 'text' || type === 'list' ? suggestSource(p.options) : undefined
    }
  })
}

export interface SelectView {
  options: { label: string; value: string }[]
  /** '' when no placeholder is shown. */
  placeholder: string
}

/**
 * A select field as the panel shows it. An empty enum value (the setting's
 * default, e.g. the model's own effort) is an option of its own labelled
 * labels.empty, and then there is no placeholder: a disabled placeholder
 * would stand for the same empty value and show the setting's description
 * in the closed select. Otherwise the placeholder is the description, or
 * labels.choose.
 */
export function selectView(field: Pick<ConfigField, 'options' | 'placeholder'>, labels: { empty: string; choose: string }): SelectView {
  const options = (field.options ?? []).map(o => (o.value === '' ? { label: labels.empty, value: '' } : o))
  const hasEmpty = options.some(o => o.value === '')
  return { options, placeholder: hasEmpty ? '' : field.placeholder || labels.choose }
}
