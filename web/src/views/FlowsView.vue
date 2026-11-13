<script setup lang="ts">
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useFlowsStore, FlowVersionConflictError } from '@/stores/flows'
import { useShellStore } from '@/stores/shell'
import { useApi } from '@/composables/useApi'
import { visibleText } from '@/lib/agentAccess'
import { summarizeRuns, type RunSummary } from '@/lib/flowActivity'
import { Button, Card, Input, Switch, Badge, Dialog, Menu } from '@/components/ui'
import type { MenuAction } from '@/components/ui/Menu.vue'
import { deployWarningsSummary } from '@/lib/deployWarnings'
import type { FlowIssue } from '@/lib/drafts'
import ExamplesDialog from '@/components/ExamplesDialog.vue'
import FlowMarks from '@/components/agent/FlowMarks.vue'
import AgentText from '@/components/agent/AgentText.vue'
import VersionHistoryDialog from '@/components/flow/VersionHistoryDialog.vue'
import type { FlowListItem } from '@/types'
import { Plus, Trash2, Edit3, Search, Workflow, Copy, Download, Upload, History, LayoutTemplate, AlertTriangle, Clock } from 'lucide-vue-next'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { messages as m, formatRelative, formatDate } from '@/i18n'

const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const router = useRouter()
const flowsStore = useFlowsStore()
const shell = useShellStore()
const api = useApi()

const searchQuery = ref('')
const showExamples = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

async function handleDuplicate(id: string) {
  await run(async () => { await api.duplicateFlow(id); await flowsStore.fetchFlows() }, m.value.flows.duplicated, m.value.flows.duplicateFailed)
}
async function handleExport(id: string, name: string) {
  await run(async () => {
    const flow = await api.exportFlow(id)
    const blob = new Blob([JSON.stringify(flow, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name.replace(/[^a-z0-9-_]+/gi, '_') || 'flow'}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, m.value.flows.exported, m.value.flows.exportFailed)
}

function triggerImport() {
  fileInput.value?.click()
}

async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await run(async () => {
    const flow = JSON.parse(await file.text())
    const imported = await api.importFlow(flow)
    await router.push(`/editor/${imported.id}`)
  }, m.value.flows.imported, m.value.flows.importFailed)
  input.value = ''
}

onMounted(() => {
  flowsStore.fetchFlows()
})

const displayedFlows = computed(() => {
  const query = searchQuery.value.toLowerCase()
  if (!query) return flowsStore.flows
  return flowsStore.flows.filter(
    flow =>
      flow.name.toLowerCase().includes(query) ||
      flow.description?.toLowerCase().includes(query)
  )
})

// Recent runs of each listed flow (the last 20), a few requests at a time:
// when it last ran and whether recent runs had errors.
const activity = ref<Record<string, RunSummary | null>>({})
let activityRequest = 0
watch(() => flowsStore.flows.map(f => `${f.id}:${f.updatedAt}:${f.running}`).join(','), async () => {
  const request = ++activityRequest
  const queue = flowsStore.flows.map(f => f.id)
  const worker = async () => {
    for (let id = queue.shift(); id; id = queue.shift()) {
      try {
        const runs = await api.getFlowRuns(id, 20)
        if (request === activityRequest) activity.value = { ...activity.value, [id]: summarizeRuns(Array.isArray(runs) ? runs : []) }
      } catch {
        if (request === activityRequest) activity.value = { ...activity.value, [id]: null }
      }
    }
  }
  await Promise.all([worker(), worker(), worker()])
})

const runningCount = computed(() => flowsStore.flows.filter(f => f.running).length)
const failingCount = computed(() => flowsStore.flows.filter(f => f.state === 'failed' || (activity.value[f.id]?.failed ?? 0) > 0).length)

async function toggleFlow(id: string, enabled: boolean) {
  let conflict: FlowVersionConflictError | null = null
  let warnings: FlowIssue[] = []
  const done = await run(async () => {
    try {
      warnings = await flowsStore.toggleFlow(id, enabled)
    } catch (e) {
      if (e instanceof FlowVersionConflictError) conflict = e
      throw e
    }
  }, enabled ? m.value.editor.deployed : m.value.editor.stopped, m.value.flows.toggleFailed)
  // The list has no node names: the warnings name the nodes by ID.
  if (done && warnings.length) feedback.value = { kind: 'warning', text: deployWarningsSummary(warnings, nodeId => nodeId) }
  flowsStore.error = null
  if (conflict) await offerReview(id, conflict)
}

// Turning on a flow saved since this list loaded (an approved draft, an
// agent's version) deploys nothing: show the new version and offer to review
// it in the editor first.
async function offerReview(id: string, conflict: FlowVersionConflictError) {
  const name = visibleText(flowsStore.flows.find(flow => flow.id === id)?.name ?? '') || m.value.flows.thisFlow
  feedback.value = { kind: 'error', text: m.value.flows.changedNotDeployed(name, conflict.currentVersion) }
  await flowsStore.fetchFlows()
  if (await confirmation.value?.ask(m.value.flows.changedTitle, m.value.flows.changedBody(name, conflict.currentVersion), m.value.flows.openInEditor, m.value.common.cancel)) await router.push(`/editor/${id}`)
}

// Version history and rollback (docs/design/mcp.md §10). The dialog stays
// mounted once opened so closing it returns focus to the button.
const historyId = ref('')
const historyOpen = ref(false)
const historyFlow = computed(() => flowsStore.flows.find(flow => flow.id === historyId.value) ?? null)
function openHistory(flow: FlowListItem) {
  historyId.value = flow.id
  historyOpen.value = true
}

async function deleteFlow(id: string) {
  if (pending.value) return
  const name = visibleText(flowsStore.flows.find(flow => flow.id === id)?.name ?? '') || m.value.flows.thisFlow
  if (!await confirmation.value?.ask(m.value.flows.deleteTitle, m.value.flows.deleteBody(name), m.value.flows.deleteFlow)) return
  await run(() => flowsStore.deleteFlow(id), m.value.flows.deleted, m.value.flows.deleteFailed)
  flowsStore.error = null
}

function actions(flow: FlowListItem): MenuAction[] {
  return [
    { label: m.value.flows.history, icon: History, run: () => openHistory(flow) },
    { label: m.value.flows.duplicate, icon: Copy, disabled: pending.value, run: () => handleDuplicate(flow.id) },
    { label: m.value.flows.export, icon: Download, disabled: pending.value, run: () => handleExport(flow.id, flow.name) },
    { label: m.value.flows.deleteFlow, icon: Trash2, danger: true, separated: true, disabled: pending.value, run: () => deleteFlow(flow.id) }
  ]
}

// A new flow gets its name first, so the list never fills with
// "Untitled Flow"; it is created when it is first saved.
const showNew = ref(false)
const newName = ref('')
async function openNew() {
  newName.value = ''
  showNew.value = true
  await nextTick()
  ;(document.getElementById('new-flow-name') as HTMLInputElement | null)?.focus()
}
function createNewFlow() {
  const name = newName.value.trim()
  if (!name) return
  showNew.value = false
  router.push({ path: '/editor', query: { name } })
}
</script>

<template>
  <main class="lab-page mx-auto max-w-7xl px-4 py-6 sm:px-8 sm:py-8">
    <!-- Title, at a glance, and the ways to make a flow -->
    <section class="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <h1 class="text-2xl font-semibold sm:text-3xl">{{ m.flows.title }}</h1>
        <p class="mt-2 flex flex-wrap gap-2 text-sm" aria-live="polite">
          <Badge variant="secondary">{{ m.flows.total(flowsStore.flows.length) }}</Badge>
          <Badge variant="secondary" :class="runningCount ? 'bg-primary/15 text-primary' : ''">{{ m.flows.running(runningCount) }}</Badge>
          <Badge v-if="failingCount" variant="destructive">{{ m.flows.withErrors(failingCount) }}</Badge>
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button variant="outline" :disabled="pending" :title="m.flows.importHint" @click="triggerImport">
          <Upload class="mr-2 h-4 w-4" />{{ m.flows.import }}
        </Button>
        <Button variant="outline" :title="m.flows.examplesHint" @click="showExamples = true">
          <LayoutTemplate class="mr-2 h-4 w-4" />{{ m.flows.examples }}
        </Button>
        <Button @click="openNew">
          <Plus class="mr-2 h-4 w-4" />{{ m.flows.newFlow }}
        </Button>
      </div>
    </section>
    <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />

    <!-- Search -->
    <div v-if="flowsStore.flows.length" class="mb-6 flex items-center gap-4">
      <div class="relative max-w-md flex-1">
        <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input v-model="searchQuery" :aria-label="m.flows.search" :placeholder="m.flows.searchPlaceholder" class="pl-9" />
      </div>
      <div v-if="searchQuery" class="text-sm text-muted-foreground">{{ m.flows.matches(displayedFlows.length) }}</div>
    </div>

    <!-- Loading State -->
    <div v-if="flowsStore.loading && !flowsStore.flows.length" class="flex items-center justify-center py-12" role="status" :aria-label="m.common.loading">
      <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
    </div>

    <!-- Error State -->
    <div v-else-if="flowsStore.error && !flowsStore.flows.length" class="rounded-lg border border-destructive bg-destructive/10 p-4 text-center">
      <p class="text-destructive">{{ m.flows.loadFailed }}</p>
      <Button variant="outline" class="mt-2" @click="flowsStore.fetchFlows">{{ m.common.retry }}</Button>
    </div>

    <!-- Empty State -->
    <div v-else-if="displayedFlows.length === 0" class="lab-empty rounded-xl border border-dashed px-6 py-16 text-center">
      <Workflow class="mx-auto h-16 w-16 text-primary/70" />
      <h2 class="mt-4 text-lg font-medium">{{ searchQuery ? m.flows.noMatches : m.flows.empty }}</h2>
      <p class="mx-auto mt-2 max-w-md text-muted-foreground">{{ searchQuery ? m.flows.noMatchesHint : m.flows.emptyHint }}</p>
      <Button v-if="searchQuery" class="mt-4" variant="outline" @click="searchQuery = ''">{{ m.flows.clearSearch }}</Button>
      <div v-else class="mt-4 flex flex-wrap justify-center gap-2">
        <Button @click="openNew"><Plus class="mr-2 h-4 w-4" />{{ m.flows.createFlow }}</Button>
        <Button variant="outline" @click="showExamples = true"><LayoutTemplate class="mr-2 h-4 w-4" />{{ m.flows.startFromExample }}</Button>
      </div>
    </div>

    <!-- Flows Grid -->
    <ul v-else class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <li v-for="flow in displayedFlows" :key="flow.id">
        <Card class="flex h-full flex-col transition-colors hover:border-primary/50 hover:shadow-md">
          <div class="flex flex-1 flex-col p-4">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 flex-1">
                <!-- Agents may name and describe flows (approved drafts): shown as
                     text, hidden characters revealed, here and in every label. -->
                <h2 class="truncate font-semibold"><RouterLink :to="`/editor/${flow.id}`" class="hover:underline"><AgentText :text="flow.name" /></RouterLink></h2>
                <p v-if="flow.description" class="mt-1 line-clamp-2 text-sm text-muted-foreground"><AgentText :text="flow.description" /></p>
              </div>
              <label class="flex shrink-0 items-center gap-2 text-xs font-medium" :class="flow.running ? 'text-primary' : flow.state === 'failed' ? 'text-destructive' : 'text-muted-foreground'">
                {{ flow.running ? m.editor.running : flow.state === 'failed' ? m.editor.failed : m.editor.stoppedState }}
                <Switch
                  :disabled="pending || (!flow.running && flow.nodeCount === 0)" :title="!flow.running && flow.nodeCount === 0 ? m.editor.deployEmpty : undefined" :aria-label="flow.running ? m.flows.stopFlow(visibleText(flow.name)) : m.flows.deployFlow(visibleText(flow.name))"
                  :model-value="flow.running"
                  @update:model-value="(v) => toggleFlow(flow.id, v)"
                />
              </label>
            </div>

            <!-- What it is, and how it has been doing -->
            <dl class="mt-4 space-y-1.5 text-xs text-muted-foreground">
              <div class="flex items-center gap-1.5">
                <Workflow class="h-3.5 w-3.5" aria-hidden="true" />
                <dt class="sr-only">{{ m.flows.size }}</dt>
                <dd>{{ m.flows.nodes(flow.nodeCount) }} · <span :title="formatDate(flow.updatedAt, { dateStyle: 'medium', timeStyle: 'short' })">{{ m.flows.edited(formatRelative(flow.updatedAt)) }}</span></dd>
              </div>
              <div class="flex items-center gap-1.5">
                <Clock class="h-3.5 w-3.5" aria-hidden="true" />
                <dt class="sr-only">{{ m.flows.lastRunLabel }}</dt>
                <dd v-if="activity[flow.id] === undefined">…</dd>
                <dd v-else-if="activity[flow.id] === null">{{ m.flows.runsUnknown }}</dd>
                <dd v-else-if="!activity[flow.id]!.lastRunAt">{{ m.flows.neverRun }}</dd>
                <dd v-else>{{ m.flows.lastRun(formatRelative(activity[flow.id]!.lastRunAt!)) }}</dd>
              </div>
              <div v-if="activity[flow.id]?.failed" class="flex items-center gap-1.5 text-destructive">
                <AlertTriangle class="h-3.5 w-3.5" aria-hidden="true" />
                <dt class="sr-only">{{ m.flows.errorsLabel }}</dt>
                <dd>{{ m.flows.recentErrors(activity[flow.id]!.failed, activity[flow.id]!.total) }}</dd>
              </div>
            </dl>

            <!-- Agent marks (Deployed by agent, Changed by agent) -->
            <div v-if="flow.agentChanged || flow.deployment?.actorKind === 'agent'" class="mt-3 flex flex-wrap gap-2">
              <FlowMarks :flow="flow" />
            </div>

            <div class="min-h-4 flex-1" />
            <div class="flex items-center gap-2 border-t pt-3">
              <Button variant="outline" size="sm" class="flex-1" @click="router.push(`/editor/${flow.id}`)">
                <Edit3 class="mr-2 h-4 w-4" />{{ m.flows.edit }}
              </Button>
              <Menu :label="m.flows.moreActions(visibleText(flow.name))" :actions="actions(flow)" />
            </div>
          </div>
        </Card>
      </li>
    </ul>

    <ConfirmDialog ref="confirmation" />
    <VersionHistoryDialog
      v-if="historyFlow"
      v-model:open="historyOpen"
      :flow-id="historyFlow.id"
      :flow-name="historyFlow.name"
      :running="historyFlow.running"
    />
    <ExamplesDialog v-model:open="showExamples" @manage-connections="shell.showBrokers = true" />
    <Dialog v-model:open="showNew" :title="m.flows.newFlow">
      <form class="space-y-4" @submit.prevent="createNewFlow">
        <div>
          <h2 class="text-lg font-semibold">{{ m.flows.newFlow }}</h2>
          <p class="mt-1 text-sm text-muted-foreground">{{ m.flows.newFlowHint }}</p>
        </div>
        <div>
          <label for="new-flow-name" class="text-sm font-medium">{{ m.editor.name }}</label>
          <Input id="new-flow-name" v-model="newName" class="mt-1" :placeholder="m.flows.newFlowPlaceholder" required maxlength="120" />
        </div>
        <div class="flex justify-end gap-2">
          <Button type="button" variant="outline" @click="showNew = false">{{ m.common.cancel }}</Button>
          <Button type="submit" :disabled="!newName.trim()">{{ m.flows.create }}</Button>
        </div>
      </form>
    </Dialog>

    <!-- Hidden file input for flow import -->
    <input ref="fileInput" type="file" accept="application/json,.json" class="hidden" @change="onImportFile" />
  </main>
</template>
