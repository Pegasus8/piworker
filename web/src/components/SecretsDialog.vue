<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { Button, Input, Label, Dialog } from '@/components/ui'
import { KeyRound, Trash2, Plus } from 'lucide-vue-next'
import { DEFAULT_SHORT_BELOW, isShortValue, shortSecretNotice, type SecretItem } from '@/lib/secrets'

const props = defineProps<{ open: boolean }>()
// changed: a secret was saved or deleted (the Agents page counts short ones).
const emit = defineEmits<{ 'update:open': [boolean]; changed: [] }>()

import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'

const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const api = useApi()
const items = ref<SecretItem[]>([])
const shortBelow = ref(DEFAULT_SHORT_BELOW)
const loading = ref(false)
const error = ref('')

const newName = ref('')
const newValue = ref('')

// An agent could confirm a guess of a short value (ADR-0021): marked with
// an icon and words, never color alone.
const notice = computed(() => shortSecretNotice(shortBelow.value))
const hasShort = computed(() => items.value.some(s => s.short))
const newValueShort = computed(() => !!newValue.value && isShortValue(newValue.value, shortBelow.value))

// Kept in script so the literal braces don't confuse the template parser.
const secretSyntax = '{{secret.NAME}}'

async function load() {
  loading.value = true
  error.value = ''
  try {
    const list = await api.getSecrets()
    items.value = list.secrets
    shortBelow.value = list.shortBelow
  } catch (e: any) {
    error.value = e?.response?.data?.error || 'Failed to load secrets'
  } finally {
    loading.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) load()
  }
)

async function add() {
  const name = newName.value.trim()
  if (!name || !newValue.value) return
  await run(async () => {
    await api.setSecret(name, newValue.value)
    newName.value = ''; newValue.value = ''
    emit('changed')
    await load()
  }, 'Secret saved.', 'Could not save the secret. Try again.')
}
async function remove(name: string) {
  if (pending.value || !await confirmation.value?.ask('Delete secret?', `Delete “${name}”? Flows that reference it may stop working. This cannot be undone.`, 'Delete secret')) return
  await run(async () => { await api.deleteSecret(name); emit('changed'); await load() }, 'Secret deleted.', 'Could not delete the secret. Try again.')
}
</script>

<template>
  <Dialog title="Manage secrets" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <KeyRound class="h-5 w-5 text-primary" />
          <div>
            <h2 class="text-lg font-semibold">Secrets</h2>
            <p class="text-sm text-muted-foreground">
              Reference these in node fields as <code>{{ secretSyntax }}</code>. Values are write-only.
            </p>
          </div>
        </div>

        <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>

        <OperationFeedback :message="feedback" @dismiss="feedback = null" />
        <!-- Existing secrets -->
        <div class="max-h-60 space-y-1 overflow-auto rounded border">
          <p v-if="!loading && !items.length" class="p-3 text-center text-sm text-muted-foreground">
            No secrets yet.
          </p>
          <div
            v-for="item in items"
            :key="item.name"
            class="flex justify-between gap-2 px-3 py-2 text-sm"
            :class="item.short ? 'items-start' : 'items-center'"
          >
            <div class="min-w-0 space-y-1">
              <span class="break-all font-mono">{{ item.name }}</span>
              <p v-if="item.short" class="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
                <component :is="notice.icon" class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                <span><strong>{{ notice.label }}:</strong> {{ notice.text }}</span>
              </p>
            </div>
            <Button variant="ghost" size="icon" class="shrink-0 text-destructive" :disabled="pending" :aria-label="`Delete secret ${item.name}`" @click="remove(item.name)">
              <Trash2 class="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <p v-if="hasShort" class="text-xs text-muted-foreground">
          To replace a value, add the secret again with the same name.
        </p>

        <!-- Add new -->
        <div class="space-y-2 border-t pt-3">
          <div class="grid grid-cols-2 gap-2">
            <div>
              <Label class="text-xs">Name</Label>
              <Input aria-label="Secret name" :disabled="pending" v-model="newName" placeholder="telegram_token" class="mt-1" />
            </div>
            <div>
              <Label class="text-xs">Value</Label>
              <Input aria-label="Secret value" :disabled="pending" v-model="newValue" type="password" placeholder="secret value" class="mt-1"
                :aria-describedby="newValueShort ? 'secret-value-short' : undefined" />
            </div>
          </div>
          <!-- Always present, filled only while the value is short: a live
               region announces the hint when it appears, not only on focus. -->
          <div id="secret-value-short" aria-live="polite">
            <p v-if="newValueShort" class="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
              <component :is="notice.icon" class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              <span><strong>{{ notice.label }}:</strong> {{ notice.text }}</span>
            </p>
          </div>
          <Button size="sm" class="w-full" :disabled="pending || !newName.trim() || !newValue" @click="add">
            <Plus class="mr-2 h-4 w-4" /> Add secret
          </Button>
        </div>

        <div class="flex justify-end">
          <Button variant="outline" @click="close">Done</Button>
        </div>
      </div>
      <ConfirmDialog ref="confirmation" />
    </template>
  </Dialog>
</template>
