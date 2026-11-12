<script setup lang="ts">
// The approval queue (docs/design/mcp.md §9): tool calls with confirm "admin"
// wait here (agents' drafts have their own pages). Arguments, summaries and
// results come from agents or flows and are rendered as plain text only.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { Badge, Button, Card, Label, Select } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import AgentNav from '@/components/agent/AgentNav.vue'
import AgentText from '@/components/agent/AgentText.vue'
import type { Feedback } from '@/composables/useFeedback'
import { useAgentStore, apiErrorMessage, type ApprovalFilter } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import {
  approvalRisk, approvalToolChange, countHidden, formatDateTime, formatRelative, jsonText, visibleText,
  type AgentApproval, type ApprovalDecision
} from '@/lib/agentAccess'
import { messages as m } from '@/i18n'

const agent = useAgentStore()
const auth = useAuthStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const feedback = ref<Feedback | null>(null)
const pending = ref(false)
const loading = ref(false)
const loadError = ref('')
const filter = ref<ApprovalFilter>('pending')
const now = ref(new Date())
const filters = computed(() => (['pending', 'approved', 'rejected', 'expired', 'all'] as const)
  .map(value => ({ value, label: m.value.agents.approvals.filters[value] })))

// Loads can overlap (filter, refresh, new requests); the store shows only the
// latest list, and the page stays loading until none is in flight.
let loads = 0
async function load(opts: { background?: boolean } = {}) {
  if (!auth.authEnabled) return
  loads++
  loading.value = true
  loadError.value = ''
  try {
    await agent.fetchApprovals(filter.value, opts)
  } catch (e) {
    loadError.value = apiErrorMessage(e, m.value.agents.approvals.loadFailed)
  } finally {
    loading.value = --loads > 0
  }
}

let clock: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  clock = setInterval(() => { now.value = new Date() }, 30_000)
  await load()
  if (auth.authEnabled) await Promise.allSettled([agent.fetchTools(), agent.fetchFlowNames()])
})
onBeforeUnmount(() => clearInterval(clock))
watch(filter, () => load())
// New requests arrive through the badge's polling. Agents can change the count
// at will, so this reload is a background request: an open tab alone must not
// keep the session alive.
watch(() => agent.pendingCount, (count, before) => {
  if (filter.value === 'pending' && count !== before && !pending.value) void load({ background: true })
})

const toolOf = (a: AgentApproval) => agent.toolFor(a.flowId, a.ref)
// The tool name and risk recorded with the request are what approving runs
// (ADR-0015).
const title = (a: AgentApproval) => visibleText(a.toolName)
const flowName = (a: AgentApproval) => a.flowId ? (toolOf(a)?.flowName ?? agent.flowNames[a.flowId] ?? a.flowId) : ''
const statusLabel = (s: string): string | undefined => s === 'all' ? undefined : (m.value.agents.approvals.filters as Record<string, string>)[s]
const tokenStatusLabel = (s: string): string | undefined => (m.value.agents.approvals.tokenStatus as Record<string, string>)[s]
const riskVariant = (risk: string) => risk === 'privileged' || risk === 'unknown' ? 'destructive' : risk === 'external' ? 'secondary' : 'outline'
const riskOf = (a: AgentApproval) => approvalRisk(a)
// Why approving would run nothing ('' when it would run or cannot be told here).
const toolChange = (a: AgentApproval) => approvalToolChange(a, toolOf(a))
const canApprove = (a: AgentApproval) => a.status === 'pending' && a.tokenStatus === 'active' && !toolChange(a)
const hasArgs = (a: AgentApproval) => a.args !== undefined && a.args !== null
// Invisible or direction-changing characters in what the agent sent.
const hiddenCount = (a: AgentApproval) => countHidden(a.summary ?? '') + countHidden(jsonText(a.args))
const hiddenLabel = (n: number) => m.value.agents.approvals.hidden(n)

function outcome(d: ApprovalDecision): string {
  const t = m.value.agents.approvals
  if (d.approval.status === 'rejected') return t.rejected
  if (d.executed) return t.approvedRun
  // Execution errors can echo what the agent sent.
  return d.executionError ? t.approvedNotRun(visibleText(d.executionError)) : t.approved
}

async function decide(a: AgentApproval, approve: boolean) {
  if (pending.value) return
  const hidden = hiddenCount(a)
  const t = m.value.agents.approvals
  const warning = hidden ? t.hiddenWarning(hiddenLabel(hidden)) : ''
  const asked = approve
    ? await confirmation.value?.ask(t.approveTitle, t.approveBody(title(a), a.tokenName || a.tokenId, warning), t.approveAction)
    : await confirmation.value?.ask(t.rejectTitle, t.rejectBody(title(a)), t.rejectAction)
  if (!asked) return
  pending.value = true
  feedback.value = { kind: 'pending', text: m.value.common.working }
  try {
    const d = await agent.decide(a.id, approve)
    feedback.value = { kind: d.approval.status === 'approved' && !d.executed ? 'error' : 'success', text: `${title(a)}: ${outcome(d)}` }
  } catch (e) {
    feedback.value = { kind: 'error', text: apiErrorMessage(e, m.value.agents.approvals.decideFailed) }
    await load()
  } finally {
    pending.value = false
  }
}

const decisions = computed(() => agent.recentDecisions)
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-4xl space-y-6">
      <AgentNav />
      <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />

      <template v-if="auth.authEnabled">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-lg font-semibold">{{ m.agents.approvals.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.agents.approvals.intro }}</p>
          </div>
          <div class="flex items-end gap-2">
            <div class="space-y-1">
              <Label for="approval-filter" class="text-xs">{{ m.agents.approvals.show }}</Label>
              <Select id="approval-filter" v-model="filter" :aria-label="m.agents.approvals.statusFilter" :options="filters" class="h-9 w-36" />
            </div>
            <Button variant="outline" size="icon" :aria-label="m.agents.approvals.refresh" :disabled="loading" @click="load()"><RefreshCw class="h-4 w-4" /></Button>
          </div>
        </div>

        <Card v-if="decisions.length" class="space-y-3 p-4">
          <h3 class="font-semibold">{{ m.agents.approvals.decidedNow }}</h3>
          <ul class="space-y-3">
            <li v-for="d in decisions" :key="d.approval.id" class="space-y-1 text-sm">
              <p><span class="break-all font-mono font-medium">{{ title(d.approval) }}</span> — {{ outcome(d) }}</p>
              <pre v-if="d.approval.result !== undefined" :aria-label="m.agents.approvals.result" class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-2 text-xs"><AgentText :text="jsonText(d.approval.result)" /></pre>
            </li>
          </ul>
        </Card>

        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
        <p v-else-if="!loading && !agent.approvals.length" class="rounded-md bg-muted p-6 text-center text-sm text-muted-foreground">
          {{ filter === 'pending' ? m.agents.approvals.nothingWaiting : m.agents.approvals.none }}
        </p>

        <article v-for="a in agent.approvals" :key="a.id" :aria-label="m.agents.approvals.request(title(a))" class="space-y-3 rounded-lg border bg-card p-4 shadow-sm">
          <div class="flex flex-wrap items-center gap-2">
            <Badge variant="outline">{{ m.agents.approvals.toolCall }}</Badge>
            <h3 class="break-all font-mono font-semibold">{{ title(a) }}</h3>
            <Badge :variant="a.status === 'pending' ? 'default' : 'secondary'">{{ statusLabel(a.status) ?? a.status }}</Badge>
            <Badge :variant="riskVariant(riskOf(a).risk)" :title="riskOf(a).reason">{{ riskOf(a).label }}</Badge>
            <Badge v-if="hiddenCount(a)" variant="destructive" :title="m.agents.approvals.hiddenTitle">{{ hiddenLabel(hiddenCount(a)) }}</Badge>
          </div>
          <dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
            <div v-if="flowName(a)"><dt class="text-xs text-muted-foreground">{{ m.agents.approvals.flow }}</dt><dd class="break-words">{{ flowName(a) }}</dd></div>
            <div>
              <dt class="text-xs text-muted-foreground">{{ m.agents.approvals.token }}</dt>
              <dd class="flex flex-wrap items-center gap-2">
                <span class="break-words">{{ a.tokenName || a.tokenId }}</span>
                <Badge v-if="a.tokenStatus !== 'active'" variant="destructive">{{ tokenStatusLabel(a.tokenStatus) ?? a.tokenStatus }}</Badge>
              </dd>
            </div>
            <div><dt class="text-xs text-muted-foreground">{{ m.agents.approvals.requested }}</dt><dd :title="formatDateTime(a.createdAt)">{{ formatRelative(a.createdAt, now) }}</dd></div>
            <div v-if="a.status === 'pending'"><dt class="text-xs text-muted-foreground">{{ m.agents.approvals.expires }}</dt><dd :title="formatDateTime(a.expiresAt)">{{ formatRelative(a.expiresAt, now) }}</dd></div>
            <div v-else-if="a.decidedAt"><dt class="text-xs text-muted-foreground">{{ m.agents.approvals.decided }}</dt><dd :title="formatDateTime(a.decidedAt)">{{ formatRelative(a.decidedAt, now) }}{{ a.decidedBy ? m.agents.approvals.by(a.decidedBy) : '' }}</dd></div>
          </dl>
          <p v-if="riskOf(a).risk === 'unknown'" class="text-sm text-muted-foreground">{{ riskOf(a).reason }}</p>
          <div v-if="a.summary" class="space-y-1">
            <p class="text-xs text-muted-foreground">{{ m.agents.approvals.summaryLabel }}</p>
            <p :aria-label="m.agents.approvals.summary" class="whitespace-pre-wrap break-words rounded-md border p-2 text-sm"><AgentText :text="a.summary" /></p>
          </div>
          <div v-if="hasArgs(a)" class="space-y-1">
            <p class="text-xs text-muted-foreground">{{ m.agents.approvals.argsLabel }}</p>
            <pre :aria-label="m.agents.approvals.args" dir="ltr" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs [unicode-bidi:bidi-override]"><AgentText :text="jsonText(a.args)" /></pre>
          </div>
          <div v-if="a.result !== undefined" class="space-y-1">
            <p class="text-xs text-muted-foreground">{{ m.agents.approvals.result }}</p>
            <pre :aria-label="m.agents.approvals.result" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs"><AgentText :text="jsonText(a.result)" /></pre>
          </div>
          <p v-if="a.status === 'pending' && a.tokenStatus !== 'active'" class="text-sm text-muted-foreground">
            {{ m.agents.approvals.inactiveToken }}
          </p>
          <p v-else-if="toolChange(a)" role="note" class="text-sm text-amber-700 dark:text-amber-400">{{ toolChange(a) }}</p>
          <div v-if="a.status === 'pending'" class="flex flex-wrap gap-2">
            <Button :disabled="pending || !canApprove(a)" @click="decide(a, true)">{{ m.agents.approvals.approve }}</Button>
            <Button variant="outline" :disabled="pending" @click="decide(a, false)">{{ m.agents.approvals.reject }}</Button>
          </div>
        </article>
      </template>
    </div>
    <ConfirmDialog ref="confirmation" />
  </main>
</template>
