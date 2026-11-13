// Automatic backups and restore (ADR-0032): the Backups page and the
// navigation's attention marker share this overview.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiClient } from '@/composables/useApi'
import { BACKGROUND_HEADERS } from '@/stores/agent'
import { backupAlert, overviewFrom, settingsRequest, type BackupSettings, type BackupsOverview } from '@/lib/backups'

/** How often the navigation re-reads the backup status. */
export const BACKUPS_POLL_MS = 5 * 60_000
/** How long to wait for PiWorker to come back after a restore. */
export const RESTART_TIMEOUT_MS = 90_000

const path = (name: string) => encodeURIComponent(name)

export const useBackupsStore = defineStore('backups', () => {
  const overview = ref<BackupsOverview | null>(null)
  const alert = computed(() => (overview.value ? backupAlert(overview.value) : null))

  async function fetchOverview(opts: { background?: boolean } = {}) {
    const { data } = await apiClient.get('/backups', opts.background ? { headers: BACKGROUND_HEADERS } : {})
    overview.value = overviewFrom(data.data)
  }

  async function backUpNow() {
    await apiClient.post('/backups')
    await fetchOverview()
  }

  /** The password is needed only when the directory changes. */
  async function saveSettings(settings: BackupSettings, password = '') {
    await apiClient.put('/backups/settings', settingsRequest(settings, password))
    await fetchOverview()
  }

  async function remove(name: string) {
    await apiClient.delete(`/backups/${path(name)}`)
    await fetchOverview()
  }

  /** Stages the restore; PiWorker restarts to apply it. */
  async function restore(name: string, password: string) {
    await apiClient.post(`/backups/${path(name)}/restore`, { password })
  }

  /**
   * Resolves true once PiWorker answers its health check again after having
   * gone away, false after the timeout.
   */
  async function waitForRestart(opts: { timeoutMs?: number; intervalMs?: number; sleep?: (ms: number) => Promise<void> } = {}) {
    const sleep = opts.sleep ?? ((ms: number) => new Promise<void>(r => setTimeout(r, ms)))
    const interval = opts.intervalMs ?? 1500
    const deadline = Date.now() + (opts.timeoutMs ?? RESTART_TIMEOUT_MS)
    let wentAway = false
    while (Date.now() < deadline) {
      await sleep(interval)
      try {
        await apiClient.get('/health', { headers: BACKGROUND_HEADERS, timeout: 3000 })
        if (wentAway) return true
      } catch {
        wentAway = true
      }
    }
    return false
  }

  return { overview, alert, fetchOverview, backUpNow, saveSettings, remove, restore, waitForRestart }
})
