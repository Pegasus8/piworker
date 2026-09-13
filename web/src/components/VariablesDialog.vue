<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { useFeedback } from '@/composables/useFeedback'
import { Button, Dialog, Input, Label, Select, Textarea } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()
const api = useApi()
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
 loading.value = true; error.value = ''
 try { variables.value = await api.getVariables() }
 catch { error.value = 'Could not load variables. Try again.' }
 finally { loading.value = false }
}
watch(() => props.open, open => { if (open) { void load(); feedback.value = null } })
function edit(key: string) {
 name.value = key
 const current = variables.value[key]
 format.value = typeof current === 'string' ? 'text' : 'json'
 value.value = typeof current === 'string' ? current : JSON.stringify(current, null, 2)
}
async function save() {
 if (validation.value || pending.value) return
 const parsed = format.value === 'json' ? JSON.parse(value.value) : value.value
 if (Object.prototype.hasOwnProperty.call(variables.value, name.value) && !await confirmation.value?.ask('Replace global variable?', `All flows that read “${name.value}” will use its new value.`, 'Replace variable')) return
 await run(async () => { await api.setVariable(name.value, parsed); variables.value = { ...variables.value, [name.value]: parsed }; name.value = ''; value.value = '' }, 'Variable saved.', 'Could not save the variable. Your input is still here. Try again.')
}
async function remove(key: string) {
 if (pending.value || !await confirmation.value?.ask('Delete global variable?', `Flows reading “${key}” will receive their default or a missing value.`, 'Delete variable')) return
 await run(async () => { await api.deleteVariable(key); delete variables.value[key] }, 'Variable deleted.', 'Could not delete the variable. Try again.')
}
</script>
<template>
 <Dialog :open="open" title="Global variables" @update:open="!pending && emit('update:open', $event)">
  <h2 class="text-lg font-semibold">Global variables</h2>
  <p class="mt-2 text-sm text-muted-foreground">Shared by all flows and saved across restarts. Changes affect running flows on their next read. Use Secrets for credentials.</p>
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
