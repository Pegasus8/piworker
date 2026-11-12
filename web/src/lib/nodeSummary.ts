import type { ConfigField } from '@/types'
import { formatDuration, isDurationUnit } from '@/lib/duration'

// What a node on the canvas says about its settings: up to two values,
// required ones first, never a secret; and which required ones are empty.
const MAX = 32
const HIDDEN: ConfigField['type'][] = ['boolean', 'json', 'keyvalue']

function shorten(text: string) {
  const line = text.replace(/\s+/g, ' ').trim()
  return line.length > MAX ? `${line.slice(0, MAX - 1)}…` : line
}

function shown(field: ConfigField, value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value === 'number' && field.unit && isDurationUnit(field.unit)) return formatDuration(value, field.unit)
  if (typeof value === 'object') return ''
  return shorten(String(value))
}

export function nodeSummary(config: Record<string, unknown>, schema: ConfigField[]): { text: string; missing: string[] } {
  const missing: string[] = []
  const values: string[] = []
  const ordered = [...schema.filter(f => f.required), ...schema.filter(f => !f.required)]
  for (const field of ordered) {
    const value = config[field.key]
    const text = shown(field, value)
    if (field.required && !text && !HIDDEN.includes(field.type)) missing.push(field.label)
    if (field.sensitive || HIDDEN.includes(field.type) || !text) continue
    if (!field.required && field.default !== undefined && value === field.default) continue
    if (values.length < 2) values.push(text)
  }
  return { text: values.join(' · '), missing }
}
