<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { useFeedback } from '@/composables/useFeedback'
import { Button, Dialog, Input, Label, Select, Textarea } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
const props = defineProps<{ open: boolean; flowId?: string }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()
const api = useApi()
const scope = ref('global')
const scopeSelect = ref<InstanceType<typeof Select>>()
const selectedFlow = computed(() => scope.value === 'flow' ? props.flowId : undefined)
const title = computed(() => scope.value === 'flow' ? 'Flow variables' : 'Global variables')
let loadRequest = 0
const variables = ref<Record<string, unknown>>({})
const loading = ref(false)
const error = ref('')
const name = ref(''), value = ref(''), format = ref('text')
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const validation = computed(() => {
 if (!/^[A-Za-z0-9_.-]+$/.test(name.value)) return 'Use letters, numbers, dots, underscores or hyphens in the name.'
 if (format.value === 'json') { try { JSON.parse(value.value) } catch { return 'Enter valid JSON.' } }
 return ''
})
async function load() {
 const request = ++loadRequest
 loading.value = true; error.value = ''; variables.value = {}
 try { const values = await api.getVariables(selectedFlow.value); if (request === loadRequest) variables.value = values }
 catch { if (request === loadRequest) error.value = 'Could not load variables. Try again.' }
 finally { if (request === loadRequest) loading.value = false }
}
watch(() => props.open, open => { if (open) { scope.value = props.flowId ? 'flow' : 'global'; name.value = ''; value.value = ''; void load(); feedback.value = null } else { loadRequest++ } })
async function changeScope(next: string | number) {
 if (pending.value) return
 if ((name.value || value.value) && !await confirmation.value?.ask('Discard variable draft?', 'Changing scope clears the unsaved variable.', 'Discard draft')) {
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
 if (Object.prototype.hasOwnProperty.call(variables.value, name.value) && !await confirmation.value?.ask(`Replace ${scope.value} variable?`, `${scope.value === 'flow' ? 'This flow' : 'All flows'} will use the new value of “${name.value}” on the next read.`, 'Replace variable')) return
 await run(async () => { await api.setVariable(name.value, parsed, selectedFlow.value); variables.value = { ...variables.value, [name.value]: parsed }; name.value = ''; value.value = '' }, 'Variable saved.', 'Could not save the variable. Your input is still here. Try again.')
}
async function remove(key: string) {
 if (pending.value || !await confirmation.value?.ask(`Delete ${scope.value} variable?`, `${scope.value === 'flow' ? 'This flow' : 'Flows'} reading “${key}” will receive their default or a missing value.`, 'Delete variable')) return
 await run(async () => { await api.deleteVariable(key, selectedFlow.value); delete variables.value[key] }, 'Variable deleted.', 'Could not delete the variable. Try again.')
}
</script>
<template>
 <Dialog :open="open" :title="title" @update:open="!pending && emit('update:open', $event)">
  <h2 class="text-lg font-semibold">{{ title }}</h2>
  <div v-if="flowId" class="mt-3"><Label for="variable-scope">Variable scope</Label><Select ref="scopeSelect" id="variable-scope" :model-value="scope" :disabled="pending" :options="[{value:'flow',label:'This flow'},{value:'global',label:'Global · all flows'}]" @update:model-value="changeScope" /></div>
  <p v-else class="mt-2 text-xs text-muted-foreground">Open a saved flow in the editor to manage its own variables.</p>
  <p class="mt-2 text-sm text-muted-foreground">{{ scope === 'flow' ? 'Only this flow can read or change these values.' : 'Shared by all flows.' }} Saved across restarts. Changes affect running flows on their next read. Use Secrets for credentials.</p>
  <details class="mt-3 rounded-lg border p-3 text-sm">
   <summary class="cursor-pointer font-medium text-primary">How to use variables</summary>
   <div class="mt-3 space-y-2 text-muted-foreground">
    <p>Choose Global for shared settings, or This flow for independent configuration and state. The same name can exist in both scopes; references never fall back to another scope.</p>
    <p>In expressions: <code>vars["threshold"]</code> (global) or <code>flowVars["threshold"]</code> (this flow).</p>
    <p>In template fields: <code v-pre>{{vars.threshold}}</code> or <code v-pre>{{flowVars.threshold}}</code>.</p>
    <p>In Get Variable / Set Variable nodes, select the scope and enter the name. Ctrl+Space opens compatible references in the editor.</p>
    <p>Text stays text. Choose JSON for numbers, booleans, objects, arrays or null. Missing template values become empty text; Get Variable uses its default; expressions may fail on missing values.</p>
    <p>Node previews use copies and never save variable changes. Flow values survive stops and restarts, but are removed when the flow is deleted. Duplicating or exporting a flow does not copy its saved variable values; configure them for the new flow.</p>
    <p>Variables are visible data, not credentials. Keep passwords and tokens in Secrets.</p>
   </div>
  </details>
  <OperationFeedback :message="feedback" class="mt-3" />
  <p v-if="loading" role="status" class="my-3 text-sm">Loading variables…</p>
  <p v-if="error" role="alert" class="my-3 text-sm text-destructive">{{ error }} <Button variant="outline" size="sm" @click="load">Retry</Button></p>
  <ul class="my-4 max-h-48 divide-y overflow-auto rounded-lg border">
   <li v-for="key in Object.keys(variables).sort()" :key="key" class="flex items-start gap-2 p-3 text-sm">
    <div class="min-w-0 flex-1"><strong class="break-all">{{ key }}</strong><pre class="max-h-20 overflow-auto whitespace-pre-wrap break-all text-xs text-muted-foreground">{{ JSON.stringify(variables[key]) }}</pre></div>
    <Button size="sm" variant="ghost" :disabled="pending" :aria-label="`Edit variable ${key}`" @click="edit(key)">Edit</Button>
    <Button size="sm" variant="ghost" :disabled="pending" :aria-label="`Delete variable ${key}`" @click="remove(key)">Delete</Button>
   </li>
  </ul>
  <p v-if="!loading && !error && !Object.keys(variables).length" class="mb-3 text-sm text-muted-foreground">No variables yet. Add one below or use a Set Variable node.</p>
  <fieldset :disabled="pending" class="space-y-3">
   <div><Label for="variable-name">Variable name</Label><Input id="variable-name" v-model="name" placeholder="threshold" /></div>
   <div><Label for="variable-format">Value format</Label><Select id="variable-format" v-model="format" :options="[{ value: 'text', label: 'Text' }, { value: 'json', label: 'JSON (number, boolean, object, array or null)' }]" /></div>
   <div><Label for="variable-value">Variable value</Label><Textarea id="variable-value" v-model="value" /></div>
  </fieldset>
  <p v-if="name && validation" class="mt-2 text-sm text-destructive">{{ validation }}</p>
  <div class="mt-4 flex justify-end gap-2"><Button variant="outline" :disabled="pending" @click="emit('update:open',false)">Done</Button><Button :disabled="pending || loading || !!error || !!validation" @click="save">Save variable</Button></div>
  <ConfirmDialog ref="confirmation" />
 </Dialog>
</template>
