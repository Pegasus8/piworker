<script setup lang="ts">
// The flow assistant (docs/design/llm.md §7–§8, ADR-0041): an LLM
// connection turns a description into a proposed flow, which the admin
// reviews here (nodes with their risk, connections, the model's notes and
// what Lint still finds) and places on the canvas as one undo step. It
// never saves or deploys. The model's text is shown as plain text with
// hidden characters revealed (AgentText). Closing the dialog while the
// model works cancels the request. A change of the current flow is not
// applied once the canvas changed since it was sent (it would undo those
// edits): it is proposed again.
import { computed, onUnmounted, ref, watch } from 'vue'
import { Button, Dialog, Label, Select, Switch, Textarea, Input } from '@/components/ui'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import RiskBadge from '@/components/agent/RiskBadge.vue'
import AgentText from '@/components/agent/AgentText.vue'
import FlowIssueItem from '@/components/agent/FlowIssueItem.vue'
import FieldHint from './FieldHint.vue'
import { Cable, Loader2, Sparkles } from 'lucide-vue-next'
import type { Feedback } from '@/composables/useFeedback'
import { useApi } from '@/composables/useApi'
import { useSuggestionContext } from '@/composables/useSuggestionContext'
import { useFlowsStore } from '@/stores/flows'
import { useLLMStore } from '@/stores/llm'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import {
  MAX_DESCRIPTION_CHARS, applyMode, assistantError, canvasFingerprint, changeSummary, currentDefinition, descriptionLength, issueLocation, proposalView, sentFingerprint
} from '@/lib/assistant'
import { suggestionHint, suggestions } from '@/lib/suggestions'
import { connectionLabel } from '@/lib/llm'
import { issueCounts, issueCountsLabel } from '@/lib/drafts'
import { nodeIcon } from '@/lib/nodeIcons'
import { nodeRisk } from '@/lib/nodeRisk'
import { visibleText } from '@/lib/agentAccess'
import type { AssistantFlowRequest, AssistantFlowResult } from '@/types'
import { formatNumber, messages as m } from '@/i18n'

// manage-connections: open the LLM connections dialog (none is defined yet).
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; applied: [Feedback]; 'manage-connections': [] }>()

const api = useApi()
const flows = useFlowsStore()
const llm = useLLMStore()
const nodeTypes = useNodeTypesStore()
const t = computed(() => m.value.assistant)

// --- The form ---------------------------------------------------------------

const connection = ref('')
const model = ref('')
const description = ref('')
const changeCurrent = ref(false)
const loadingConnections = ref(false)
const connectionsFailed = ref(false)

async function loadConnections() {
  loadingConnections.value = true
  connectionsFailed.value = !await llm.loadSuggestions({ llmConnections: true, models: connection.value, force: true })
  loadingConnections.value = false
}

// The chosen connection stays a defined one: the first by default.
watch(() => llm.connections.map(c => c.name), names => {
  if (!names.includes(connection.value)) connection.value = names[0] ?? ''
}, { immediate: true })
// Another connection has other models: its default until one is chosen.
watch(connection, (name, previous) => {
  if (previous) model.value = ''
  if (name) void llm.loadModels(name)
}, { immediate: true })

const noConnections = computed(() => llm.connectionsLoaded && !llm.connections.length)
const connectionOptions = computed(() => llm.connections.map(c => ({
  value: c.name,
  label: t.value.connectionOption(c.name, connectionLabel(c))
})))
const selected = computed(() => llm.connections.find(c => c.name === connection.value))

// The model field's suggestions and hint, as the node settings show them.
const suggestionContext = useSuggestionContext(() => ({ connection: connection.value }))
const modelSuggestions = computed(() => suggestions('llm-models', suggestionContext.value))
const modelHint = computed(() => suggestionHint('llm-models', suggestionContext.value, model.value))

const length = computed(() => descriptionLength(description.value))
const tooLong = computed(() => length.value > MAX_DESCRIPTION_CHARS)
const canChangeCurrent = computed(() => flows.nodes.length > 0)
const canGenerate = computed(() => !!selected.value && length.value > 0 && !tooLong.value && !!(model.value.trim() || selected.value.model))

// --- The request ------------------------------------------------------------

const running = ref(false)
const elapsed = ref(0)
const notice = ref<Feedback | null>(null)
const result = ref<AssistantFlowResult | null>(null)
// Whether the proposal is a change of the current flow (it then replaces the canvas).
const resultChanges = ref(false)
// The canvas a change of the current flow was built from (canvasFingerprint).
const sentCanvas = ref('')
// Placed on the canvas: the next opening starts from the form (the
// proposal stays on screen while the dialog closes).
let placed = false

watch(() => props.open, open => {
  if (!open) return
  if (placed) {
    placed = false
    result.value = null
    notice.value = null
  }
  void loadConnections()
  // The proposal is converted with the node types (labels, icons, ports).
  if (!nodeTypes.initialized) void nodeTypes.fetchNodeTypes()
}, { immediate: true })

let controller: AbortController | undefined
let timer: ReturnType<typeof setInterval> | undefined

function stopTimer() {
  clearInterval(timer)
  timer = undefined
}

async function generate() {
  if (!canGenerate.value || running.value) return
  const changesCurrent = changeCurrent.value && canChangeCurrent.value
  const body: AssistantFlowRequest = { connection: connection.value, description: description.value.trim() }
  if (model.value.trim()) body.model = model.value.trim()
  if (changesCurrent) body.flow = currentDefinition(flows.currentFlow, flows.nodes, flows.edges)
  const sent = body.flow ? sentFingerprint(body.flow) : ''
  const request = new AbortController()
  controller = request
  running.value = true
  notice.value = null
  result.value = null
  elapsed.value = 0
  const started = Date.now()
  timer = setInterval(() => { elapsed.value = Math.floor((Date.now() - started) / 1000) }, 1000)
  try {
    const answer = await api.assistFlow(body, request.signal)
    if (controller !== request) return
    result.value = answer
    resultChanges.value = changesCurrent
    sentCanvas.value = sent
  } catch (e) {
    if (controller !== request) return
    notice.value = { kind: request.signal.aborted ? 'warning' : 'error', text: assistantError(e, request.signal.aborted) }
  } finally {
    if (controller === request) {
      controller = undefined
      running.value = false
      stopTimer()
    }
  }
}

function cancel() {
  controller?.abort()
}

function setOpen(open: boolean) {
  if (!open) cancel()
  emit('update:open', open)
}

onUnmounted(() => {
  controller?.abort()
  stopTimer()
})

// --- The proposal -----------------------------------------------------------

const view = computed(() => result.value ? proposalView(result.value.flow, nodeTypes.getById) : null)
const mode = computed(() => applyMode(flows.nodes.length, resultChanges.value))
const changes = computed(() => view.value && mode.value === 'replace' && flows.nodes.length ? changeSummary(flows.nodes, view.value.nodes.filter(n => n.known)) : null)
const counts = computed(() => issueCounts(result.value?.issues))
const typesUnavailable = computed(() => !nodeTypes.initialized && !nodeTypes.loading)
const applyLabel = computed(() => !flows.nodes.length ? t.value.apply : mode.value === 'replace' ? t.value.applyChanges : t.value.addToCanvas)
const applyHint = computed(() => !flows.nodes.length ? t.value.applyHints.replace : mode.value === 'replace' ? t.value.applyHints.change : t.value.applyHints.add)
// A change of the current flow replaces the whole canvas with what the
// model built from it as it was sent: once the canvas changed (the dialog
// was closed and nodes edited, or another flow opened), applying it would
// silently undo those edits, so it is refused and can be proposed again.
// Checked while the dialog is open (closed, it keeps its content mounted:
// the canvas is not compared on every edit).
const stale = computed(() => props.open && !!result.value && resultChanges.value && canvasFingerprint(flows.nodes, flows.edges) !== sentCanvas.value)
const canApply = computed(() => !!view.value && nodeTypes.initialized && view.value.nodes.some(n => n.known) && !stale.value)

function apply() {
  if (!result.value || !canApply.value) return
  const applied = flows.applyProposal(result.value.flow, mode.value)
  if (!applied.nodes) {
    notice.value = { kind: 'error', text: t.value.nothingToApply }
    return
  }
  emit('applied', applied.skippedTypes.length
    ? { kind: 'warning', text: t.value.appliedSkipped(applied.nodes, applied.connections, applied.skippedTypes.map(visibleText)) }
    : { kind: 'success', text: t.value.applied(applied.nodes, applied.connections) })
  // The description stays for another try.
  placed = true
  emit('update:open', false)
}

function edit() {
  result.value = null
  notice.value = null
}
</script>

<template>
  <Dialog :open="open" :title="t.title" class="max-w-2xl" @update:open="setOpen">
    <div class="space-y-4">
      <div class="flex items-start gap-2">
        <Sparkles class="mt-1 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
        <div>
          <h2 class="text-lg font-semibold">{{ t.title }}</h2>
          <p class="text-sm text-muted-foreground">{{ t.intro }}</p>
        </div>
      </div>

      <OperationFeedback :message="notice" @dismiss="notice = null" />

      <!-- The proposal -->
      <section v-if="result && view" :aria-label="t.proposal" class="space-y-4">
        <div class="rounded-lg border p-3">
          <p class="text-xs font-medium uppercase tracking-wide text-muted-foreground">{{ t.proposal }}</p>
          <h3 class="break-words font-semibold"><AgentText :text="result.flow.name || t.untitled" /></h3>
          <p v-if="result.flow.description" class="mt-1 whitespace-pre-wrap break-words text-sm text-muted-foreground"><AgentText :text="result.flow.description" /></p>
          <p class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <span>{{ t.nodes(view.nodes.length) }} · {{ t.connections(result.flow.connections.length) }}</span>
            <span v-if="view.risk" class="flex items-center gap-1">{{ t.highestRisk }}: <RiskBadge :risk="view.risk" /></span>
          </p>
          <p v-if="changes" class="mt-1 text-sm">{{ t.changes(changes.added, changes.removed, changes.kept) }}</p>
        </div>

        <ul v-if="view.unknownTypes.length || view.skippedConnections" class="space-y-1 text-sm" role="status">
          <li v-for="type in view.unknownTypes" :key="type" class="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2"><AgentText :text="t.unknownType(type)" /></li>
          <li v-if="view.skippedConnections" class="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2">{{ t.skippedConnections(view.skippedConnections) }}</li>
        </ul>

        <div>
          <h4 class="mb-2 text-sm font-semibold">{{ t.nodesHeading }}</h4>
          <ul class="max-h-64 divide-y overflow-auto rounded-md border">
            <li v-for="(n, i) in view.nodes" :key="`${i}-${n.id}`" class="flex items-start gap-2 px-3 py-2 text-sm" :class="n.known ? '' : 'opacity-60'">
              <component :is="nodeIcon(n.icon)" class="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
              <div class="min-w-0 flex-1">
                <p class="break-words"><span class="font-medium"><AgentText :text="n.label" /></span> <span class="break-all font-mono text-xs text-muted-foreground"><AgentText :text="n.id" /></span></p>
                <p v-if="n.known && n.label !== n.type" class="break-all font-mono text-xs text-muted-foreground">{{ n.type }}</p>
                <p v-if="n.riskCanRise" class="text-xs text-muted-foreground">{{ t.riskCanRise }}</p>
              </div>
              <span v-if="!n.enabled" class="shrink-0 text-xs text-muted-foreground">{{ t.disabled }}</span>
              <RiskBadge v-if="n.known" :risk="n.risk" :title="nodeRisk(n.risk).hint" class="shrink-0" />
            </li>
          </ul>
        </div>

        <div v-if="result.notes.trim()">
          <h4 class="mb-1 text-sm font-semibold">{{ t.notes }}</h4>
          <p class="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-md border p-3 text-sm"><AgentText :text="result.notes" /></p>
        </div>

        <div>
          <h4 class="mb-1 text-sm font-semibold">{{ t.issues }} · {{ issueCountsLabel(counts) }}</h4>
          <p v-if="!result.issues.length" class="text-sm text-muted-foreground">{{ t.noIssues }}</p>
          <template v-else>
            <p class="mb-2 text-xs text-muted-foreground">{{ t.issuesHint }}</p>
            <ul class="max-h-48 space-y-1 overflow-auto text-sm">
              <FlowIssueItem v-for="(issue, i) in result.issues" :key="`${i}-${issue.path}-${issue.code}`" :issue="issue" :where="issueLocation(issue, result.flow, nodeTypes.getById)" />
            </ul>
          </template>
        </div>

        <p v-if="result.usage.requests" class="text-xs text-muted-foreground">{{ t.usage(formatNumber(result.usage.inputTokens), formatNumber(result.usage.outputTokens), result.usage.requests) }}</p>

        <p v-if="typesUnavailable" role="alert" class="text-sm text-destructive">{{ t.nodeTypesUnavailable }}</p>
        <p v-else-if="!view.nodes.some(n => n.known)" role="alert" class="text-sm text-destructive">{{ t.nothingToApply }}</p>
        <p v-else-if="stale" role="alert" class="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2 text-sm">{{ t.stale }}</p>
        <p v-else class="text-sm text-muted-foreground">{{ applyHint }} {{ t.undoHint }}</p>

        <div class="flex flex-wrap justify-end gap-2">
          <Button variant="outline" @click="setOpen(false)">{{ m.common.close }}</Button>
          <Button variant="outline" @click="edit">{{ t.edit }}</Button>
          <Button v-if="stale" :disabled="!canGenerate" @click="generate"><Sparkles class="mr-2 h-4 w-4" aria-hidden="true" />{{ t.proposeAgain }}</Button>
          <Button v-else :disabled="!canApply" @click="apply">{{ applyLabel }}</Button>
        </div>
      </section>

      <!-- The form -->
      <template v-else>
        <p v-if="loadingConnections && !llm.connectionsLoaded" role="status" class="text-sm text-muted-foreground">{{ t.loadingConnections }}</p>
        <p v-else-if="connectionsFailed && !llm.connections.length" role="alert" class="text-sm text-destructive">
          {{ t.connectionsFailed }} <Button size="sm" variant="outline" @click="loadConnections">{{ m.common.retry }}</Button>
        </p>

        <!-- Nothing to ask: say how to add a connection. -->
        <div v-else-if="noConnections" class="space-y-2 rounded-lg border border-dashed p-4 text-sm" role="status">
          <p class="font-medium">{{ t.noConnections }}</p>
          <p class="text-muted-foreground">{{ t.noConnectionsBody }}</p>
          <Button size="sm" variant="outline" @click="emit('manage-connections')"><Cable class="mr-2 h-4 w-4" aria-hidden="true" />{{ t.manageConnections }}</Button>
        </div>

        <form v-else class="space-y-4" @submit.prevent="generate">
          <fieldset :disabled="running" class="space-y-4">
            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <Label for="assistant-connection">{{ t.connection }}</Label>
                <Select id="assistant-connection" v-model="connection" class="mt-1" :options="connectionOptions" :placeholder="t.chooseConnection" />
              </div>
              <div>
                <Label for="assistant-model">{{ t.model }}</Label>
                <Input id="assistant-model" v-model="model" class="mt-1" list="assistant-models" autocomplete="off"
                  :placeholder="selected?.model ? t.modelDefault(selected.model) : ''"
                  :aria-describedby="modelHint.text ? 'assistant-model-hint' : undefined" />
                <datalist id="assistant-models">
                  <option v-for="s in modelSuggestions" :key="s.value" :value="s.value" :label="s.label" />
                </datalist>
                <FieldHint id="assistant-model-hint" :hint="modelHint" info-class="text-muted-foreground" />
              </div>
            </div>

            <div>
              <Label for="assistant-description">{{ t.description }}</Label>
              <Textarea id="assistant-description" v-model="description" class="mt-1 font-sans" :rows="5" :placeholder="t.descriptionPlaceholder"
                aria-describedby="assistant-description-count" />
              <p id="assistant-description-count" class="mt-1 text-xs" :class="tooLong ? 'text-destructive' : 'text-muted-foreground'">
                {{ tooLong ? t.descriptionTooLong(MAX_DESCRIPTION_CHARS) : t.descriptionCount(length, MAX_DESCRIPTION_CHARS) }}
              </p>
            </div>

            <div v-if="canChangeCurrent" class="flex items-start gap-3">
              <Switch id="assistant-change" v-model="changeCurrent" aria-describedby="assistant-change-hint" class="mt-0.5" />
              <div>
                <Label for="assistant-change">{{ t.changeCurrent }}</Label>
                <p id="assistant-change-hint" class="text-xs text-muted-foreground">{{ t.changeCurrentHint }}</p>
              </div>
            </div>

            <p class="text-xs text-muted-foreground">{{ t.privacy }}</p>
          </fieldset>

          <!-- While the model works: how long, and a way out. The seconds
               are not announced: a screen reader would read them every second. -->
          <div v-if="running" role="status" aria-live="polite" class="flex items-start gap-3 rounded-lg border border-primary/30 bg-primary/5 p-3 text-sm">
            <Loader2 class="mt-0.5 h-4 w-4 shrink-0 animate-spin text-primary" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <p class="font-medium">{{ t.generating }} <span class="font-normal text-muted-foreground" aria-hidden="true">{{ t.elapsed(elapsed) }}</span></p>
              <p class="text-xs text-muted-foreground">{{ t.progressHint }}</p>
            </div>
            <Button type="button" size="sm" variant="outline" @click="cancel">{{ m.common.cancel }}</Button>
          </div>

          <div class="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="outline" @click="setOpen(false)">{{ m.common.close }}</Button>
            <Button type="submit" :disabled="!canGenerate || running">
              <Loader2 v-if="running" class="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />
              <Sparkles v-else class="mr-2 h-4 w-4" aria-hidden="true" />
              {{ t.generate }}
            </Button>
          </div>
        </form>
      </template>
    </div>
  </Dialog>
</template>
