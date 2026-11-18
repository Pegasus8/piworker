<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useObservabilityStore } from '@/stores/observability'
import { useApi } from '@/composables/useApi'
import { Button, Dialog, Label, Select, Textarea } from '@/components/ui'
import { messages as m } from '@/i18n'
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
    if (JSON.parse(payload.value) === null) return m.value.flowTools.manualRun.nullValue
    return ''
  } catch { return m.value.flowTools.manualRun.invalidJson }
})
const ready = computed(() => !!store.currentFlow?.id && store.currentFlow.running && !store.isDirty && nodes.value.some(n => n.value === nodeId.value) && !validation.value && !pending.value)
async function send() {
  if (!ready.value) return
  const id = store.currentFlow!.id
  pending.value = true; error.value = ''; success.value = ''
  try {
    await api.injectNode(id, nodeId.value, mode.value === 'configured' ? undefined : mode.value === 'json' ? JSON.parse(payload.value) : payload.value)
    success.value = m.value.flowTools.manualRun.sent
    void obs.refreshRuns()
  } catch { error.value = m.value.flowTools.manualRun.failed }
  finally { pending.value = false }
}
</script>
<template>
  <Dialog :open="open" :title="m.flowTools.manualRun.title" @update:open="!pending && emit('update:open', $event)">
    <h2 class="text-lg font-semibold">{{ m.flowTools.manualRun.title }}</h2>
    <p class="mt-2 text-sm text-muted-foreground">{{ m.flowTools.manualRun.intro }}</p>
    <fieldset :disabled="pending" class="my-4 space-y-3">
      <div><Label for="manual-node">{{ m.flowTools.manualRun.trigger }}</Label><Select id="manual-node" v-model="nodeId" :options="nodes" :placeholder="m.flowTools.manualRun.chooseTrigger" /></div>
      <div><Label for="manual-mode">{{ m.flowTools.manualRun.source }}</Label><Select id="manual-mode" v-model="mode" :options="[{ value: 'configured', label: m.flowTools.manualRun.configured }, { value: 'json', label: m.flowTools.manualRun.json }, { value: 'text', label: m.flowTools.manualRun.text }]" /></div>
      <div v-if="mode !== 'configured'"><Label for="manual-payload">{{ m.flowTools.manualRun.payload }}</Label><Textarea id="manual-payload" v-model="payload" class="font-mono" /></div>
    </fieldset>
    <p class="text-xs text-muted-foreground">{{ m.flowTools.manualRun.hintBefore }} <code>payload.data</code>{{ m.flowTools.manualRun.hintAlongside }} <code>payload.count</code> {{ m.flowTools.manualRun.hintAnd }} <code>payload.timestamp</code>{{ m.flowTools.manualRun.hintAfter }}</p>
    <p v-if="validation" role="alert" class="mt-3 text-sm text-destructive">{{ validation }}</p>
    <p v-if="error" role="alert" class="mt-3 text-sm text-destructive">{{ error }}</p>
    <p v-if="success" role="status" class="mt-3 text-sm text-primary">{{ success }}</p>
    <div class="mt-4 flex justify-end gap-2"><Button variant="outline" :disabled="pending" @click="emit('update:open', false)">{{ m.common.done }}</Button><Button :disabled="!ready" @click="send">{{ pending ? m.flowTools.manualRun.sending : m.flowTools.manualRun.send }}</Button></div>
  </Dialog>
</template>
