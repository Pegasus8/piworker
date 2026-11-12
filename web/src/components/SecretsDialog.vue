<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { Button, Input, Label, Dialog } from '@/components/ui'
import { KeyRound, Trash2, Plus } from 'lucide-vue-next'
import { messages as m } from '@/i18n'
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
    error.value = e?.response?.data?.error || m.value.settings.secrets.loadFailed
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
  }, m.value.settings.secrets.saved, m.value.settings.secrets.saveFailed)
}
async function remove(name: string) {
  if (pending.value || !await confirmation.value?.ask(m.value.settings.secrets.deleteTitle, m.value.settings.secrets.deleteText(name), m.value.settings.secrets.deleteConfirm)) return
  await run(async () => { await api.deleteSecret(name); emit('changed'); await load() }, m.value.settings.secrets.deleted, m.value.settings.secrets.deleteFailed)
}
</script>

<template>
  <Dialog :title="m.settings.secrets.dialogTitle" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <KeyRound class="h-5 w-5 text-primary" />
          <div>
            <h2 class="text-lg font-semibold">{{ m.settings.secrets.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.settings.secrets.intro[0] }}<code>{{ secretSyntax }}</code>{{ m.settings.secrets.intro[1] }}</p>
          </div>
        </div>

        <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>

        <OperationFeedback :message="feedback" @dismiss="feedback = null" />
        <!-- Existing secrets -->
        <div class="max-h-60 space-y-1 overflow-auto rounded border">
          <p v-if="!loading && !items.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.settings.secrets.empty }}</p>
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
            <Button variant="ghost" size="icon" class="shrink-0 text-destructive" :disabled="pending" :aria-label="m.settings.secrets.deleteLabel(item.name)" @click="remove(item.name)">
              <Trash2 class="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <p v-if="hasShort" class="text-xs text-muted-foreground">{{ m.settings.secrets.replaceHint }}</p>

        <!-- Add new -->
        <div class="space-y-2 border-t pt-3">
          <div class="grid grid-cols-2 gap-2">
            <div>
              <Label class="text-xs">{{ m.settings.secrets.name }}</Label>
              <Input :aria-label="m.settings.secrets.nameLabel" :disabled="pending" v-model="newName" placeholder="telegram_token" class="mt-1" />
            </div>
            <div>
              <Label class="text-xs">{{ m.settings.secrets.value }}</Label>
              <Input :aria-label="m.settings.secrets.valueLabel" :disabled="pending" v-model="newValue" type="password" :placeholder="m.settings.secrets.valuePlaceholder" class="mt-1"
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
            <Plus class="mr-2 h-4 w-4" /> {{ m.settings.secrets.add }}
          </Button>
        </div>

        <div class="flex justify-end">
          <Button variant="outline" @click="close">{{ m.common.done }}</Button>
        </div>
      </div>
      <ConfirmDialog ref="confirmation" />
    </template>
  </Dialog>
</template>
