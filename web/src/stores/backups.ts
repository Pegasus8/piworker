// Automatic backups and restore (ADR-0032): the Backups page and the
// navigation's attention marker share this overview.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiClient } from '@/composables/useApi'
import { BACKGROUND_HEADERS } from '@/stores/agent'
import { backupAlert, overviewFrom, remoteRequest, remoteStateFrom, settingsRequest, type BackupEntry, type BackupSettings, type BackupsOverview, type RemoteSettings } from '@/lib/backups'

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

  /** The running process's instance, from the health check. */
  async function instance(): Promise<string> {
    try {
      const { data } = await apiClient.get('/health', { headers: BACKGROUND_HEADERS, timeout: 3000 })
      return typeof data?.instance === 'string' ? data.instance : ''
    } catch { return '' }
  }

  /**
   * Resolves true once a new PiWorker process (an instance other than
   * `before`) answers its health check, false after the timeout.
   */
  async function waitForRestart(before: string, opts: { timeoutMs?: number; intervalMs?: number; sleep?: (ms: number) => Promise<void> } = {}) {
    const sleep = opts.sleep ?? ((ms: number) => new Promise<void>(r => setTimeout(r, ms)))
    const interval = opts.intervalMs ?? 1500
    const deadline = Date.now() + (opts.timeoutMs ?? RESTART_TIMEOUT_MS)
    while (Date.now() < deadline) {
      await sleep(interval)
      const now = await instance()
      if (now && now !== before) return true
    }
    return false
  }

  /** Saves the remote destination; empty secrets keep the stored ones. */
  async function saveRemote(settings: RemoteSettings, extra: { secretKey: string; passphrase: string; password: string }) {
    const { data } = await apiClient.put('/backups/remote', remoteRequest(settings, extra))
    await fetchOverview()
    return remoteStateFrom(data.data)
  }

  /** Checks the destination; the stored key is used only for the stored endpoint and key ID. */
  async function testRemote(settings: RemoteSettings, secretKey: string) {
    await apiClient.post('/backups/remote/test', remoteRequest(settings, { secretKey, passphrase: '', password: '' }))
  }

  async function listRemote(): Promise<BackupEntry[]> {
    const { data } = await apiClient.get('/backups/remote/list')
    return overviewFrom({ backups: data.data }).backups
  }

  async function restoreRemote(name: string, password: string) {
    await apiClient.post(`/backups/remote/${path(name)}/restore`, { password })
  }

  /** The backup encrypted with passphrase, as a file to save. */
  async function download(name: string, passphrase: string, password: string): Promise<Blob> {
    const { data } = await apiClient.post(`/backups/${path(name)}/download`, { passphrase, password }, { responseType: 'blob' })
    return data as Blob
  }

  /** Restores an uploaded backup file (base64) on this installation. */
  async function upload(data: string, passphrase: string, password: string) {
    await apiClient.post('/backups/upload', { data, passphrase, password })
  }

  /** Restores a backup on the setup page of a fresh installation. */
  async function setupRestore(data: string, passphrase: string, code: string) {
    await apiClient.post('/auth/setup/restore', { data, passphrase, code })
  }

  return { overview, alert, fetchOverview, backUpNow, saveSettings, remove, restore, instance, waitForRestart,
    saveRemote, testRemote, listRemote, restoreRemote, download, upload, setupRestore }
})
