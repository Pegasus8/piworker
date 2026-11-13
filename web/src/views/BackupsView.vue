<script setup lang="ts">
// Automatic backups and restore (ADR-0032): status, settings, the backups
// on this device. A restore asks for the account password, then PiWorker
// restarts to apply it and every session ends.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Button, Card, Badge, Input, Label, Switch, Dialog } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { DatabaseBackup, History, Trash2 } from 'lucide-vue-next'
import { useFeedback } from '@/composables/useFeedback'
import { apiErrorMessage } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { useBackupsStore } from '@/stores/backups'
import { messages as m, formatDateTime } from '@/i18n'
import { formatSize, reasonLabel, retentionError, type BackupEntry, type BackupSettings } from '@/lib/backups'

const store = useBackupsStore()
const auth = useAuthStore()
const router = useRouter()
const { feedback, pending, run } = useFeedback()

const loading = ref(true)
const loadError = ref('')
const form = ref<BackupSettings>({ enabled: true, time: '03:17', dir: '', retention: { daily: 7, weekly: 4, monthly: 6 } })
const dirPassword = ref('')

function resetForm() {
  const s = store.overview?.settings
  if (s) form.value = { ...s, retention: { ...s.retention } }
  dirPassword.value = ''
}

async function load() {
  loadError.value = ''
  try {
    await store.fetchOverview()
    resetForm()
  } catch (e) {
    loadError.value = apiErrorMessage(e, m.value.backups.loadFailed)
  } finally {
    loading.value = false
  }
}
onMounted(load)

const ov = computed(() => store.overview)
const dirChanged = computed(() => form.value.dir.trim() !== (ov.value?.settings.dir ?? ''))
const needsDirPassword = computed(() => dirChanged.value && auth.authEnabled)
const formError = computed(() => retentionError(form.value.retention))
const canSave = computed(() => !pending.value && !formError.value && /^([01]\d|2[0-3]):[0-5]\d$/.test(form.value.time) && (!needsDirPassword.value || !!dirPassword.value))

async function save() {
  if (!canSave.value) return
  const ok = await run(() => store.saveSettings(form.value, needsDirPassword.value ? dirPassword.value : ''), m.value.backups.saved, e => apiErrorMessage(e, m.value.backups.saveFailed))
  if (ok) resetForm()
}

async function backUpNow() {
  await run(() => store.backUpNow(), m.value.backups.backedUp, e => apiErrorMessage(e, m.value.backups.backUpFailed))
}

const confirm = ref<InstanceType<typeof ConfirmDialog> | null>(null)
async function remove(b: BackupEntry) {
  const when = formatDateTime(b.created)
  if (!(await confirm.value?.ask(m.value.backups.deleteTitle, m.value.backups.deleteText(when), m.value.backups.delete))) return
  await run(() => store.remove(b.name), m.value.backups.deleted, e => apiErrorMessage(e, m.value.backups.deleteFailed))
}

// Restore
const restoring = ref<BackupEntry | null>(null)
const restorePassword = ref('')
const restoreError = ref('')
const restarting = ref(false)
const restartSlow = ref(false)
function askRestore(b: BackupEntry) {
  restoring.value = b
  restorePassword.value = ''
  restoreError.value = ''
}
async function doRestore() {
  const b = restoring.value
  if (!b || pending.value) return
  restoreError.value = ''
  try {
    pending.value = true
    await store.restore(b.name, restorePassword.value)
  } catch (e) {
    restoreError.value = apiErrorMessage(e, m.value.backups.restoreFailed)
    return
  } finally {
    pending.value = false
  }
  restoring.value = null
  restarting.value = true
  const back = await store.waitForRestart()
  if (!back) {
    restartSlow.value = true
    return
  }
  // Every session ended with the restore.
  if (auth.authEnabled) {
    auth.clearAuth()
    await router.push('/login')
  } else {
    window.location.reload()
  }
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-5xl space-y-6">
      <header class="flex flex-wrap items-end justify-between gap-4">
        <div class="space-y-2">
          <h1 class="flex items-center gap-2 text-2xl font-bold"><DatabaseBackup class="h-6 w-6 text-primary" aria-hidden="true" />{{ m.backups.title }}</h1>
          <p class="max-w-2xl text-muted-foreground">{{ m.backups.intro }}</p>
        </div>
        <Button :disabled="pending || loading || restarting" @click="backUpNow"><DatabaseBackup class="mr-2 h-4 w-4" />{{ m.backups.backUpNow }}</Button>
      </header>

      <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />
      <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>

      <div v-if="restarting" role="status" class="flex items-center gap-3 rounded-lg border p-4">
        <div v-if="!restartSlow" class="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" aria-hidden="true" />
        <p class="text-sm">{{ restartSlow ? m.backups.restartSlow : m.backups.restarting }}</p>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-12" role="status" :aria-label="m.backups.loading">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>

      <template v-else-if="ov">
        <p v-if="store.alert" :role="store.alert.kind === 'error' ? 'alert' : 'status'"
          :class="store.alert.kind === 'error' ? 'text-destructive' : 'text-amber-700 dark:text-amber-400'" class="text-sm">{{ store.alert.text }}</p>

        <!-- Status -->
        <Card class="p-4 sm:p-6">
          <h2 class="mb-3 font-semibold">{{ m.backups.statusTitle }}</h2>
          <dl class="grid gap-3 text-sm sm:grid-cols-3">
            <div>
              <dt class="text-muted-foreground">{{ m.backups.lastSuccess }}</dt>
              <dd>{{ ov.status.lastSuccess ? formatDateTime(ov.status.lastSuccess) : m.backups.never }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">{{ m.backups.nextRun }}</dt>
              <dd>{{ ov.nextRun ? formatDateTime(ov.nextRun) : m.backups.off }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">{{ m.backups.location }}</dt>
              <dd class="break-all font-mono text-xs">{{ ov.dir }}</dd>
            </div>
          </dl>
        </Card>

        <!-- Settings -->
        <Card class="p-4 sm:p-6">
          <form class="space-y-4" @submit.prevent="save">
            <h2 class="font-semibold">{{ m.backups.settingsTitle }}</h2>
            <label class="flex items-center gap-3 text-sm">
              <Switch v-model="form.enabled" :disabled="pending" :aria-label="m.backups.enabled" />
              {{ m.backups.enabled }}
            </label>
            <div class="grid gap-4 sm:grid-cols-[10rem_1fr]">
              <div>
                <Label for="backup-time">{{ m.backups.time }}</Label>
                <Input id="backup-time" v-model="form.time" type="time" class="mt-1" :disabled="pending" aria-describedby="backup-time-hint" />
              </div>
              <div>
                <Label for="backup-dir">{{ m.backups.dir }}</Label>
                <Input id="backup-dir" v-model="form.dir" class="mt-1 font-mono" :placeholder="ov.defaultDir" :disabled="pending" spellcheck="false" aria-describedby="backup-dir-hint" />
              </div>
            </div>
            <p id="backup-time-hint" class="text-xs text-muted-foreground">{{ m.backups.timeHint }}</p>
            <p id="backup-dir-hint" class="text-xs text-muted-foreground">{{ m.backups.dirHint(ov.defaultDir) }}</p>
            <div v-if="needsDirPassword">
              <Label for="backup-dir-password">{{ m.backups.dirPassword }}</Label>
              <Input id="backup-dir-password" v-model="dirPassword" type="password" autocomplete="current-password" class="mt-1 sm:max-w-xs" :disabled="pending" aria-describedby="backup-dir-password-hint" />
              <p id="backup-dir-password-hint" class="mt-1 text-xs text-muted-foreground">{{ m.backups.dirPasswordHint }}</p>
            </div>
            <fieldset class="space-y-2">
              <legend class="text-sm font-medium">{{ m.backups.keep }}</legend>
              <div class="grid grid-cols-3 gap-3 sm:max-w-md">
                <div>
                  <Label for="keep-daily" class="text-xs">{{ m.backups.keepDaily }}</Label>
                  <Input id="keep-daily" v-model.number="form.retention.daily" type="number" min="0" max="400" step="1" class="mt-1" :disabled="pending" />
                </div>
                <div>
                  <Label for="keep-weekly" class="text-xs">{{ m.backups.keepWeekly }}</Label>
                  <Input id="keep-weekly" v-model.number="form.retention.weekly" type="number" min="0" max="400" step="1" class="mt-1" :disabled="pending" />
                </div>
                <div>
                  <Label for="keep-monthly" class="text-xs">{{ m.backups.keepMonthly }}</Label>
                  <Input id="keep-monthly" v-model.number="form.retention.monthly" type="number" min="0" max="400" step="1" class="mt-1" :disabled="pending" />
                </div>
              </div>
              <p class="text-xs text-muted-foreground">{{ m.backups.keepHint }}</p>
              <p v-if="formError" class="text-xs text-destructive" aria-live="polite">{{ formError }}</p>
            </fieldset>
            <Button type="submit" :disabled="!canSave">{{ m.backups.save }}</Button>
          </form>
        </Card>

        <!-- List -->
        <Card class="p-4 sm:p-6">
          <h2 class="mb-3 font-semibold">{{ m.backups.listTitle }}</h2>
          <p v-if="!ov.backups.length" class="text-sm text-muted-foreground">{{ m.backups.empty }}</p>
          <div v-else class="overflow-x-auto">
            <table class="w-full min-w-[560px] text-left text-sm">
              <thead class="border-b text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th scope="col" class="py-2 pr-3 font-medium">{{ m.backups.created }}</th>
                  <th scope="col" class="py-2 pr-3 font-medium">{{ m.backups.reason }}</th>
                  <th scope="col" class="py-2 pr-3 font-medium">{{ m.backups.size }}</th>
                  <th scope="col" class="py-2 pr-3 font-medium">{{ m.backups.version }}</th>
                  <th scope="col" class="py-2 font-medium"><span class="sr-only">{{ m.backups.actions }}</span></th>
                </tr>
              </thead>
              <tbody class="divide-y">
                <tr v-for="b in ov.backups" :key="b.name">
                  <th scope="row" class="py-2 pr-3 font-normal">{{ formatDateTime(b.created) }}</th>
                  <td class="py-2 pr-3"><Badge variant="outline">{{ reasonLabel(b.reason) }}</Badge></td>
                  <td class="py-2 pr-3">{{ formatSize(b.size) }}</td>
                  <td class="py-2 pr-3 font-mono text-xs">{{ b.version || '—' }}</td>
                  <td class="py-2 text-right">
                    <div class="flex justify-end gap-2">
                      <Button size="sm" variant="outline" :disabled="pending || restarting" @click="askRestore(b)">
                        <History class="mr-1 h-4 w-4" aria-hidden="true" />{{ m.backups.restore }}
                      </Button>
                      <Button size="sm" variant="ghost" :disabled="pending || restarting" :aria-label="`${m.backups.delete} ${formatDateTime(b.created)}`" @click="remove(b)">
                        <Trash2 class="h-4 w-4" aria-hidden="true" />
                      </Button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Card>
      </template>
    </div>

    <ConfirmDialog ref="confirm" />

    <Dialog :open="!!restoring" :title="m.backups.restoreTitle" @update:open="v => { if (!v) restoring = null }">
      <form v-if="restoring" class="space-y-4" @submit.prevent="doRestore">
        <h2 class="text-lg font-semibold">{{ m.backups.restoreTitle }}</h2>
        <p class="text-sm text-muted-foreground">{{ m.backups.restoreText(formatDateTime(restoring.created)) }}</p>
        <p class="text-sm font-medium">{{ m.backups.restoreRestart }}</p>
        <div v-if="auth.authEnabled">
          <Label for="restore-password">{{ m.backups.restorePassword }}</Label>
          <Input id="restore-password" v-model="restorePassword" type="password" autocomplete="current-password" class="mt-1" :disabled="pending" />
        </div>
        <p v-if="restoreError" role="alert" class="text-sm text-destructive">{{ restoreError }}</p>
        <div class="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" :disabled="pending" @click="restoring = null">{{ m.common.cancel }}</Button>
          <Button type="submit" variant="destructive" :disabled="pending || (auth.authEnabled && !restorePassword)">{{ m.backups.restoreConfirm }}</Button>
        </div>
      </form>
    </Dialog>
  </main>
</template>
