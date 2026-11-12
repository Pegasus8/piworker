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
import { messages as m } from '@/i18n'

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
      ? m.value.flowTools.versions.notFound
      : apiErrorMessage(e, m.value.flowTools.versions.loadFailed)
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
  const discard = props.dirty ? m.value.flowTools.versions.discard : ''
  const ok = await confirmation.value?.ask(
    m.value.flowTools.versions.confirmTitle(target.version),
    rollbackConfirmText(list.value, props.flowName, props.running) + discard,
    m.value.flowTools.versions.confirmAction(target.version))
  if (!ok) return
  busy.value = true
  feedback.value = { kind: 'pending', text: m.value.flowTools.versions.rollingBack }
  try {
    const result = await flows.rollbackFlow(props.flowId)
    feedback.value = applyOutcome(result, m.value.flowTools.versions.rolledBack(target.version))
    emit('rolled-back', result)
    await load()
  } catch (e) {
    feedback.value = { kind: 'error', text: visibleText(apiErrorMessage(e, m.value.flowTools.versions.rollbackFailed)) }
    await load()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog :open="open" :title="m.flowTools.versions.title" class="max-w-2xl" @update:open="emit('update:open', $event)">
    <div class="space-y-4">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 class="flex items-center gap-2 text-lg font-semibold"><History class="h-5 w-5 text-primary" aria-hidden="true" />{{ m.flowTools.versions.title }}</h2>
          <p class="break-words text-sm text-muted-foreground"><AgentText :text="flowName" /></p>
        </div>
        <Button variant="ghost" size="icon" :aria-label="m.flowTools.versions.close" autofocus @click="emit('update:open', false)"><X class="h-4 w-4" /></Button>
      </div>

      <OperationFeedback :message="feedback" @dismiss="feedback = null" />
      <p v-if="loading && !list" role="status" class="text-sm text-muted-foreground">{{ m.flowTools.versions.loading }}</p>
      <div v-else-if="loadError" role="alert" class="space-y-2 text-sm text-destructive">
        <p>{{ loadError }}</p>
        <Button variant="outline" size="sm" @click="load">{{ m.common.retry }}</Button>
      </div>

      <template v-if="list">
        <section aria-labelledby="rollback-heading" class="space-y-2 rounded-md border p-3">
          <h3 id="rollback-heading" class="text-sm font-semibold">{{ m.flowTools.versions.rollbackHeading }}</h3>
          <p class="text-sm text-muted-foreground">
            {{ m.flowTools.versions.current(list.current) }}
            {{ list.lastAdminApproved ? m.flowTools.versions.lastApproved(list.lastAdminApproved) : m.flowTools.versions.noneApproved }}
          </p>
          <Button v-if="rollbackTarget(list).possible" variant="outline" :disabled="busy" @click="rollback">
            <RotateCcw class="mr-2 h-4 w-4" aria-hidden="true" />{{ m.flowTools.versions.rollbackButton(rollbackTarget(list).version) }}
          </Button>
          <p v-else class="text-sm">{{ rollbackTarget(list).reason }}</p>
        </section>

        <ol class="space-y-2" :aria-label="m.flowTools.versions.list">
          <li v-for="v in list.versions" :key="v.version" class="space-y-1 rounded-md border p-3 text-sm" :class="!v.adminApproved && 'border-amber-500/40'">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-semibold">{{ m.flowTools.versions.version(v.version) }}</span>
              <Badge v-for="tag in versionTags(v, list)" :key="tag.kind" :variant="tag.kind === 'notApproved' ? 'destructive' : tag.kind === 'current' ? 'default' : 'secondary'">{{ tag.label }}</Badge>
            </div>
            <p class="text-muted-foreground">
              <span class="break-words"><AgentText :text="versionActor(v)" /></span> · <span :title="formatDateTime(v.createdAt)">{{ formatDateTime(v.createdAt) }}</span>
            </p>
            <p v-if="v.note" class="whitespace-pre-wrap break-words text-xs text-muted-foreground"><AgentText :text="v.note" /></p>
          </li>
        </ol>
        <p v-if="!list.versions.length" class="text-sm text-muted-foreground">{{ m.flowTools.versions.none }}</p>
      </template>
    </div>
    <ConfirmDialog ref="confirmation" />
  </Dialog>
</template>
