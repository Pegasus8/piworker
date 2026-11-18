<script setup lang="ts">
// Restore a backup file (ADR-0032): a plain archive or an encrypted (age)
// copy. On the setup page of a fresh installation the setup code
// authorises it; in the editor the account password does. PiWorker then
// restarts to apply it.
import { computed, ref } from 'vue'
import { Button, Input, Label } from '@/components/ui'
import { Upload } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useBackupsStore } from '@/stores/backups'
import { apiErrorMessage } from '@/stores/agent'
import { messages as m } from '@/i18n'
import { backupFileError, fileToBase64, isEncryptedHead } from '@/lib/backups'

const props = defineProps<{ mode: 'setup' | 'editor' }>()
// restarting: the restore was accepted; restored: PiWorker is back (or
// false: it did not come back in time).
const emit = defineEmits<{ restarting: []; restored: [back: boolean] }>()

const auth = useAuthStore()
const store = useBackupsStore()
const fileInput = ref<HTMLInputElement | null>(null)
const file = ref<File | null>(null)
const encrypted = ref(false)
const fileError = ref('')
const passphrase = ref('')
const secret = ref('') // setup code or account password
const error = ref('')
const pending = ref(false)
let before = ''

const needsSecret = computed(() => props.mode === 'setup' || auth.authEnabled)
const canRestore = computed(() => !!file.value && !fileError.value && !pending.value &&
  (!encrypted.value || !!passphrase.value) && (!needsSecret.value || !!secret.value.trim()))

async function onFile(event: Event) {
  const f = (event.target as HTMLInputElement).files?.[0] ?? null
  file.value = f
  fileError.value = error.value = ''
  encrypted.value = false
  if (!f) return
  const head = new Uint8Array(await f.slice(0, 32).arrayBuffer())
  fileError.value = backupFileError(head, f.size)
  encrypted.value = isEncryptedHead(head)
}

async function submit() {
  if (!canRestore.value || !file.value) return
  pending.value = true
  error.value = ''
  try {
    before = await store.instance()
    const data = await fileToBase64(file.value)
    const phrase = encrypted.value ? passphrase.value : ''
    if (props.mode === 'setup') await store.setupRestore(data, phrase, secret.value.trim())
    else await store.upload(data, phrase, secret.value)
  } catch (e) {
    error.value = apiErrorMessage(e, m.value.backups.restoreFailed)
    pending.value = false
    return
  }
  emit('restarting')
  emit('restored', await store.waitForRestart(before))
}
</script>

<template>
  <form class="space-y-3" @submit.prevent="submit">
    <input ref="fileInput" type="file" accept=".gz,.age,application/gzip,application/octet-stream" class="sr-only" :aria-label="m.backups.chooseFile" @change="onFile" />
    <div class="flex flex-wrap items-center gap-2">
      <Button type="button" variant="outline" size="sm" :disabled="pending" @click="fileInput?.click()">
        <Upload class="mr-2 h-4 w-4" aria-hidden="true" />{{ m.backups.chooseFile }}
      </Button>
      <span v-if="file" class="break-all text-xs text-muted-foreground">{{ file.name }}</span>
    </div>
    <p v-if="fileError" role="alert" class="text-sm text-destructive">{{ fileError }}</p>
    <div v-if="encrypted">
      <Label :for="`restore-file-passphrase-${mode}`">{{ m.backups.filePassphrase }}</Label>
      <Input :id="`restore-file-passphrase-${mode}`" v-model="passphrase" type="password" autocomplete="off" class="mt-1" :disabled="pending" :aria-describedby="`restore-file-passphrase-hint-${mode}`" />
      <p :id="`restore-file-passphrase-hint-${mode}`" class="mt-1 text-xs text-muted-foreground">{{ m.backups.fileEncrypted }}</p>
    </div>
    <div v-if="needsSecret">
      <Label :for="`restore-file-secret-${mode}`">{{ mode === 'setup' ? m.auth.setupCode : m.backups.restorePassword }}</Label>
      <Input :id="`restore-file-secret-${mode}`" v-model="secret" :type="mode === 'setup' ? 'text' : 'password'" :autocomplete="mode === 'setup' ? 'off' : 'current-password'" class="mt-1" :disabled="pending" />
    </div>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
    <Button type="submit" :variant="mode === 'setup' ? 'default' : 'destructive'" :class="mode === 'setup' ? 'w-full' : ''" :disabled="!canRestore">
      {{ m.backups.fileRestore }}
    </Button>
  </form>
</template>
