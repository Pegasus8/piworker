// Secrets as the editor lists them (GET /api/secrets): names, never values or
// lengths, and whether each value is short. What agents read (the read views,
// tool results) hides known secret values by comparison, so an agent whose
// own text comes back to it, at any agent deploy level, can see whether a
// guess comes back hidden: practical only for short or predictable values
// (ADR-0021, docs/design/mcp.md §6). The editor marks short ones so the admin
// can replace them with longer random values.
import type { Component } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'

/** A stored secret: its name and whether its value is short. */
export interface SecretItem {
  name: string
  short: boolean
}

export interface SecretList {
  secrets: SecretItem[]
  /** A value with fewer characters than this is short. */
  shortBelow: number
}

/** The server's threshold (secrets.ShortValueRunes), for a response without one. */
export const DEFAULT_SHORT_BELOW = 16

/** Reads the listing's data member, keeping only names and short marks. */
export function secretList(data: unknown): SecretList {
  const d = (data ?? {}) as { secrets?: unknown; shortBelow?: unknown }
  const raw = Array.isArray(d.secrets) ? d.secrets : []
  const secrets: SecretItem[] = []
  for (const s of raw) {
    const item = s as { name?: unknown; short?: unknown } | null
    if (item && typeof item.name === 'string') secrets.push({ name: item.name, short: item.short === true })
  }
  const shortBelow = typeof d.shortBelow === 'number' && d.shortBelow > 0 ? d.shortBelow : DEFAULT_SHORT_BELOW
  return { secrets, shortBelow }
}

/**
 * Whether a value being typed is short. Characters are counted as code
 * points, as the server counts runes.
 */
export function isShortValue(value: string, shortBelow = DEFAULT_SHORT_BELOW): boolean {
  return [...value].length < shortBelow
}

export interface ShortSecretNotice {
  icon: Component
  /** A short label shown before the text. */
  label: string
  text: string
}

/** How a short secret is marked: an icon and words, never color alone. */
export function shortSecretNotice(shortBelow = DEFAULT_SHORT_BELOW): ShortSecretNotice {
  return {
    icon: AlertTriangle,
    label: 'Short value',
    text: `An agent could confirm a guess of it (see Agents). Use ${shortBelow} random characters or more.`
  }
}

export interface ShortSecretsSummary {
  /** How many secrets are short (null: unknown). */
  short: number | null
  text: string
}

/** The Agents page's count of short secrets. */
export function shortSecretsSummary(list: SecretList | null): ShortSecretsSummary {
  if (!list) return { short: null, text: 'Could not check the secrets for short values.' }
  const total = list.secrets.length
  if (!total) return { short: 0, text: 'No secrets are stored.' }
  const short = list.secrets.filter(s => s.short).length
  // Length is no proof of randomness: say so even when nothing is short.
  if (!short) {
    return {
      short,
      text: `No secret is shorter than ${list.shortBelow} characters. Use random values: a long word or date can still be guessed.`
    }
  }
  const one = short === 1
  return {
    short,
    text: `${short} of ${total} secrets ${one ? 'is' : 'are'} short (fewer than ${list.shortBelow} characters): an agent could confirm a guess of ${one ? 'it' : 'them'}.`
  }
}
