<script setup lang="ts">
// A flow's version history (docs/design/mcp.md §10): who saved each version
// (the editor or an agent token), which one is deployed and which you last
// approved, and a one-click rollback to that version (confirmed, naming it;
// a running flow is redeployed). Token names and notes are agent data: text.
import { ref, watch } from 'vue'
import { History, RotateCcw, X } from 'lucide-vue-next'
import { Badge, Button, Dialog } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import AgentText from '@/components/agent/AgentText.vue'
import type { Feedback } from '@/composables/useFeedback'
import { useFlowsStore } from '@/stores/flows'
import { apiErrorMessage } from '@/stores/agent'
import { formatDateTime, visibleText } from '@/lib/agentAccess'
import { applyOutcome, type ApplyResult } from '@/lib/drafts'
import { rollbackConfirmText, rollbackTarget, versionActor, versionTags } from '@/lib/flowMarks'
import type { VersionList } from '@/types'

const props = withDefaults(defineProps<{
  open: boolean
  flowId: string
  flowName: string
  running: boolean
  /** The editor holds unsaved changes, which a rollback discards. */
  dirty?: boolean
}>(), { dirty: false })
const emit = defineEmits<{ 'update:open': [value: boolean]; 'rolled-back': [result: ApplyResult] }>()

const flows = useFlowsStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const list = ref<VersionList | null>(null)
const loading = ref(false)
const loadError = ref('')
const busy = ref(false)
const feedback = ref<Feedback | null>(null)

const status = (e: unknown) => (e as { response?: { status?: number } })?.response?.status

let seq = 0
async function load() {
  const mine = ++seq
  loading.value = true
  loadError.value = ''
  try {
    const l = await flows.fetchVersions(props.flowId)
    if (mine === seq) list.value = l
  } catch (e) {
    if (mine !== seq) return
    list.value = null
    loadError.value = status(e) === 404
      ? 'This flow has no version history here (the flow was deleted, or this server does not keep versions).'
      : apiErrorMessage(e, 'Could not load the version history. Try again.')
  } finally {
    if (mine === seq) loading.value = false
  }
}

watch(() => [props.open, props.flowId] as const, ([open]) => {
  if (!open) return
  feedback.value = null
  void load()
}, { immediate: true })

async function rollback() {
  if (!list.value || busy.value) return
  const target = rollbackTarget(list.value)
  if (!target.possible) return
  const discard = props.dirty ? ' Your unsaved changes in the editor are discarded.' : ''
  const ok = await confirmation.value?.ask(
    `Roll back to version ${target.version}?`,
    rollbackConfirmText(list.value, props.flowName, props.running) + discard,
    `Roll back to version ${target.version}`)
  if (!ok) return
  busy.value = true
  feedback.value = { kind: 'pending', text: 'Rolling back…' }
  try {
    const result = await flows.rollbackFlow(props.flowId)
    feedback.value = applyOutcome(result, `Rolled back to version ${target.version}`)
    emit('rolled-back', result)
    await load()
  } catch (e) {
    feedback.value = { kind: 'error', text: visibleText(apiErrorMessage(e, 'Could not roll back. Try again.')) }
    await load()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog :open="open" title="Version history" class="max-w-2xl" @update:open="emit('update:open', $event)">
    <div class="space-y-4">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 class="flex items-center gap-2 text-lg font-semibold"><History class="h-5 w-5 text-primary" aria-hidden="true" />Version history</h2>
          <p class="break-words text-sm text-muted-foreground"><AgentText :text="flowName" /></p>
        </div>
        <Button variant="ghost" size="icon" aria-label="Close version history" autofocus @click="emit('update:open', false)"><X class="h-4 w-4" /></Button>
      </div>

      <OperationFeedback :message="feedback" @dismiss="feedback = null" />
      <p v-if="loading && !list" role="status" class="text-sm text-muted-foreground">Loading versions…</p>
      <div v-else-if="loadError" role="alert" class="space-y-2 text-sm text-destructive">
        <p>{{ loadError }}</p>
        <Button variant="outline" size="sm" @click="load">Retry</Button>
      </div>

      <template v-if="list">
        <section aria-labelledby="rollback-heading" class="space-y-2 rounded-md border p-3">
          <h3 id="rollback-heading" class="text-sm font-semibold">Roll back to the version you last approved</h3>
          <p class="text-sm text-muted-foreground">
            Current version: {{ list.current }}.
            {{ list.lastAdminApproved ? `You last approved version ${list.lastAdminApproved}.` : 'No version you approved is kept.' }}
          </p>
          <Button v-if="rollbackTarget(list).possible" variant="outline" :disabled="busy" @click="rollback">
            <RotateCcw class="mr-2 h-4 w-4" aria-hidden="true" />Roll back to version {{ rollbackTarget(list).version }}…
          </Button>
          <p v-else class="text-sm">{{ rollbackTarget(list).reason }}</p>
        </section>

        <ol class="space-y-2" aria-label="Versions, newest first">
          <li v-for="v in list.versions" :key="v.version" class="space-y-1 rounded-md border p-3 text-sm" :class="!v.adminApproved && 'border-amber-500/40'">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-semibold">Version {{ v.version }}</span>
              <Badge v-for="tag in versionTags(v, list)" :key="tag" :variant="tag === 'Not approved by you' ? 'destructive' : tag === 'Current' ? 'default' : 'secondary'">{{ tag }}</Badge>
            </div>
            <p class="text-muted-foreground">
              <span class="break-words"><AgentText :text="versionActor(v)" /></span> · <span :title="formatDateTime(v.createdAt)">{{ formatDateTime(v.createdAt) }}</span>
            </p>
            <p v-if="v.note" class="whitespace-pre-wrap break-words text-xs text-muted-foreground"><AgentText :text="v.note" /></p>
          </li>
        </ol>
        <p v-if="!list.versions.length" class="text-sm text-muted-foreground">No versions are kept for this flow.</p>
      </template>
    </div>
    <ConfirmDialog ref="confirmation" />
  </Dialog>
</template>
