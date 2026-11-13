import { expect, test } from 'bun:test'
import { setLocale } from '../src/i18n'
import { backupAlert, formatSize, overviewFrom, reasonLabel, retentionError, settingsRequest } from '../src/lib/backups'

const raw = {
  settings: { enabled: true, time: '03:17', dir: '', retention: { daily: 7, weekly: 4, monthly: 6 } },
  dir: '/var/lib/piworker/backups',
  defaultDir: '/var/lib/piworker/backups',
  status: { lastAttempt: '2026-11-13T03:20:00Z', lastSuccess: '2026-11-13T03:20:00Z', lastName: 'piworker-backup-20261113T032000Z.tar.gz' },
  nextRun: '2026-11-14T03:25:00Z',
  backups: [
    { name: 'piworker-backup-20261113T032000Z.tar.gz', size: 5378, manifest: { created: '2026-11-13T03:20:00Z', reason: 'scheduled', piworker: 'v1.2.3' } }
  ],
  restorePending: false
}

test('overviewFrom reads the server answer and tolerates missing parts', () => {
  const ov = overviewFrom(raw)
  expect(ov.settings.time).toBe('03:17')
  expect(ov.backups).toHaveLength(1)
  expect(ov.backups[0]).toEqual({ name: 'piworker-backup-20261113T032000Z.tar.gz', size: 5378, created: '2026-11-13T03:20:00Z', reason: 'scheduled', version: 'v1.2.3' })
  expect(ov.nextRun).toBe('2026-11-14T03:25:00Z')

  const empty = overviewFrom({})
  expect(empty.backups).toEqual([])
  expect(empty.settings.enabled).toBe(false)
  expect(empty.nextRun).toBeNull()
})

test('backupAlert warns about a failure, a stale backup and a pending restore', () => {
  setLocale('en')
  const now = Date.parse('2026-11-13T12:00:00Z')
  expect(backupAlert(overviewFrom(raw), now)).toBeNull()

  const failed = overviewFrom({ ...raw, status: { ...raw.status, lastAttempt: '2026-11-13T11:00:00Z', lastError: 'disk full' } })
  expect(backupAlert(failed, now)).toEqual({ kind: 'error', text: expect.stringContaining('disk full') })

  const stale = overviewFrom(raw)
  expect(backupAlert(stale, Date.parse('2026-11-15T12:00:00Z'))?.kind).toBe('warning')

  const never = overviewFrom({ ...raw, status: {}, backups: [] })
  expect(backupAlert(never, now)).toBeNull() // the first backup is still to come

  const disabled = overviewFrom({ ...raw, settings: { ...raw.settings, enabled: false } })
  expect(backupAlert(disabled, Date.parse('2026-12-15T12:00:00Z'))).toBeNull()

  const pending = overviewFrom({ ...raw, restorePending: true })
  expect(backupAlert(pending, now)?.kind).toBe('warning')
})

test('formatSize and reasonLabel', () => {
  setLocale('en')
  expect(formatSize(512)).toBe('512 B')
  expect(formatSize(5378)).toBe('5.3 KB')
  expect(formatSize(3 * 1024 * 1024)).toBe('3 MB')
  expect(reasonLabel('scheduled')).toBe('Scheduled')
  expect(reasonLabel('before-restore')).toBe('Before a restore')
  expect(reasonLabel('other')).toBe('other')
  setLocale('es')
  expect(reasonLabel('manual')).toBe('Manual')
  setLocale('en')
})

test('retentionError and settingsRequest', () => {
  setLocale('en')
  expect(retentionError({ daily: 0, weekly: 0, monthly: 0 })).not.toBe('')
  expect(retentionError({ daily: 1, weekly: 0, monthly: 0 })).toBe('')
  expect(retentionError({ daily: -1, weekly: 1, monthly: 0 })).not.toBe('')
  expect(retentionError({ daily: 1.5, weekly: 1, monthly: 0 })).not.toBe('')

  const req = settingsRequest({ enabled: true, time: '03:17', dir: '  /mnt/usb/piworker  ', retention: { daily: 7, weekly: 4, monthly: 6 } }, '')
  expect(req).toEqual({ enabled: true, time: '03:17', dir: '/mnt/usb/piworker', retention: { daily: 7, weekly: 4, monthly: 6 } })
  expect(settingsRequest({ enabled: true, time: '03:17', dir: '', retention: { daily: 7, weekly: 4, monthly: 6 } }, 'pw').password).toBe('pw')
})
