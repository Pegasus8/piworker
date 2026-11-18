<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { useFeedback } from '@/composables/useFeedback'
import { Button, Dialog, Input, Label, Select, Textarea } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { messages as m } from '@/i18n'
const props = defineProps<{ open: boolean; flowId?: string }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()
const api = useApi()
const scope = ref('global')
const scopeSelect = ref<InstanceType<typeof Select>>()
const selectedFlow = computed(() => scope.value === 'flow' ? props.flowId : undefined)
const title = computed(() => scope.value === 'flow' ? m.value.settings.variables.flowTitle : m.value.settings.variables.globalTitle)
let loadRequest = 0
const variables = ref<Record<string, unknown>>({})
const loading = ref(false)
const error = ref('')
const name = ref(''), value = ref(''), format = ref('text')
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const validation = computed(() => {
 if (!/^[A-Za-z0-9_.-]+$/.test(name.value)) return m.value.settings.variables.nameRule
 if (format.value === 'json') { try { JSON.parse(value.value) } catch { return m.value.settings.variables.invalidJson } }
 return ''
})
async function load() {
 const request = ++loadRequest
 loading.value = true; error.value = ''; variables.value = {}
 try { const values = await api.getVariables(selectedFlow.value); if (request === loadRequest) variables.value = values }
 catch { if (request === loadRequest) error.value = m.value.settings.variables.loadFailed }
 finally { if (request === loadRequest) loading.value = false }
}
watch(() => props.open, open => { if (open) { scope.value = props.flowId ? 'flow' : 'global'; name.value = ''; value.value = ''; void load(); feedback.value = null } else { loadRequest++ } })
async function changeScope(next: string | number) {
 if (pending.value) return
 if ((name.value || value.value) && !await confirmation.value?.ask(m.value.settings.variables.discardTitle, m.value.settings.variables.discardText, m.value.settings.variables.discardConfirm)) {
  // Native selects change before the confirmation; restore the displayed value.
  if (scopeSelect.value) (scopeSelect.value.$el as HTMLSelectElement).value = scope.value
  return
 }
 scope.value = String(next); name.value = ''; value.value = ''; feedback.value = null; void load()
}
function edit(key: string) {
 name.value = key
 const current = variables.value[key]
 format.value = typeof current === 'string' ? 'text' : 'json'
 value.value = typeof current === 'string' ? current : JSON.stringify(current, null, 2)
}
async function save() {
 if (validation.value || pending.value) return
 const parsed = format.value === 'json' ? JSON.parse(value.value) : value.value
 if (Object.prototype.hasOwnProperty.call(variables.value, name.value) && !await confirmation.value?.ask(m.value.settings.variables.replaceTitle(scope.value === 'flow'), m.value.settings.variables.replaceText(scope.value === 'flow', name.value), m.value.settings.variables.replaceConfirm)) return
 await run(async () => { await api.setVariable(name.value, parsed, selectedFlow.value); variables.value = { ...variables.value, [name.value]: parsed }; name.value = ''; value.value = '' }, m.value.settings.variables.saved, m.value.settings.variables.saveFailed)
}
async function remove(key: string) {
 if (pending.value || !await confirmation.value?.ask(m.value.settings.variables.deleteTitle(scope.value === 'flow'), m.value.settings.variables.deleteText(scope.value === 'flow', key), m.value.settings.variables.deleteConfirm)) return
 await run(async () => { await api.deleteVariable(key, selectedFlow.value); delete variables.value[key] }, m.value.settings.variables.deleted, m.value.settings.variables.deleteFailed)
}
</script>
<template>
 <Dialog :open="open" :title="title" @update:open="!pending && emit('update:open', $event)">
  <h2 class="text-lg font-semibold">{{ title }}</h2>
  <div v-if="flowId" class="mt-3"><Label for="variable-scope">{{ m.settings.variables.scope }}</Label><Select ref="scopeSelect" id="variable-scope" :model-value="scope" :disabled="pending" :options="[{value:'flow',label:m.settings.variables.thisFlow},{value:'global',label:m.settings.variables.globalAll}]" @update:model-value="changeScope" /></div>
  <p v-else class="mt-2 text-xs text-muted-foreground">{{ m.settings.variables.openSaved }}</p>
  <p class="mt-2 text-sm text-muted-foreground">{{ scope === 'flow' ? m.settings.variables.flowOnly : m.settings.variables.shared }} {{ m.settings.variables.persistence }}</p>
  <details class="mt-3 rounded-lg border p-3 text-sm">
   <summary class="cursor-pointer font-medium text-primary">{{ m.settings.variables.howTo }}</summary>
   <div class="mt-3 space-y-2 text-muted-foreground">
    <p>{{ m.settings.variables.help1 }}</p>
    <p>{{ m.settings.variables.help2[0] }}<code>vars["threshold"]</code>{{ m.settings.variables.help2[1] }}<code>flowVars["threshold"]</code>{{ m.settings.variables.help2[2] }}</p>
    <p>{{ m.settings.variables.help3[0] }}<code v-pre>{{vars.threshold}}</code>{{ m.settings.variables.help3[1] }}<code v-pre>{{flowVars.threshold}}</code>{{ m.settings.variables.help3[2] }}</p>
    <p>{{ m.settings.variables.help4 }}</p>
    <p>{{ m.settings.variables.help5 }}</p>
    <p>{{ m.settings.variables.help6 }}</p>
    <p>{{ m.settings.variables.help7 }}</p>
   </div>
  </details>
  <OperationFeedback :message="feedback" class="mt-3" />
  <p v-if="loading" role="status" class="my-3 text-sm">{{ m.settings.variables.loading }}</p>
  <p v-if="error" role="alert" class="my-3 text-sm text-destructive">{{ error }} <Button variant="outline" size="sm" @click="load">{{ m.common.retry }}</Button></p>
  <ul class="my-4 max-h-48 divide-y overflow-auto rounded-lg border">
   <li v-for="key in Object.keys(variables).sort()" :key="key" class="flex items-start gap-2 p-3 text-sm">
    <div class="min-w-0 flex-1"><strong class="break-all">{{ key }}</strong><pre class="max-h-20 overflow-auto whitespace-pre-wrap break-all text-xs text-muted-foreground">{{ JSON.stringify(variables[key]) }}</pre></div>
    <Button size="sm" variant="ghost" :disabled="pending" :aria-label="m.settings.variables.editLabel(key)" @click="edit(key)">{{ m.settings.variables.edit }}</Button>
    <Button size="sm" variant="ghost" :disabled="pending" :aria-label="m.settings.variables.deleteLabel(key)" @click="remove(key)">{{ m.common.delete }}</Button>
   </li>
  </ul>
  <p v-if="!loading && !error && !Object.keys(variables).length" class="mb-3 text-sm text-muted-foreground">{{ m.settings.variables.empty }}</p>
  <fieldset :disabled="pending" class="space-y-3">
   <div><Label for="variable-name">{{ m.settings.variables.name }}</Label><Input id="variable-name" v-model="name" placeholder="threshold" /></div>
   <div><Label for="variable-format">{{ m.settings.variables.format }}</Label><Select id="variable-format" v-model="format" :options="[{ value: 'text', label: m.settings.variables.text }, { value: 'json', label: m.settings.variables.json }]" /></div>
   <div><Label for="variable-value">{{ m.settings.variables.value }}</Label><Textarea id="variable-value" v-model="value" /></div>
  </fieldset>
  <p v-if="name && validation" class="mt-2 text-sm text-destructive">{{ validation }}</p>
  <div class="mt-4 flex justify-end gap-2"><Button variant="outline" :disabled="pending" @click="emit('update:open',false)">{{ m.common.done }}</Button><Button :disabled="pending || loading || !!error || !!validation" @click="save">{{ m.settings.variables.save }}</Button></div>
  <ConfirmDialog ref="confirmation" />
 </Dialog>
</template>
