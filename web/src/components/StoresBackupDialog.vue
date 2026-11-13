<script setup lang="ts">
// Export and restore of variables, broker connections and secrets
// (ADR-0031). Exporting secret values seals them with a passphrase and asks
// for the account password; importing shows what the file holds first.
import { computed, ref, watch } from 'vue'
import { Button, Input, Label, Dialog } from '@/components/ui'
import { ArchiveRestore, Download, Upload } from 'lucide-vue-next'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import { apiErrorMessage } from '@/stores/agent'
import { messages as m, formatDateTime } from '@/i18n'
import { downloadJson } from '@/lib/download'
import { MIN_PASSPHRASE, bundleSummary, importResultText, type BundleSummary } from '@/lib/storesBackup'

const props = defineProps<{ open: boolean }>()
// changed: an import wrote secrets or connections.
const emit = defineEmits<{ 'update:open': [boolean]; changed: [] }>()

const api = useApi()
const auth = useAuthStore()
const { feedback, pending, run } = useFeedback()

// Export
const includeSecrets = ref(false)
const passphrase = ref('')
const passphraseRepeat = ref('')
const password = ref('')
const passphraseError = computed(() => {
  if (!includeSecrets.value) return ''
  if ([...passphrase.value].length < MIN_PASSPHRASE) return m.value.settings.backup.passphraseShort(MIN_PASSPHRASE)
  if (passphrase.value !== passphraseRepeat.value) return m.value.settings.backup.passphraseMismatch
  return ''
})
const canExport = computed(() => !pending.value && (!includeSecrets.value || (auth.authEnabled && !passphraseError.value && !!password.value)))

// Import
const fileInput = ref<HTMLInputElement | null>(null)
const fileName = ref('')
const bundle = ref<unknown>(null)
const summary = ref<BundleSummary | null>(null)
const fileError = ref('')
const importPassphrase = ref('')
const overwrite = ref(false)
const resultLines = ref<string[]>([])

function reset() {
  includeSecrets.value = false
  passphrase.value = passphraseRepeat.value = password.value = ''
  fileName.value = fileError.value = importPassphrase.value = ''
  bundle.value = summary.value = null
  overwrite.value = false
  resultLines.value = []
  feedback.value = null
  if (fileInput.value) fileInput.value.value = ''
}
watch(() => props.open, open => { if (open) reset() })

async function doExport() {
  if (!canExport.value) return
  const withSecrets = includeSecrets.value
  const ok = await run(async () => {
    const file = await api.exportStores(withSecrets ? passphrase.value : '', withSecrets ? password.value : '')
    downloadJson(file, `piworker-stores-${new Date().toISOString().slice(0, 10)}.json`)
  }, withSecrets ? m.value.settings.backup.exportedSecrets : m.value.settings.backup.exported, e => apiErrorMessage(e, m.value.settings.backup.exportFailed))
  // The password is asked again for every export with secrets.
  if (ok) password.value = ''
}

async function onFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  bundle.value = summary.value = null
  fileError.value = ''
  resultLines.value = []
  if (!file) return
  fileName.value = file.name
  let raw: unknown
  try {
    raw = JSON.parse(await file.text())
  } catch {
    fileError.value = m.value.settings.backup.unreadable
    return
  }
  const read = bundleSummary(raw)
  if ('error' in read) {
    fileError.value = read.error
    return
  }
  bundle.value = raw
  summary.value = read.summary
}

const summaryParts = computed(() => {
  const s = summary.value
  if (!s) return []
  const t = m.value.settings.backup
  const parts = [t.summaryVariables(s.variables)]
  if (s.flowVariables.length) parts.push(t.summaryFlows(s.flowVariables.map(f => f.flowName)))
  if (s.connections.length) parts.push(t.summaryConnections(s.connections.length))
  parts.push(t.summarySecrets(s.secrets.length))
  return parts
})

const summaryText = computed(() => {
  const created = summary.value?.createdAt ? m.value.settings.backup.summaryCreated(formatDateTime(summary.value.createdAt)) : ''
  return [created, summaryParts.value.join(' · ')].filter(Boolean).join(' ')
})

async function doImport() {
  if (!bundle.value || pending.value) return
  let lines: string[] = []
  let kind: 'success' | 'warning' = 'success'
  const ok = await run(async () => {
    const result = await api.importStores(bundle.value, importPassphrase.value, overwrite.value)
    const text = importResultText(result)
    lines = text.lines
    kind = text.kind
  }, '', e => apiErrorMessage(e, m.value.settings.backup.importFailed))
  if (ok) {
    resultLines.value = lines
    feedback.value = { kind, text: lines[0] }
    emit('changed')
  }
}
</script>

<template>
  <Dialog :title="m.settings.backup.dialogTitle" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <ArchiveRestore class="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div>
            <h2 class="text-lg font-semibold">{{ m.settings.backup.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.settings.backup.intro }}</p>
          </div>
        </div>

        <OperationFeedback :message="feedback" @dismiss="feedback = null" />

        <!-- Export -->
        <section class="space-y-2 rounded border p-3" aria-labelledby="backup-export-title">
          <h3 id="backup-export-title" class="font-medium">{{ m.settings.backup.exportTitle }}</h3>
          <p class="text-xs text-muted-foreground">{{ m.settings.backup.exportHint }}</p>
          <label class="flex items-start gap-2 text-sm">
            <input v-model="includeSecrets" type="checkbox" class="mt-0.5 h-4 w-4" :disabled="pending || !auth.authEnabled" />
            <span>{{ m.settings.backup.includeSecrets }}</span>
          </label>
          <p v-if="!auth.authEnabled" class="text-xs text-muted-foreground">{{ m.settings.backup.noAuth }}</p>
          <div v-if="includeSecrets" class="space-y-2">
            <p class="text-xs text-muted-foreground">{{ m.settings.backup.includeSecretsHint(MIN_PASSPHRASE) }}</p>
            <div class="grid gap-2 sm:grid-cols-2">
              <div>
                <Label class="text-xs" for="backup-passphrase">{{ m.settings.backup.passphrase }}</Label>
                <Input id="backup-passphrase" v-model="passphrase" type="password" autocomplete="new-password" class="mt-1" :disabled="pending" />
              </div>
              <div>
                <Label class="text-xs" for="backup-passphrase-repeat">{{ m.settings.backup.passphraseRepeat }}</Label>
                <Input id="backup-passphrase-repeat" v-model="passphraseRepeat" type="password" autocomplete="new-password" class="mt-1" :disabled="pending" />
              </div>
            </div>
            <p v-if="passphrase && passphraseError" class="text-xs text-destructive" aria-live="polite">{{ passphraseError }}</p>
            <div>
              <Label class="text-xs" for="backup-account-password">{{ m.settings.backup.accountPassword }}</Label>
              <Input id="backup-account-password" v-model="password" type="password" autocomplete="current-password" class="mt-1" :disabled="pending" aria-describedby="backup-account-password-hint" />
              <p id="backup-account-password-hint" class="mt-1 text-xs text-muted-foreground">{{ m.settings.backup.accountPasswordHint }}</p>
            </div>
          </div>
          <Button size="sm" :disabled="!canExport" @click="doExport">
            <Download class="mr-2 h-4 w-4" />{{ m.settings.backup.export }}
          </Button>
        </section>

        <!-- Import -->
        <section class="space-y-2 rounded border p-3" aria-labelledby="backup-import-title">
          <h3 id="backup-import-title" class="font-medium">{{ m.settings.backup.importTitle }}</h3>
          <p class="text-xs text-muted-foreground">{{ m.settings.backup.importHint }}</p>
          <input ref="fileInput" type="file" accept="application/json,.json" class="sr-only" :aria-label="m.settings.backup.chooseFile" @change="onFile" />
          <div class="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" :disabled="pending" @click="fileInput?.click()">
              <Upload class="mr-2 h-4 w-4" />{{ m.settings.backup.chooseFile }}
            </Button>
            <span v-if="fileName" class="break-all text-xs text-muted-foreground">{{ fileName }}</span>
          </div>
          <p v-if="fileError" role="alert" class="text-sm text-destructive">{{ fileError }}</p>
          <div v-if="summary" class="space-y-2 text-sm">
            <p>{{ summaryText }}</p>
            <p class="text-xs text-muted-foreground">{{ summary.sealed ? m.settings.backup.summarySealed : m.settings.backup.summaryNotSealed }}</p>
            <div v-if="summary.sealed">
              <Label class="text-xs" for="backup-import-passphrase">{{ m.settings.backup.importPassphrase }}</Label>
              <Input id="backup-import-passphrase" v-model="importPassphrase" type="password" autocomplete="off" class="mt-1" :disabled="pending" aria-describedby="backup-import-passphrase-hint" />
              <p id="backup-import-passphrase-hint" class="mt-1 text-xs text-muted-foreground">{{ m.settings.backup.importPassphraseHint }}</p>
            </div>
            <label class="flex items-start gap-2">
              <input v-model="overwrite" type="checkbox" class="mt-0.5 h-4 w-4" :disabled="pending" />
              <span>{{ m.settings.backup.overwrite }}<span class="block text-xs text-muted-foreground">{{ m.settings.backup.overwriteHint }}</span></span>
            </label>
            <Button size="sm" :disabled="pending" @click="doImport">{{ m.settings.backup.import }}</Button>
          </div>
          <ul v-if="resultLines.length" class="list-disc space-y-1 pl-5 text-sm" aria-live="polite">
            <li v-for="(line, i) in resultLines" :key="i" class="break-words">{{ line }}</li>
          </ul>
        </section>

        <div class="flex justify-end">
          <Button variant="outline" @click="close">{{ m.common.done }}</Button>
        </div>
      </div>
    </template>
  </Dialog>
</template>
