<script setup lang="ts">
// The named LLM connections (docs/design/llm.md §1.1, §6, §8): what the LLM
// nodes call. Keys are write-only; a test lists the models without
// spending tokens; a delete is refused while a running flow uses the
// connection, and the refusal names those flows.
import { computed, nextTick, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { Button, Input, Label, Dialog, Badge, Select } from '@/components/ui'
import { Sparkles, Pencil, Trash2, Plus, CheckCircle2, AlertCircle, AlertTriangle, Loader2 } from 'lucide-vue-next'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import SecretKeyInput from '@/components/llm/SecretKeyInput.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useApi } from '@/composables/useApi'
import { useLLMStore } from '@/stores/llm'
import { messages as m, formatNumber } from '@/i18n'
import { LLM_PRESETS, conflictFlows, connectionKind, llmConnectionRequest, llmEndpoint } from '@/lib/llm'
import { apiErrorMessage } from '@/lib/apiError'
import { modelSuggestion } from '@/lib/suggestions'
import type { FlowRef } from '@/lib/zigbee'
import type { LLMConnection, LLMConnectionTest, LLMProvider } from '@/types'
import {
  applyPreset, blankConnectionForm, connectionFormError, connectionFormFrom, modelSuggestions, usageSummary,
  type ConnectionFormState, type PresetChoice, type UsageLevel
} from '@/components/llm/connections'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const store = useLLMStore()
const api = useApi()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const loading = ref(false)
const error = ref('')
// The running flows a refused delete named.
const blocked = ref<FlowRef[]>([])
// Secret names for the key's references; none when they cannot be read.
const secretNames = ref<string[]>([])

const form = ref<ConnectionFormState>(blankConnectionForm())
// The connection being edited (its name cannot change), or null for a new one.
const editing = ref<LLMConnection | null>(null)
const attempted = ref(false)
const formError = computed(() => connectionFormError(form.value, editing.value, store.connections.map(c => c.name)))
const shownError = computed(() => attempted.value ? formError.value : '')
const testing = ref(false)
const testResult = ref<LLMConnectionTest | null>(null)
let testRun = 0
// Kept in script so the literal braces don't confuse the template parser.
const secretSyntax = '{{secret.NAME}}'
const customExample = 'http://192.168.1.20:8000/v1'

const presetOptions = computed(() => [
  ...LLM_PRESETS.map(p => ({ value: p.id, label: p.label })),
  { value: 'custom', label: m.value.llm.connections.custom }
])
const providerOptions = computed(() => (['anthropic', 'openai'] as const).map(p => ({ value: p, label: m.value.editor.llm.providers[p] })))
const preset = computed(() => LLM_PRESETS.find(p => p.id === form.value.preset))
// Each listed connection with today's usage against its limit.
const rows = computed(() => store.connections.map(item => ({ item, usage: usageSummary(item) })))

const baseUrlPlaceholder = computed(() => form.value.preset === 'custom' ? customExample : llmEndpoint({ provider: form.value.provider, baseUrl: '' }))
const baseUrlHint = computed(() => {
  const t = m.value.llm.connections
  if (!preset.value) return t.baseUrlCustom
  if (!preset.value.baseUrl) return t.baseUrlDefault(llmEndpoint({ provider: preset.value.provider, baseUrl: '' }))
  return preset.value.needsKey ? '' : t.baseUrlLocal
})
const keyPlaceholder = computed(() => {
  const t = m.value.llm.connections
  if (editing.value?.hasApiKey && !form.value.clearApiKey) return t.keyKeep
  if (form.value.provider === 'anthropic') return t.keyRequired
  return !preset.value || !preset.value.needsKey ? t.keyOptional : ''
})
// What the model field suggests: the last test's models, then the stored connection's.
const models = computed(() => modelSuggestions(testResult.value?.models, editing.value ? store.models[editing.value.name] : undefined).map(modelSuggestion))
const testModels = computed(() => testResult.value?.ok ? testResult.value.models ?? [] : [])

async function load() {
  loading.value = true
  error.value = ''
  try {
    await store.fetchConnections()
  } catch (e) {
    error.value = apiErrorMessage(e, m.value.llm.connections.loadFailed)
  } finally {
    loading.value = false
  }
}

async function loadSecrets() {
  try {
    secretNames.value = (await api.getSecrets()).secrets.map(s => s.name)
  } catch {
    secretNames.value = []
  }
}

function reset() {
  form.value = blankConnectionForm()
  editing.value = null
  attempted.value = false
  testResult.value = null
}

watch(() => props.open, open => {
  if (!open) return
  reset()
  feedback.value = null
  blocked.value = []
  void load()
  void loadSecrets()
}, { immediate: true })

function dismiss() {
  feedback.value = null
  blocked.value = []
}

// Editing never shows the stored key: empty keeps it. The stored
// connection's models are suggested (listed once, cached by the store).
async function edit(item: LLMConnection) {
  editing.value = item
  form.value = connectionFormFrom(item)
  attempted.value = false
  testResult.value = null
  dismiss()
  void store.loadModels(item.name)
  await nextTick()
  document.getElementById('llm-conn-preset')?.focus()
}

function choosePreset(value: string) {
  form.value = applyPreset(form.value, value as PresetChoice)
}

// Settings that change what a test lists: its result no longer applies
// (the model and the limit do not change it).
watch(() => [form.value.name, form.value.provider, form.value.baseUrl, form.value.apiKey, form.value.clearApiKey], () => {
  testRun++
  testResult.value = null
  testing.value = false
})

async function save() {
  attempted.value = true
  if (formError.value) return
  const name = form.value.name.trim()
  const t = m.value.llm.connections
  blocked.value = []
  const ok = await run(() => store.saveConnection(name, llmConnectionRequest(form.value)), t.saved(name), e => apiErrorMessage(e, t.saveFailed))
  if (ok) reset()
}

async function test() {
  attempted.value = true
  if (formError.value || testing.value) return
  const current = ++testRun
  testing.value = true
  testResult.value = null
  let result: LLMConnectionTest
  try {
    result = await store.testConnection(form.value.name.trim(), llmConnectionRequest(form.value))
  } catch (e) {
    result = { ok: false, error: apiErrorMessage(e, m.value.editor.llm.testFailed) }
  }
  if (current !== testRun) return
  testResult.value = result
  testing.value = false
}

async function remove(name: string) {
  const t = m.value.llm.connections
  if (pending.value || !await confirmation.value?.ask(t.deleteTitle, t.deleteText(name), t.deleteConfirm)) return
  blocked.value = []
  let refused: FlowRef[] = []
  const ok = await run(() => store.deleteConnection(name), t.deleted, e => {
    refused = conflictFlows(e)
    return apiErrorMessage(e, t.deleteFailed)
  })
  if (!ok) blocked.value = refused
  else if (editing.value?.name === name) reset()
}

function setLimit(value: string | number) {
  form.value.dailyTokenLimit = typeof value === 'number' ? value : undefined
}

const levelText: Record<UsageLevel, string> = {
  unlimited: 'text-muted-foreground',
  ok: 'text-muted-foreground',
  high: 'text-amber-700 dark:text-amber-400',
  exhausted: 'text-destructive'
}
const levelBar: Record<UsageLevel, string> = { unlimited: 'bg-primary', ok: 'bg-primary', high: 'bg-amber-500', exhausted: 'bg-destructive' }
const providerOf = (value: string) => value as LLMProvider
</script>

<template>
  <Dialog :title="m.llm.connections.title" class="max-w-2xl" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-start gap-2">
          <Sparkles class="mt-1 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div class="space-y-1">
            <h2 class="text-lg font-semibold">{{ m.llm.connections.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.llm.connections.intro }}</p>
            <p class="text-xs text-muted-foreground">{{ m.llm.connections.privacy }}</p>
          </div>
        </div>

        <p v-if="error" role="alert" class="text-sm text-destructive">
          {{ error }} <Button size="sm" variant="outline" @click="load">{{ m.common.retry }}</Button>
        </p>
        <div class="space-y-2">
          <OperationFeedback :message="feedback" @dismiss="dismiss" />
          <!-- A refused delete: the flows that use the connection. -->
          <div v-if="blocked.length" class="rounded border border-destructive/40 px-3 py-2 text-sm">
            <p class="font-medium">{{ m.llm.connections.usedBy }}</p>
            <ul class="mt-1 list-disc space-y-0.5 pl-5">
              <li v-for="flow in blocked" :key="flow.id" class="break-words">
                <RouterLink :to="`/editor/${encodeURIComponent(flow.id)}`" class="text-primary underline-offset-4 hover:underline" :aria-label="m.llm.connections.openFlow(flow.name)" @click="emit('update:open', false)">{{ flow.name }}</RouterLink>
              </li>
            </ul>
          </div>
        </div>

        <!-- Existing connections -->
        <ul class="max-h-72 divide-y overflow-auto rounded border" :aria-label="m.llm.connections.listLabel">
          <li v-if="loading && !store.connections.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.common.loading }}</li>
          <li v-else-if="!store.connections.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.llm.connections.empty }}</li>
          <li v-for="{ item, usage } in rows" :key="item.name" class="flex items-start justify-between gap-2 px-3 py-2 text-sm">
            <div class="min-w-0 flex-1 space-y-1">
              <p class="flex flex-wrap items-center gap-2">
                <span class="break-all font-mono font-medium">{{ item.name }}</span>
                <Badge v-if="item.hasApiKey" variant="secondary" class="text-[10px]">{{ m.llm.connections.keyStored }}</Badge>
              </p>
              <p class="break-all text-xs text-muted-foreground">
                {{ connectionKind(item) }} · {{ llmEndpoint(item) }} · {{ item.model ? m.llm.connections.model(item.model) : m.llm.connections.noModel }}
              </p>
              <p class="text-xs" :class="levelText[usage.level]">{{ usage.text }}</p>
              <!-- The text says it; the bar only shows it at a glance. -->
              <div v-if="usage.level !== 'unlimited'" class="h-1.5 w-full max-w-xs overflow-hidden rounded-full bg-muted" aria-hidden="true">
                <div class="h-full rounded-full" :class="levelBar[usage.level]" :style="{ width: `${usage.percent}%` }" />
              </div>
              <p v-if="usage.notice" class="flex items-start gap-1 text-xs" :class="levelText[usage.level]">
                <AlertTriangle class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />{{ usage.notice }}
              </p>
            </div>
            <div class="flex shrink-0 gap-1">
              <Button variant="ghost" size="icon" :disabled="pending" :aria-label="m.llm.connections.editLabel(item.name)" :title="m.llm.connections.edit" @click="edit(item)">
                <Pencil class="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon" class="text-destructive" :disabled="pending" :aria-label="m.llm.connections.deleteLabel(item.name)" :title="m.common.delete" @click="remove(item.name)">
                <Trash2 class="h-3.5 w-3.5" />
              </Button>
            </div>
          </li>
        </ul>

        <!-- Add or edit -->
        <form class="space-y-3 border-t pt-3" novalidate @submit.prevent="save">
          <h3 class="text-sm font-semibold">{{ editing ? m.llm.connections.editTitle(editing.name) : m.llm.connections.newTitle }}</h3>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <Label for="llm-conn-name" class="text-xs">{{ m.llm.connections.name }}</Label>
              <Input id="llm-conn-name" v-model="form.name" :disabled="pending || !!editing" placeholder="claude" class="mt-1" autocomplete="off" spellcheck="false"
                aria-describedby="llm-conn-name-help" />
              <p id="llm-conn-name-help" class="mt-1 text-xs text-muted-foreground">{{ editing ? m.llm.connections.nameFixed : m.llm.connections.nameHint }}</p>
            </div>
            <div>
              <Label for="llm-conn-preset" class="text-xs">{{ m.llm.connections.preset }}</Label>
              <Select id="llm-conn-preset" :model-value="form.preset" :options="presetOptions" :disabled="pending" class="mt-1" @update:model-value="choosePreset" />
            </div>
            <div v-if="form.preset === 'custom'" class="sm:col-span-2">
              <Label for="llm-conn-provider" class="text-xs">{{ m.llm.connections.provider }}</Label>
              <Select id="llm-conn-provider" :model-value="form.provider" :options="providerOptions" :disabled="pending" class="mt-1"
                aria-describedby="llm-conn-provider-help" @update:model-value="(v: string) => form.provider = providerOf(v)" />
              <p id="llm-conn-provider-help" class="mt-1 text-xs text-muted-foreground">{{ m.llm.connections.providerHint }}</p>
            </div>
            <div class="sm:col-span-2">
              <Label for="llm-conn-url" class="text-xs">{{ m.llm.connections.baseUrl }}</Label>
              <Input id="llm-conn-url" v-model="form.baseUrl" :disabled="pending" :placeholder="baseUrlPlaceholder" class="mt-1" autocomplete="off" spellcheck="false"
                inputmode="url" :aria-describedby="baseUrlHint ? 'llm-conn-url-help' : undefined" />
              <p v-if="baseUrlHint" id="llm-conn-url-help" class="mt-1 break-words text-xs text-muted-foreground">{{ baseUrlHint }}</p>
            </div>
            <div class="sm:col-span-2">
              <Label for="llm-conn-key" class="text-xs">{{ m.llm.connections.apiKey }}</Label>
              <SecretKeyInput id="llm-conn-key" v-model="form.apiKey" :secrets="secretNames" :disabled="pending || form.clearApiKey" :placeholder="keyPlaceholder"
                describedby="llm-conn-key-help" class="mt-1" />
              <label v-if="editing?.hasApiKey" class="mt-1 flex items-center gap-2 text-xs">
                <input v-model="form.clearApiKey" type="checkbox" :disabled="pending" class="h-3.5 w-3.5" />
                {{ m.llm.connections.clearKey }}
              </label>
              <p id="llm-conn-key-help" class="mt-1 text-xs text-muted-foreground">{{ m.llm.connections.keyHint[0] }}<code>{{ secretSyntax }}</code>{{ m.llm.connections.keyHint[1] }}</p>
            </div>
            <div>
              <Label for="llm-conn-model" class="text-xs">{{ m.llm.connections.defaultModel }}</Label>
              <Input id="llm-conn-model" v-model="form.model" :disabled="pending" :placeholder="m.llm.connections.modelPlaceholder" class="mt-1" autocomplete="off" spellcheck="false"
                list="llm-conn-models" aria-describedby="llm-conn-model-help" />
              <datalist id="llm-conn-models">
                <option v-for="s in models" :key="s.value" :value="s.value" :label="s.label" />
              </datalist>
              <p id="llm-conn-model-help" class="mt-1 text-xs text-muted-foreground">{{ m.llm.connections.modelHint }}</p>
            </div>
            <div>
              <Label for="llm-conn-limit" class="text-xs">{{ m.llm.connections.dailyLimit }}</Label>
              <Input id="llm-conn-limit" type="number" min="0" step="1" inputmode="numeric" :model-value="form.dailyTokenLimit ?? ''" :disabled="pending"
                :placeholder="m.llm.connections.noLimit" class="mt-1" aria-describedby="llm-conn-limit-help" @update:model-value="setLimit" />
              <p id="llm-conn-limit-help" class="mt-1 text-xs text-muted-foreground">{{ m.llm.connections.dailyLimitHint }}</p>
            </div>
          </div>

          <p v-if="shownError" role="alert" class="text-sm text-destructive">{{ shownError }}</p>

          <!-- The test's result: announced when it arrives. -->
          <div aria-live="polite" class="space-y-2">
            <p v-if="testing" class="flex items-center gap-1.5 text-sm text-muted-foreground"><Loader2 class="h-4 w-4 animate-spin" aria-hidden="true" />{{ m.llm.connections.testing }}</p>
            <template v-else-if="testResult?.ok">
              <p class="flex items-center gap-1.5 text-sm text-primary">
                <CheckCircle2 class="h-4 w-4 shrink-0" aria-hidden="true" />{{ m.llm.connections.testOk(testModels.length, formatNumber(testModels.length)) }}
              </p>
              <!-- The models found: one sets the default model. -->
              <details v-if="testModels.length" class="text-xs">
                <summary class="cursor-pointer text-muted-foreground">{{ m.llm.connections.modelsFound(testModels.length, formatNumber(testModels.length)) }}</summary>
                <ul class="mt-2 flex max-h-40 flex-wrap gap-1 overflow-auto" :aria-label="m.llm.connections.modelsLabel">
                  <li v-for="model in testModels" :key="model.id">
                    <button type="button" class="rounded border px-2 py-0.5 font-mono hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      :class="form.model === model.id ? 'border-primary bg-primary/10 text-primary' : ''" :aria-pressed="form.model === model.id"
                      :aria-label="m.llm.connections.useModel(model.id)" :title="model.name || model.id" :disabled="pending" @click="form.model = model.id">{{ model.id }}</button>
                  </li>
                </ul>
              </details>
            </template>
            <p v-else-if="testResult" class="flex items-start gap-1.5 break-words text-sm text-destructive">
              <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><span>{{ m.llm.connections.testError(testResult.error ?? '') }}</span>
            </p>
          </div>

          <div class="flex flex-wrap items-center justify-end gap-2">
            <p class="mr-auto text-xs text-muted-foreground">{{ m.llm.connections.testCost }}</p>
            <Button v-if="editing" type="button" variant="ghost" size="sm" :disabled="pending" @click="reset()">{{ m.llm.connections.cancelEdit }}</Button>
            <Button type="button" variant="outline" size="sm" :disabled="pending || testing" @click="test">{{ m.llm.connections.test }}</Button>
            <Button type="submit" size="sm" :disabled="pending">
              <Plus v-if="!editing" class="mr-2 h-4 w-4" aria-hidden="true" />
              {{ editing ? m.llm.connections.save : m.llm.connections.add }}
            </Button>
          </div>
        </form>

        <div class="flex justify-end border-t pt-3">
          <Button variant="outline" @click="close">{{ m.common.done }}</Button>
        </div>
      </div>
      <ConfirmDialog ref="confirmation" />
    </template>
  </Dialog>
</template>
