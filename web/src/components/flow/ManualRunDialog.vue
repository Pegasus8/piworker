<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useObservabilityStore } from '@/stores/observability'
import { useApi } from '@/composables/useApi'
import { Button, Dialog, Label, Select, Textarea } from '@/components/ui'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
const store = useFlowsStore()
const obs = useObservabilityStore()
const api = useApi()
const nodeId = ref('')
const mode = ref('configured')
const payload = ref('{}')
const pending = ref(false)
const error = ref('')
const success = ref('')
const nodes = computed(() => store.nodes.filter(n => n.data.nodeType === 'trigger-manual' && n.data.enabled !== false).map(n => ({ value: n.id, label: `${n.data.label} · ${n.id}` })))
watch(() => props.open, open => {
  if (open) { nodeId.value = nodes.value.length === 1 ? nodes.value[0].value : ''; error.value = ''; success.value = '' }
})
const validation = computed(() => {
  if (mode.value !== 'json') return ''
  try {
    if (JSON.parse(payload.value) === null) return 'Use a JSON value other than null. The API treats null as the configured payload.'
    return ''
  } catch { return 'Enter valid JSON before sending.' }
})
const ready = computed(() => !!store.currentFlow?.id && store.currentFlow.running && !store.isDirty && nodes.value.some(n => n.value === nodeId.value) && !validation.value && !pending.value)
async function send() {
  if (!ready.value) return
  const id = store.currentFlow!.id
  pending.value = true; error.value = ''; success.value = ''
  try {
    await api.injectNode(id, nodeId.value, mode.value === 'configured' ? undefined : mode.value === 'json' ? JSON.parse(payload.value) : payload.value)
    success.value = 'Message sent. Follow its execution in Activity.'
    void obs.refreshRuns()
  } catch { error.value = 'Could not send the message. Check that the flow is running and try again. If the connection was lost, check Activity before retrying to avoid sending twice.' }
  finally { pending.value = false }
}
</script>
<template>
  <Dialog :open="open" title="Run manually" @update:open="!pending && emit('update:open', $event)">
    <h2 class="text-lg font-semibold">Run manually</h2>
    <p class="mt-2 text-sm text-muted-foreground">Send one message through the deployed flow. Connected actions will execute. Your saved configuration stays the same.</p>
    <fieldset :disabled="pending" class="my-4 space-y-3">
      <div><Label for="manual-node">Manual trigger</Label><Select id="manual-node" v-model="nodeId" :options="nodes" placeholder="Choose a trigger" /></div>
      <div><Label for="manual-mode">Message source</Label><Select id="manual-mode" v-model="mode" :options="[{ value: 'configured', label: 'Saved node payload' }, { value: 'json', label: 'Custom JSON' }, { value: 'text', label: 'Custom text' }]" /></div>
      <div v-if="mode !== 'configured'"><Label for="manual-payload">Payload</Label><Textarea id="manual-payload" v-model="payload" class="font-mono" /></div>
    </fieldset>
    <p class="text-xs text-muted-foreground">Downstream nodes read this value at <code>payload.data</code>, alongside <code>payload.count</code> and <code>payload.timestamp</code>. The topic comes from the saved trigger.</p>
    <p v-if="validation" role="alert" class="mt-3 text-sm text-destructive">{{ validation }}</p>
    <p v-if="error" role="alert" class="mt-3 text-sm text-destructive">{{ error }}</p>
    <p v-if="success" role="status" class="mt-3 text-sm text-primary">{{ success }}</p>
    <div class="mt-4 flex justify-end gap-2"><Button variant="outline" :disabled="pending" @click="emit('update:open', false)">Done</Button><Button :disabled="!ready" @click="send">{{ pending ? 'Sending…' : 'Send message' }}</Button></div>
  </Dialog>
</template>
