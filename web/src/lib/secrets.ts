// Secrets as the editor lists them (GET /api/secrets): names, never values or
// lengths, and whether each value is short. What agents read (the read views,
// tool results) hides known secret values by comparison, so an agent whose
// own text comes back to it, at any agent deploy level, can see whether a
// guess comes back hidden: practical only for short or predictable values
// (ADR-0021, docs/design/mcp.md §6). The editor marks short ones so the admin
// can replace them with longer random values.
import type { Component } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import { msg } from '@/i18n'

/**
 * One place a secret is referenced ({{secret.NAME}}): a node setting of a
 * flow, or a broker connection's credential.
 */
export interface SecretUse {
  flowId?: string
  flowName?: string
  /** The flow is deployed: deleting the secret affects it now. */
  flowRunning?: boolean
  nodeId?: string
  nodeType?: string
  /** The node is disabled: it does not run. */
  nodeDisabled?: boolean
  connection?: string
  setting: string
}

/** A stored secret: its name, whether its value is short and where it is used. */
export interface SecretItem {
  name: string
  short: boolean
  uses: SecretUse[]
}

/** A secret name referenced somewhere that is not defined. */
export interface UndefinedSecret {
  name: string
  uses: SecretUse[]
}

export interface SecretList {
  secrets: SecretItem[]
  /** References to secrets that are not defined. */
  undefined: UndefinedSecret[]
  /** A value with fewer characters than this is short. */
  shortBelow: number
}

/** The server's threshold (secrets.ShortValueRunes), for a response without one. */
export const DEFAULT_SHORT_BELOW = 16

/** Reads one use, keeping only its known fields; null when it has no setting. */
export function secretUse(raw: unknown): SecretUse | null {
  const u = (raw ?? {}) as Record<string, unknown>
  if (typeof u.setting !== 'string') return null
  const use: SecretUse = { setting: u.setting }
  for (const key of ['flowId', 'flowName', 'nodeId', 'nodeType', 'connection'] as const) {
    if (typeof u[key] === 'string') use[key] = u[key] as string
  }
  if (u.flowRunning === true) use.flowRunning = true
  if (u.nodeDisabled === true) use.nodeDisabled = true
  return use
}

function secretUses(raw: unknown): SecretUse[] {
  return (Array.isArray(raw) ? raw : []).map(secretUse).filter((u): u is SecretUse => u !== null)
}

/** Reads the listing's data member, keeping only names, short marks and uses. */
export function secretList(data: unknown): SecretList {
  const d = (data ?? {}) as { secrets?: unknown; undefined?: unknown; shortBelow?: unknown }
  const raw = Array.isArray(d.secrets) ? d.secrets : []
  const secrets: SecretItem[] = []
  for (const s of raw) {
    const item = s as { name?: unknown; short?: unknown; uses?: unknown } | null
    if (item && typeof item.name === 'string') secrets.push({ name: item.name, short: item.short === true, uses: secretUses(item.uses) })
  }
  const undefinedSecrets: UndefinedSecret[] = []
  for (const s of Array.isArray(d.undefined) ? d.undefined : []) {
    const item = s as { name?: unknown; uses?: unknown } | null
    if (item && typeof item.name === 'string') undefinedSecrets.push({ name: item.name, uses: secretUses(item.uses) })
  }
  const shortBelow = typeof d.shortBelow === 'number' && d.shortBelow > 0 ? d.shortBelow : DEFAULT_SHORT_BELOW
  return { secrets, undefined: undefinedSecrets, shortBelow }
}

/** Where a secret is used, as one line ("Flow “Mailer” · node mail (action-email) · password"). */
export function secretUseText(u: SecretUse): string {
  const t = msg().settings.secrets
  if (u.connection) return t.useConnection(u.connection, u.setting)
  const parts = [t.useFlow(u.flowName || u.flowId || '?'), t.useNode(u.nodeId ?? '?', u.nodeType ?? ''), u.setting]
  if (u.nodeDisabled) parts.push(t.useDisabled)
  else if (u.flowRunning) parts.push(t.useRunning)
  return parts.join(' · ')
}

/** How many places use a secret, and how many of them run now. */
export function secretUsesSummary(uses: SecretUse[]): string {
  const t = msg().settings.secrets
  if (!uses.length) return t.unused
  const running = uses.filter(u => u.connection || (u.flowRunning && !u.nodeDisabled)).length
  return t.usedIn(uses.length, running)
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
    label: msg().settings.shortLabel,
    text: msg().settings.shortText(shortBelow)
  }
}

export interface ShortSecretsSummary {
  /** How many secrets are short (null: unknown). */
  short: number | null
  text: string
}

/** The Agents page's count of short secrets. */
export function shortSecretsSummary(list: SecretList | null): ShortSecretsSummary {
  const t = msg().settings
  if (!list) return { short: null, text: t.summaryUnknown }
  const total = list.secrets.length
  if (!total) return { short: 0, text: t.summaryNone }
  const short = list.secrets.filter(s => s.short).length
  // Length is no proof of randomness: say so even when nothing is short.
  if (!short) {
    return { short, text: t.summaryNoShort(list.shortBelow) }
  }
  return { short, text: t.summaryShort(short, total, list.shortBelow) }
}
