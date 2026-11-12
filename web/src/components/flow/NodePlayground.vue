<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useApi } from '@/composables/useApi'
import { Button, Textarea, Input, Label, Select } from '@/components/ui'
import { FlaskConical, Loader2 } from 'lucide-vue-next'
import PayloadTree from './PayloadTree.vue'
import type { NodeTestResult, NodeTestDraft } from '@/types'
import { messages as m } from '@/i18n'
const props = defineProps<{ flowId?: string; nodeType: string; config: Record<string, unknown>; draft?: NodeTestDraft }>()
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
  } catch (error) { return m.value.flowTools.playground.invalidJson(error instanceof Error ? error.message : m.value.flowTools.playground.checkInput) }
  try {
    const meta = JSON.parse(metadata.value)
    if (!meta || typeof meta !== 'object' || Array.isArray(meta)) return m.value.flowTools.playground.metadataObject
  } catch { return m.value.flowTools.playground.metadataValid }
  return ''
})
const outputs = computed(() => result.value?.outputs ?? [])
const status = computed(() => {
  if (running.value) return m.value.flowTools.playground.testing
  if (stale.value) return m.value.flowTools.playground.stale
  if (result.value?.error) return m.value.flowTools.playground.failed
  if (result.value) return m.value.flowTools.playground.summary(outputs.value.length, result.value.durMs?.toFixed(2) ?? '—')
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
    const response = await api.testNode(props.nodeType, JSON.parse(JSON.stringify(props.config)), mode.value === 'json' ? JSON.parse(payload.value) : payload.value, { flowId: props.flowId, topic: topic.value, meta: JSON.parse(metadata.value) }, currentController.signal)
    if (active && !currentController.signal.aborted) { result.value = response; lastSignature.value = testedSignature }
  } catch (error: any) {
    if (active && !currentController.signal.aborted) {
      result.value = { error: error?.response?.data?.error || m.value.flowTools.playground.unreachable }
      lastSignature.value = testedSignature
    }
  } finally { if (active) { running.value = false; controller = undefined } }
}
function cancel() { controller?.abort(); notice.value = m.value.flowTools.playground.cancelled }
onUnmounted(() => { active = false; controller?.abort() })
function valueType(value: unknown) { return value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value }
</script>
<template>
  <section :aria-label="m.flowTools.playground.title" class="grid min-w-0 gap-4 xl:grid-cols-2">
    <div class="xl:col-span-2">
      <h3 class="font-semibold">{{ m.flowTools.playground.title }}</h3>
      <p class="mt-1 text-sm text-muted-foreground">{{ m.flowTools.playground.intro }}</p>
    </div>
    <section class="min-w-0 rounded-xl border bg-background/50 p-4">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h4 class="text-sm font-semibold">{{ m.flowTools.playground.input }}</h4>
        <Select :aria-label="m.flowTools.playground.format" class="w-28" :model-value="mode" :options="[{ value: 'json', label: 'JSON' }, { value: 'text', label: m.flowTools.playground.text }]" @update:model-value="mode = $event as 'json' | 'text'" />
      </div>
    <div class="mb-3">
      <div class="flex gap-2">
        <Button :disabled="!!validation || running" @click="runTest"><Loader2 v-if="running" class="mr-2 h-4 w-4 animate-spin" /><FlaskConical v-else class="mr-2 h-4 w-4" />{{ running ? m.flowTools.playground.testingShort : m.flowTools.playground.test }}</Button>
        <Button v-if="running" variant="outline" @click="cancel">{{ m.flowTools.playground.cancel }}</Button>
      </div>

    </div>
      <Label for="sample-payload" class="sr-only">{{ m.flowTools.playground.payload }}</Label>
      <Textarea id="sample-payload" v-model="payload" :rows="5" spellcheck="false" :aria-invalid="!!validation" aria-describedby="payload-help payload-validation" class="resize-y" />
      <p id="payload-help" class="mt-2 text-xs text-muted-foreground">{{ mode === 'json' ? m.flowTools.playground.jsonHelp : m.flowTools.playground.textHelp }}</p>
      <div class="mt-3 flex flex-wrap gap-2">
        <Button v-if="mode === 'json'" size="sm" variant="outline" :disabled="!!validation" @click="formatJSON">{{ m.flowTools.playground.formatJson }}</Button>
        <Button size="sm" variant="ghost" :title="m.flowTools.playground.sampleHint" @click="sample">{{ m.flowTools.playground.sample }}</Button>
      </div>
      <details class="mt-4 border-t pt-3">
        <summary class="cursor-pointer text-sm font-medium">{{ m.flowTools.playground.context }}</summary>
        <div class="mt-3 space-y-3">
          <div><Label for="sample-topic">{{ m.flowTools.playground.topic }}</Label><Input id="sample-topic" v-model="topic" placeholder="sensors/office" /></div>
          <div><Label for="sample-metadata">{{ m.flowTools.playground.metadata }}</Label><Textarea id="sample-metadata" v-model="metadata" spellcheck="false" /></div>
        </div>
      </details>
      <p v-if="validation" id="payload-validation" role="alert" class="mt-3 break-words text-sm text-destructive">{{ validation }}</p>
    </section>
    <section :aria-label="m.flowTools.playground.outputSection" class="min-w-0 rounded-xl border bg-background/50 p-4">
      <h4 class="mb-2 text-sm font-semibold">{{ m.flowTools.playground.output }}</h4>
      <p v-if="status" role="status" class="mb-3 text-sm" :class="stale ? 'text-amber-400' : 'text-muted-foreground'">{{ status }}</p>
      <p v-if="!result && !running" class="text-sm text-muted-foreground">{{ m.flowTools.playground.empty }}</p>
      <pre v-if="result?.error" :role="stale ? undefined : 'alert'" class="whitespace-pre-wrap break-words rounded bg-destructive/10 p-3 text-sm text-destructive">{{ result.error }}</pre>
      <div v-else-if="result && !outputs.length">
        <h5 class="font-medium">{{ m.flowTools.playground.noOutput }}</h5>
        <p class="mt-1 text-sm text-muted-foreground">{{ m.flowTools.playground.noOutputHint }}</p>
      </div>
      <article v-for="(output, index) in outputs" :key="index" class="mt-3 min-w-0 rounded-lg border p-3">
        <div class="flex flex-wrap items-center justify-between gap-2"><h5 class="font-semibold">{{ m.flowTools.playground.outputN(index + 1) }}</h5><span class="text-xs text-muted-foreground">{{ valueType(output.payload) }} · <span>{{ m.flowTools.playground.port }} {{ output.sourcePort || '—' }}</span></span></div>
        <PayloadTree :value="output.payload" />
        <details class="mt-3"><summary class="cursor-pointer text-xs text-muted-foreground">{{ m.flowTools.playground.raw }}</summary><pre class="mt-2 max-h-60 overflow-auto rounded bg-muted p-2 text-xs">{{ JSON.stringify(output, null, 2) }}</pre></details>
        <Button class="mt-3" size="sm" variant="outline" :aria-label="m.flowTools.playground.reuseLabel(index + 1)" @click="reuse(output.payload)">{{ m.flowTools.playground.reuse }}</Button>
      </article>
    </section>
  </section>
</template>
