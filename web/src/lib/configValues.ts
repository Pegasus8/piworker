import type { ConfigField } from '@/types'

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

// Converts a backend JSON config schema ({ properties, required }) into the
// fields the config panel renders.
export function configFieldsFromSchema(schema: any): ConfigField[] {
  const required: string[] = schema?.required || []
  const props = schema?.properties || {}
  return Object.keys(props).map((key): ConfigField => {
    const p = props[key]
    const hasEnum = Array.isArray(p.enum) && p.enum.length > 0
    let type: ConfigField['type'] = 'text'
    if (p.type === 'number' || p.type === 'integer') type = 'number'
    else if (p.type === 'boolean') type = 'boolean'
    else if (p.type === 'object') type = 'keyvalue'
    else if (hasEnum) type = 'select'
    return {
      key,
      label: p.title || key,
      type,
      required: required.includes(key),
      default: p.default,
      placeholder: p.description || '',
      options: hasEnum ? p.enum.map((v: string) => ({ label: v, value: v })) : undefined
    }
  })
}
