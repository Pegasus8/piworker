<script setup lang="ts">
// "Test tool": calls the deployed tool of a trigger-tool node through the
// REST twin of an agent's MCP call. The input schema, concurrency limits and
// cooldown apply; the confirm policy does not. The result comes from the
// flow and is shown as plain text only (never rendered as HTML).
import { computed, onUnmounted, ref, watch } from 'vue'
import { Button, Label, Textarea } from '@/components/ui'
import { Play } from 'lucide-vue-next'
import { useApi } from '@/composables/useApi'
import { argsTextFromSchema, callErrorText, callResultText, parseToolArgs } from '@/lib/toolCall'

const props = defineProps<{
  flowId?: string
  nodeId: string
  schema: unknown
  running: boolean
  dirty?: boolean
}>()

const api = useApi()
const args = ref(argsTextFromSchema(props.schema))
const output = ref('')
const failed = ref(false)
const pending = ref(false)
let controller: AbortController | undefined

const parsed = computed(() => parseToolArgs(args.value))
const blocked = computed(() => {
  if (!props.flowId) return 'Save the flow, then deploy it to test this tool.'
  if (!props.running) return 'Deploy the flow to test this tool.'
  return ''
})

watch(() => props.nodeId, () => {
  args.value = argsTextFromSchema(props.schema)
  output.value = ''
  failed.value = false
})

async function run() {
  if (blocked.value || parsed.value.error || pending.value || !props.flowId) return
  controller = new AbortController()
  pending.value = true
  output.value = ''
  failed.value = false
  try {
    const res = await api.callTool(props.flowId, props.nodeId, parsed.value.value ?? {}, controller.signal)
    output.value = callResultText(res)
    failed.value = res.status === 'error'
  } catch (err) {
    output.value = callErrorText(err)
    failed.value = true
  } finally {
    pending.value = false
    controller = undefined
  }
}

onUnmounted(() => controller?.abort())
</script>

<template>
  <section aria-labelledby="test-tool-title" class="space-y-2 border-t pt-4">
    <h3 id="test-tool-title" class="font-semibold">Test tool</h3>
    <p class="text-sm text-muted-foreground">
      Call the deployed tool the way an agent would. Connected actions will execute.
      The arguments are checked against the deployed input schema; confirmation does not apply here.
    </p>
    <Label for="test-tool-args" class="block">Arguments (JSON object)</Label>
    <Textarea
      id="test-tool-args"
      v-model="args"
      :rows="6"
      spellcheck="false"
      autocapitalize="off"
      class="font-mono text-xs"
      :aria-invalid="!!parsed.error"
      :aria-describedby="parsed.error ? 'test-tool-args-error' : undefined"
    />
    <p v-if="parsed.error" id="test-tool-args-error" role="alert" class="break-words text-xs text-destructive">{{ parsed.error }}</p>
    <p v-if="blocked" role="status" class="text-xs text-muted-foreground">{{ blocked }}</p>
    <p v-else-if="dirty" class="text-xs text-muted-foreground">The deployed version runs: unsaved changes are not part of it.</p>
    <Button size="sm" :disabled="!!blocked || !!parsed.error || pending" @click="run">
      <Play class="mr-2 h-4 w-4" />
      {{ pending ? 'Running…' : 'Run' }}
    </Button>
    <pre
      v-if="output"
      :role="failed ? 'alert' : 'status'"
      class="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted p-2 text-xs"
      :class="failed ? 'text-destructive' : ''"
    >{{ output }}</pre>
  </section>
</template>
