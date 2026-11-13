// Export and restore of the stores a flow export does not carry: global and
// flow variables, broker connections and secrets (POST /api/stores/export
// and /api/stores/import, ADR-0031). The file is read in the browser first
// so the admin sees what it holds before importing; secret values and
// connections with passwords are sealed with a passphrase and are never
// readable here.
import { msg } from '@/i18n'

/** The server's minimum passphrase length (storesbackup.MinPassphraseRunes). */
export const MIN_PASSPHRASE = 12

export interface BundleSummary {
  createdAt: string
  variables: number
  flowVariables: { flowName: string; count: number }[]
  /** Connections readable in the file (without passwords). */
  connections: string[]
  secrets: string[]
  /** Secret values and connections are sealed: a passphrase opens them. */
  sealed: boolean
}

/** Reads a chosen file: its summary, or an error text when it is not an export. */
export function bundleSummary(raw: unknown): { summary: BundleSummary } | { error: string } {
  const t = msg().settings.backup
  const b = (raw ?? {}) as Record<string, unknown>
  if (b.format !== 'piworker-stores') return { error: t.notAnExport }
  if (b.version !== 1) return { error: t.unsupportedVersion(String(b.version)) }
  const obj = (v: unknown) => (v && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {})
  const list = (v: unknown) => (Array.isArray(v) ? v : [])
  const flowVariables = list(b.flowVariables).map(f => {
    const fv = obj(f)
    return { flowName: typeof fv.flowName === 'string' ? fv.flowName : '?', count: Object.keys(obj(fv.variables)).length }
  })
  return {
    summary: {
      createdAt: typeof b.createdAt === 'string' ? b.createdAt : '',
      variables: Object.keys(obj(b.variables)).length,
      flowVariables,
      connections: list(b.connections).map(c => obj(c).name).filter((n): n is string => typeof n === 'string'),
      secrets: list(b.secrets).filter((n): n is string => typeof n === 'string'),
      sealed: !!b.sealed && typeof b.sealed === 'object'
    }
  }
}

export interface Counts { added: number; replaced: number; skipped: number }
export interface FlowRef { id: string; name: string }
export interface ImportReport {
  variables: Counts
  flowVariables: Counts
  secrets: Counts
  connections: Counts
  unmatchedFlows: FlowRef[]
  secretsNotRestored: string[]
  passwordsNotRestored: string[]
}
export interface ImportResult {
  report: ImportReport
  restarted: FlowRef[]
  failed: (FlowRef & { error: string })[]
}

const counts = (v: unknown): Counts => {
  const c = (v ?? {}) as Record<string, unknown>
  const n = (x: unknown) => (typeof x === 'number' && x >= 0 ? x : 0)
  return { added: n(c.added), replaced: n(c.replaced), skipped: n(c.skipped) }
}
const refs = (v: unknown): FlowRef[] => (Array.isArray(v) ? v : []).flatMap(r => {
  const f = (r ?? {}) as Record<string, unknown>
  return typeof f.id === 'string' ? [{ id: f.id, name: typeof f.name === 'string' ? f.name : f.id }] : []
})
const names = (v: unknown): string[] => (Array.isArray(v) ? v : []).filter((n): n is string => typeof n === 'string')

/** Reads the import's data member. */
export function importResult(data: unknown): ImportResult {
  const d = (data ?? {}) as Record<string, unknown>
  const r = (d.report ?? {}) as Record<string, unknown>
  return {
    report: {
      variables: counts(r.variables), flowVariables: counts(r.flowVariables), secrets: counts(r.secrets), connections: counts(r.connections),
      unmatchedFlows: refs(r.unmatchedFlows), secretsNotRestored: names(r.secretsNotRestored), passwordsNotRestored: names(r.passwordsNotRestored)
    },
    restarted: refs(d.restarted),
    failed: (Array.isArray(d.failed) ? d.failed : []).flatMap(f => {
      const [ref] = refs([f])
      const error = ((f ?? {}) as { error?: unknown }).error
      return ref ? [{ ...ref, error: typeof error === 'string' ? error : '' }] : []
    })
  }
}

/** The import's outcome as sentences, and whether something needs attention. */
export function importResultText(r: ImportResult): { kind: 'success' | 'warning'; lines: string[] } {
  const t = msg().settings.backup
  const line = (label: string, c: Counts, feminine: boolean) => (c.added || c.replaced || c.skipped ? t.countsLine(label, c.added, c.replaced, c.skipped, feminine) : '')
  const lines = [
    line(t.kindVariables, r.report.variables, true),
    line(t.kindFlowVariables, r.report.flowVariables, true),
    line(t.kindSecrets, r.report.secrets, false),
    line(t.kindConnections, r.report.connections, true)
  ].filter(Boolean)
  if (!lines.length) lines.push(t.nothingImported)
  if (r.restarted.length) lines.push(t.restarted(r.restarted.map(f => f.name)))
  const warnings: string[] = []
  if (r.report.secretsNotRestored.length) warnings.push(t.secretsNotRestored(r.report.secretsNotRestored))
  if (r.report.passwordsNotRestored.length) warnings.push(t.passwordsNotRestored(r.report.passwordsNotRestored))
  if (r.report.unmatchedFlows.length) warnings.push(t.unmatchedFlows(r.report.unmatchedFlows.map(f => f.name)))
  if (r.failed.length) warnings.push(t.restartFailed(r.failed.map(f => `${f.name}: ${f.error}`)))
  return { kind: warnings.length ? 'warning' : 'success', lines: [...lines, ...warnings] }
}
