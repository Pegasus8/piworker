<script setup lang="ts">
// The approval queue (docs/design/mcp.md §9): tool calls with confirm "admin"
// and agents' draft deploys wait here. Arguments, summaries and results come
// from agents or flows and are rendered as plain text only.
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
import { approvalRisk, countHidden, formatDateTime, formatRelative, jsonText, visibleText, type AgentApproval, type ApprovalDecision } from '@/lib/agentAccess'

const agent = useAgentStore()
const auth = useAuthStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const feedback = ref<Feedback | null>(null)
const pending = ref(false)
const loading = ref(false)
const loadError = ref('')
const filter = ref<ApprovalFilter>('pending')
const now = ref(new Date())
const filters = [
  { value: 'pending', label: 'Waiting' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'expired', label: 'Expired' },
  { value: 'all', label: 'All' }
]

// Loads can overlap (filter, refresh, new requests); the store shows only the
// latest list, and the page stays loading until none is in flight.
let loads = 0
async function load() {
  if (!auth.authEnabled) return
  loads++
  loading.value = true
  loadError.value = ''
  try {
    await agent.fetchApprovals(filter.value)
  } catch (e) {
    loadError.value = apiErrorMessage(e, 'Could not load the approvals.')
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
watch(filter, load)
// New requests arrive through the badge's polling.
watch(() => agent.pendingCount, (count, before) => {
  if (filter.value === 'pending' && count !== before && !pending.value) void load()
})

const isTool = (a: AgentApproval) => a.kind === 'tool_call'
const toolOf = (a: AgentApproval) => isTool(a) ? agent.toolFor(a.flowId, a.ref) : undefined
const title = (a: AgentApproval) => visibleText(toolOf(a)?.toolName ?? a.ref)
const flowName = (a: AgentApproval) => a.flowId ? (toolOf(a)?.flowName ?? agent.flowNames[a.flowId] ?? a.flowId) : ''
const kindLabel = (a: AgentApproval) => isTool(a) ? 'Tool call' : a.kind === 'draft_deploy' ? 'Draft deploy' : a.kind
const statusLabel: Record<string, string> = { pending: 'Waiting', approved: 'Approved', rejected: 'Rejected', expired: 'Expired' }
const tokenStatusLabel: Record<string, string> = { active: 'active', expired: 'token expired', revoked: 'token revoked', unknown: 'token deleted' }
const riskVariant = (risk: string) => risk === 'privileged' || risk === 'unknown' ? 'destructive' : risk === 'external' ? 'secondary' : 'outline'
const riskOf = (a: AgentApproval) => approvalRisk(a.kind, toolOf(a), !!agent.toolsError)
const canApprove = (a: AgentApproval) => a.status === 'pending' && isTool(a) && a.tokenStatus === 'active'
const hasArgs = (a: AgentApproval) => a.args !== undefined && a.args !== null
// Invisible or direction-changing characters in what the agent sent.
const hiddenCount = (a: AgentApproval) => countHidden(a.summary ?? '') + countHidden(jsonText(a.args))
const hiddenLabel = (n: number) => `${n} hidden character${n === 1 ? '' : 's'}`

function outcome(d: ApprovalDecision): string {
  if (d.approval.status === 'rejected') return 'Rejected.'
  if (d.executed) return 'Approved and run.'
  // Execution errors can echo what the agent sent.
  return d.executionError ? `Approved, but it did not run: ${visibleText(d.executionError)}` : 'Approved.'
}

async function decide(a: AgentApproval, approve: boolean) {
  if (pending.value) return
  const hidden = hiddenCount(a)
  const warning = hidden ? ` The request contains ${hiddenLabel(hidden)}, shown highlighted as \\u escapes: check what they change.` : ''
  const asked = approve
    ? await confirmation.value?.ask('Approve and run?', `“${title(a)}” runs now with the arguments shown, on behalf of the token “${a.tokenName || a.tokenId}”.${warning}`, 'Approve and run')
    : await confirmation.value?.ask('Reject request?', `“${title(a)}” will not run. The agent is told the request was rejected.`, 'Reject request')
  if (!asked) return
  pending.value = true
  feedback.value = { kind: 'pending', text: 'Working…' }
  try {
    const d = await agent.decide(a.id, approve)
    feedback.value = { kind: d.approval.status === 'approved' && !d.executed ? 'error' : 'success', text: `${title(a)}: ${outcome(d)}` }
  } catch (e) {
    feedback.value = { kind: 'error', text: apiErrorMessage(e, 'Could not record the decision. Try again.') }
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
      <OperationFeedback :message="feedback" @dismiss="feedback = null" />

      <template v-if="auth.authEnabled">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-lg font-semibold">Approvals</h2>
            <p class="text-sm text-muted-foreground">Tools that ask you first wait here up to 15 minutes. Check the arguments before you approve.</p>
          </div>
          <div class="flex items-end gap-2">
            <div class="space-y-1">
              <Label for="approval-filter" class="text-xs">Show</Label>
              <Select id="approval-filter" v-model="filter" aria-label="Approval status" :options="filters" class="h-9 w-36" />
            </div>
            <Button variant="outline" size="icon" aria-label="Refresh" :disabled="loading" @click="load"><RefreshCw class="h-4 w-4" /></Button>
          </div>
        </div>

        <Card v-if="decisions.length" class="space-y-3 p-4">
          <h3 class="font-semibold">Decided just now</h3>
          <ul class="space-y-3">
            <li v-for="d in decisions" :key="d.approval.id" class="space-y-1 text-sm">
              <p><span class="break-all font-mono font-medium">{{ title(d.approval) }}</span> — {{ outcome(d) }}</p>
              <pre v-if="d.approval.result !== undefined" aria-label="Result" class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-2 text-xs"><AgentText :text="jsonText(d.approval.result)" /></pre>
            </li>
          </ul>
        </Card>

        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
        <p v-else-if="!loading && !agent.approvals.length" class="rounded-md bg-muted p-6 text-center text-sm text-muted-foreground">
          {{ filter === 'pending' ? 'Nothing is waiting for your approval.' : 'No approvals to show.' }}
        </p>

        <article v-for="a in agent.approvals" :key="a.id" :aria-label="`Request ${title(a)}`" class="space-y-3 rounded-lg border bg-card p-4 shadow-sm">
          <div class="flex flex-wrap items-center gap-2">
            <Badge variant="outline">{{ kindLabel(a) }}</Badge>
            <h3 class="break-all font-mono font-semibold">{{ title(a) }}</h3>
            <Badge :variant="a.status === 'pending' ? 'default' : 'secondary'">{{ statusLabel[a.status] ?? a.status }}</Badge>
            <Badge v-if="riskOf(a)" :variant="riskVariant(riskOf(a)!.risk)" :title="riskOf(a)!.reason">{{ riskOf(a)!.label }}</Badge>
            <Badge v-if="hiddenCount(a)" variant="destructive" title="Invisible or direction-changing characters, shown highlighted as \u escapes">{{ hiddenLabel(hiddenCount(a)) }}</Badge>
          </div>
          <dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
            <div v-if="flowName(a)"><dt class="text-xs text-muted-foreground">Flow</dt><dd class="break-words">{{ flowName(a) }}</dd></div>
            <div>
              <dt class="text-xs text-muted-foreground">Token</dt>
              <dd class="flex flex-wrap items-center gap-2">
                <span class="break-words">{{ a.tokenName || a.tokenId }}</span>
                <Badge v-if="a.tokenStatus !== 'active'" variant="destructive">{{ tokenStatusLabel[a.tokenStatus] ?? a.tokenStatus }}</Badge>
              </dd>
            </div>
            <div><dt class="text-xs text-muted-foreground">Requested</dt><dd :title="formatDateTime(a.createdAt)">{{ formatRelative(a.createdAt, now) }}</dd></div>
            <div v-if="a.status === 'pending'"><dt class="text-xs text-muted-foreground">Expires</dt><dd :title="formatDateTime(a.expiresAt)">{{ formatRelative(a.expiresAt, now) }}</dd></div>
            <div v-else-if="a.decidedAt"><dt class="text-xs text-muted-foreground">Decided</dt><dd :title="formatDateTime(a.decidedAt)">{{ formatRelative(a.decidedAt, now) }}{{ a.decidedBy ? ` by ${a.decidedBy}` : '' }}</dd></div>
          </dl>
          <p v-if="riskOf(a)?.risk === 'unknown'" class="text-sm text-muted-foreground">{{ riskOf(a)!.reason }}</p>
          <div v-if="a.summary" class="space-y-1">
            <p class="text-xs text-muted-foreground">Summary (from the request, shown as text)</p>
            <p aria-label="Summary" class="whitespace-pre-wrap break-words rounded-md border p-2 text-sm"><AgentText :text="a.summary" /></p>
          </div>
          <div v-if="hasArgs(a)" class="space-y-1">
            <p class="text-xs text-muted-foreground">Arguments sent by the agent</p>
            <pre aria-label="Arguments" dir="ltr" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs [unicode-bidi:bidi-override]"><AgentText :text="jsonText(a.args)" /></pre>
          </div>
          <div v-if="a.result !== undefined" class="space-y-1">
            <p class="text-xs text-muted-foreground">Result</p>
            <pre aria-label="Result" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs"><AgentText :text="jsonText(a.result)" /></pre>
          </div>
          <p v-if="a.kind === 'draft_deploy'" class="text-sm">
            <span class="text-muted-foreground" title="The draft review page arrives in a later version">Review draft (coming soon)</span>
            — approve draft deploys after reviewing the changes; until then you can reject them here.
          </p>
          <p v-else-if="a.status === 'pending' && a.tokenStatus !== 'active'" class="text-sm text-muted-foreground">
            The token that asked for this is no longer active, so the request can only be rejected.
          </p>
          <div v-if="a.status === 'pending'" class="flex flex-wrap gap-2">
            <Button :disabled="pending || !canApprove(a)" @click="decide(a, true)">Approve…</Button>
            <Button variant="outline" :disabled="pending" @click="decide(a, false)">Reject…</Button>
          </div>
        </article>
      </template>
    </div>
    <ConfirmDialog ref="confirmation" />
  </main>
</template>
