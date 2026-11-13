// Automatic backups (ADR-0032): the shape of GET /api/backups and the
// presentation rules the Backups page and the navigation share.
import { msg, formatNumber, formatDateTime } from '@/i18n'

export interface Retention { daily: number; weekly: number; monthly: number }
export interface BackupSettings { enabled: boolean; time: string; dir: string; retention: Retention }
export interface BackupStatus { lastAttempt?: string; lastSuccess?: string; lastError?: string; lastName?: string }
export interface BackupEntry { name: string; size: number; created: string; reason: string; version: string }
export interface BackupsOverview {
  settings: BackupSettings
  dir: string
  defaultDir: string
  status: BackupStatus
  nextRun: string | null
  backups: BackupEntry[]
  restorePending: boolean
}
export interface BackupAlert { kind: 'error' | 'warning'; text: string }

/** A backup older than this, while backups are on, is worth a warning. */
export const STALE_AFTER_MS = 48 * 3600 * 1000

const str = (v: unknown) => (typeof v === 'string' ? v : '')
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0)
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' ? (v as Record<string, unknown>) : {})

/** Reads the server's overview; missing parts become empty values. */
export function overviewFrom(raw: unknown): BackupsOverview {
  const r = obj(raw)
  const s = obj(r.settings)
  const ret = obj(s.retention)
  const st = obj(r.status)
  return {
    settings: {
      enabled: s.enabled === true,
      time: str(s.time) || '03:17',
      dir: str(s.dir),
      retention: { daily: num(ret.daily), weekly: num(ret.weekly), monthly: num(ret.monthly) }
    },
    dir: str(r.dir),
    defaultDir: str(r.defaultDir),
    status: {
      lastAttempt: str(st.lastAttempt) || undefined,
      lastSuccess: str(st.lastSuccess) || undefined,
      lastError: str(st.lastError) || undefined,
      lastName: str(st.lastName) || undefined
    },
    nextRun: str(r.nextRun) || null,
    backups: (Array.isArray(r.backups) ? r.backups : []).map(b => {
      const e = obj(b)
      const m = obj(e.manifest)
      return { name: str(e.name), size: num(e.size), created: str(m.created), reason: str(m.reason), version: str(m.piworker) }
    }),
    restorePending: r.restorePending === true
  }
}

/** What needs the admin's attention: a failure, a stale backup, a pending restore. */
export function backupAlert(ov: BackupsOverview, now = Date.now()): BackupAlert | null {
  const t = msg().backups
  if (ov.restorePending) return { kind: 'warning', text: t.alertPending }
  if (!ov.settings.enabled) return null
  if (ov.status.lastError) return { kind: 'error', text: t.alertFailed(ov.status.lastError) }
  const last = ov.status.lastSuccess ? Date.parse(ov.status.lastSuccess) : NaN
  if (Number.isFinite(last) && now - last > STALE_AFTER_MS) return { kind: 'warning', text: t.alertStale(formatDateTime(last)) }
  return null
}

/** 5378 → "5.3 KB" (binary units, one decimal under 10). */
export function formatSize(bytes: number): string {
  const units = msg().backups.sizeUnits
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) { value /= 1024; i++ }
  const digits = i === 0 || value >= 10 ? 0 : 1
  return `${formatNumber(Number(value.toFixed(digits)), { maximumFractionDigits: digits })} ${units[i]}`
}

export function reasonLabel(reason: string): string {
  return msg().backups.reasons[reason] ?? reason
}

/** Empty when the counts are valid (whole numbers from 0, at least one backup kept). */
export function retentionError(r: Retention): string {
  const t = msg().backups
  const values = [r.daily, r.weekly, r.monthly]
  if (values.some(v => !Number.isInteger(v) || v < 0)) return t.retentionInvalid
  if (values.every(v => v === 0)) return t.retentionNone
  return ''
}

/** PUT /api/backups/settings's body; the password only when one is given. */
export function settingsRequest(s: BackupSettings, password: string): BackupSettings & { password?: string } {
  const body: BackupSettings & { password?: string } = {
    enabled: s.enabled,
    time: s.time,
    dir: s.dir.trim(),
    retention: { daily: s.retention.daily, weekly: s.retention.weekly, monthly: s.retention.monthly }
  }
  if (password) body.password = password
  return body
}
