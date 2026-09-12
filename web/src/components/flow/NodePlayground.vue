<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useApi } from '@/composables/useApi'
import { Button, Textarea, Input, Label, Select } from '@/components/ui'
import { FlaskConical, Loader2 } from 'lucide-vue-next'
import PayloadTree from './PayloadTree.vue'
import type { NodeTestResult, NodeTestDraft } from '@/types'
const props = defineProps<{ nodeType: string; config: Record<string, unknown>; draft?: NodeTestDraft }>()
const emit = defineEmits<{ draft: [value: NodeTestDraft] }>()
const api = useApi()
const mode = ref<'json' | 'text'>(props.draft?.mode ?? (props.nodeType === 'process-parse' ? 'text' : 'json'))
const payload = ref(props.draft?.payload ?? '{\n  "temperature": 22.5,\n  "room": "office"\n}')
const topic = ref(props.draft?.topic ?? '')
const metadata = ref(props.draft?.metadata ?? '{}')
const result = ref<NodeTestResult | null>(null)
const running = ref(false)
const lastSignature = ref('')
const notice = ref('')
let controller: AbortController | undefined
let active = true
const draft = computed<NodeTestDraft>(() => ({ mode: mode.value, payload: payload.value, topic: topic.value, metadata: metadata.value }))
watch(draft, value => emit('draft', value))
const signature = computed(() => JSON.stringify([draft.value, props.config]))
const stale = computed(() => !!result.value && signature.value !== lastSignature.value)
const validation = computed(() => {
  try {
    if (mode.value === 'json') JSON.parse(payload.value)
  } catch (error) { return `Invalid JSON. ${error instanceof Error ? error.message : 'Check the input payload.'}` }
  try {
    const meta = JSON.parse(metadata.value)
    if (!meta || typeof meta !== 'object' || Array.isArray(meta)) return 'Metadata must be a JSON object.'
  } catch { return 'Metadata must be a valid JSON object.' }
  return ''
})
const outputs = computed(() => result.value?.outputs ?? [])
const status = computed(() => {
  if (running.value) return 'Testing node…'
  if (stale.value) return 'Result is out of date. Input or configuration changed; test again.'
  if (result.value?.error) return 'Test failed. Review the error below and try again.'
  if (result.value) return `${outputs.value.length} output message${outputs.value.length === 1 ? '' : 's'} · ${result.value.durMs?.toFixed(2) ?? '—'} ms`
  return notice.value
})
function formatJSON() {
  try { payload.value = JSON.stringify(JSON.parse(payload.value), null, 2) } catch { /* validation explains the problem */ }
}
function sample() {
  payload.value = '{\n  "temperature": 22.5,\n  "room": "office"\n}'
}
function reuse(value: unknown) {
  mode.value = 'json'
  payload.value = JSON.stringify(value, null, 2) ?? 'null'
}
async function runTest() {
  if (validation.value || running.value) return
  running.value = true
  result.value = null
  notice.value = ''
  const testedSignature = signature.value
  controller = new AbortController()
  const currentController = controller
  try {
    const response = await api.testNode(props.nodeType, JSON.parse(JSON.stringify(props.config)), mode.value === 'json' ? JSON.parse(payload.value) : payload.value, { topic: topic.value, meta: JSON.parse(metadata.value) }, currentController.signal)
    if (active && !currentController.signal.aborted) { result.value = response; lastSignature.value = testedSignature }
  } catch (error: any) {
    if (active && !currentController.signal.aborted) {
      result.value = { error: error?.response?.data?.error || 'Could not test the node. Check your connection and try again.' }
      lastSignature.value = testedSignature
    }
  } finally { if (active) { running.value = false; controller = undefined } }
}
function cancel() { controller?.abort(); notice.value = 'Test cancelled.' }
onUnmounted(() => { active = false; controller?.abort() })
function valueType(value: unknown) { return value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value }
</script>
<template>
  <section aria-label="Payload playground" class="grid min-w-0 gap-4 xl:grid-cols-2">
    <div class="xl:col-span-2">
      <h3 class="font-semibold">Payload playground</h3>
      <p class="mt-1 text-sm text-muted-foreground">Try this node with a sample message and your current configuration. No deployment needed. Each test uses a fresh node; samples are not saved with the flow.</p>
    </div>
    <section class="min-w-0 rounded-xl border bg-background/50 p-4">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h4 class="text-sm font-semibold">1. Input</h4>
        <Select aria-label="Payload format" class="w-28" :model-value="mode" :options="[{ value: 'json', label: 'JSON' }, { value: 'text', label: 'Text' }]" @update:model-value="mode = $event as 'json' | 'text'" />
      </div>
    <div class="mb-3">
      <div class="flex gap-2">
        <Button :disabled="!!validation || running" @click="runTest"><Loader2 v-if="running" class="mr-2 h-4 w-4 animate-spin" /><FlaskConical v-else class="mr-2 h-4 w-4" />{{ running ? 'Testing…' : 'Test node' }}</Button>
        <Button v-if="running" variant="outline" @click="cancel">Cancel test</Button>
      </div>

    </div>
      <Label for="sample-payload" class="sr-only">Input payload</Label>
      <Textarea id="sample-payload" v-model="payload" :rows="5" spellcheck="false" :aria-invalid="!!validation" aria-describedby="payload-help payload-validation" class="resize-y" />
      <p id="payload-help" class="mt-2 text-xs text-muted-foreground">{{ mode === 'json' ? 'Objects, arrays, strings, numbers, booleans and null are accepted.' : 'Sent exactly as text, including whitespace. Useful for Parse nodes.' }}</p>
      <div class="mt-3 flex flex-wrap gap-2">
        <Button v-if="mode === 'json'" size="sm" variant="outline" :disabled="!!validation" @click="formatJSON">Format JSON</Button>
        <Button size="sm" variant="ghost" title="Replace only the test input with a sensor message" @click="sample">Use sample payload</Button>
      </div>
      <details class="mt-4 border-t pt-3">
        <summary class="cursor-pointer text-sm font-medium">Message context</summary>
        <div class="mt-3 space-y-3">
          <div><Label for="sample-topic">Topic</Label><Input id="sample-topic" v-model="topic" placeholder="sensors/office" /></div>
          <div><Label for="sample-metadata">Metadata (JSON object)</Label><Textarea id="sample-metadata" v-model="metadata" spellcheck="false" /></div>
        </div>
      </details>
      <p v-if="validation" id="payload-validation" role="alert" class="mt-3 break-words text-sm text-destructive">{{ validation }}</p>
    </section>
    <section aria-label="Test output" class="min-w-0 rounded-xl border bg-background/50 p-4">
      <h4 class="mb-2 text-sm font-semibold">2. Output</h4>
      <p v-if="status" role="status" class="mb-3 text-sm" :class="stale ? 'text-amber-400' : 'text-muted-foreground'">{{ status }}</p>
      <p v-if="!result && !running" class="text-sm text-muted-foreground">Test the node to inspect its output here.</p>
      <pre v-if="result?.error" :role="stale ? undefined : 'alert'" class="whitespace-pre-wrap break-words rounded bg-destructive/10 p-3 text-sm text-destructive">{{ result.error }}</pre>
      <div v-else-if="result && !outputs.length">
        <h5 class="font-medium">No output messages</h5>
        <p class="mt-1 text-sm text-muted-foreground">The node completed without emitting a message. A filter may drop the input; buffering nodes may need more messages in a running flow.</p>
      </div>
      <article v-for="(output, index) in outputs" :key="index" class="mt-3 min-w-0 rounded-lg border p-3">
        <div class="flex flex-wrap items-center justify-between gap-2"><h5 class="font-semibold">Output {{ index + 1 }}</h5><span class="text-xs text-muted-foreground">{{ valueType(output.payload) }} · <span>Port: {{ output.sourcePort || '—' }}</span></span></div>
        <PayloadTree :value="output.payload" />
        <details class="mt-3"><summary class="cursor-pointer text-xs text-muted-foreground">Raw message</summary><pre class="mt-2 max-h-60 overflow-auto rounded bg-muted p-2 text-xs">{{ JSON.stringify(output, null, 2) }}</pre></details>
        <Button class="mt-3" size="sm" variant="outline" :aria-label="`Use output ${index + 1} as input`" @click="reuse(output.payload)">Use as input</Button>
      </article>
    </section>
  </section>
</template>
