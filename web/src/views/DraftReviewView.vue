<script setup lang="ts">
// One agent draft (docs/design/mcp.md §10), in the order to review it: what
// it changes and whether it is still current, the agent's note, the risk
// before → after, the alerts first, the issues, the per-node and connection
// diff (Sensitive values hidden, destinations highlighted), the comparison
// with the version you last approved, and then approve (saving a new
// version, deploying it when asked or when the flow runs) or reject with a
// reason the agent sees. A decided draft says what it became, and that its
// diff is recomputed against the current version. After a decision, focus
// moves to its outcome. Everything the draft holds came from an agent and is
// rendered as text only (AgentText), never as HTML.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { AlertTriangle, ArrowLeft, ArrowRight, CheckCircle2, Info, RefreshCw } from 'lucide-vue-next'
import { Badge, Button, Label, Textarea } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import AgentNav from '@/components/agent/AgentNav.vue'
import AgentText from '@/components/agent/AgentText.vue'
import DraftAlerts from '@/components/agent/DraftAlerts.vue'
import DraftDiff from '@/components/agent/DraftDiff.vue'
import RiskBadge from '@/components/agent/RiskBadge.vue'
import type { Feedback } from '@/composables/useFeedback'
import { DraftActionError, useDraftsStore } from '@/stores/drafts'
import { useAgentStore, apiErrorMessage } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { countHidden, formatDateTime, formatRelative, jsonText, visibleText } from '@/lib/agentAccess'
import {
  applyOutcome, approvalBlock, approveConfirmText, diffBasis, draftIcon, draftState, draftVersionText, fieldPaths,
  issueCountsLabel, issueCounts, nodeNames, proposedSettings, riskChange, severityInfo, type ApplyResult, type FlowIssue
} from '@/lib/drafts'

/** The longest rejection reason the server accepts (service.MaxRejectReasonChars). */
const MAX_REASON = 500

const route = useRoute()
const drafts = useDraftsStore()
const agent = useAgentStore()
const auth = useAuthStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const heading = ref<HTMLElement>()
const outcome = ref<HTMLElement>()

const id = computed(() => String(route.params.id ?? ''))
const loading = ref(false)
const loadError = ref('')
const notFound = ref(false)
const pending = ref(false)
const feedback = ref<Feedback | null>(null)
const applied = ref<ApplyResult | null>(null)
const refusedIssues = ref<FlowIssue[]>([])
const deployNow = ref(false)
const reason = ref('')
const showFlow = ref(false)
const now = ref(new Date())

const d = computed(() => drafts.detail?.id === id.value ? drafts.detail : null)
const names = computed(() => nodeNames(d.value?.diff, d.value?.diffApproved))
const state = computed(() => d.value ? draftState(d.value) : null)
const basis = computed(() => d.value ? diffBasis(d.value) : null)
// Where an alert's values are: the field rows of its own comparison, or the
// proposed flow for a setting that did not change.
const fieldsNow = computed(() => fieldPaths(d.value?.diff))
const fieldsApproved = computed(() => fieldPaths(d.value?.diffApproved))
const proposed = computed(() => proposedSettings(d.value?.flow))
const isPending = computed(() => d.value?.status === 'pending')
// A running flow is always redeployed: the box is checked and locked then.
const willDeploy = computed(() => !!d.value && (d.value.running || deployNow.value))
const block = computed(() => d.value ? approvalBlock(d.value, willDeploy.value) : '')
const issues = computed(() => d.value?.issues ?? [])
const counts = computed(() => issueCounts(issues.value))
const risk = computed(() => d.value ? riskChange(String(d.value.diff.riskBefore), String(d.value.diff.riskAfter)) : null)
const noteHidden = computed(() => countHidden(d.value?.note ?? ''))
const flowJson = computed(() => showFlow.value && d.value ? jsonText(d.value.flow) : '')
// Issues a refused approval listed that the page does not show already.
const extraIssues = computed(() => refusedIssues.value.filter(r =>
  !issues.value.some(i => i.path === r.path && i.code === r.code && i.message === r.message)))
const issueWhere = (i: FlowIssue) => i.path || '/'

async function load(opts: { background?: boolean } = {}) {
  if (!auth.authEnabled || !id.value) return
  if (!opts.background) {
    loading.value = true
    loadError.value = ''
    notFound.value = false
  }
  try {
    await drafts.fetchDraft(id.value, opts)
  } catch (e) {
    if (opts.background) return
    const status = (e as { response?: { status?: number } })?.response?.status
    if (status === 404) notFound.value = true
    else loadError.value = apiErrorMessage(e, 'Could not load the draft.')
  } finally {
    if (!opts.background) loading.value = false
  }
}

let clock: ReturnType<typeof setInterval> | undefined
onBeforeUnmount(() => clearInterval(clock))
onMounted(async () => {
  clock = setInterval(() => { now.value = new Date() }, 30_000)
  await load()
  await nextTick()
  heading.value?.focus()
})
watch(id, async () => {
  feedback.value = null
  applied.value = null
  refusedIssues.value = []
  reason.value = ''
  deployNow.value = false
  showFlow.value = false
  await load()
  await nextTick()
  heading.value?.focus()
})
// The flow may start or stop meanwhile: approving a running flow redeploys it.
watch(() => d.value?.running, running => { deployNow.value = !!running }, { immediate: true })
// A draft can be voided (its token revoked) or decided in another tab: the
// badge's count changes, and the page rereads the draft in the background.
watch(() => agent.pendingDrafts, (count, before) => {
  if (count !== before && isPending.value && !pending.value) void load({ background: true })
})

// A decision reloads the draft: once it is no longer pending, the decision
// section (and the button that had focus) is gone. Focus and show the
// outcome instead of leaving focus on <body> far below it.
async function focusOutcome() {
  await nextTick()
  outcome.value?.focus()
  outcome.value?.scrollIntoView?.({ block: 'nearest' })
}

async function approve() {
  const draft = d.value
  if (!draft || pending.value || block.value) return
  const deploy = willDeploy.value
  const hidden = noteHidden.value ? ` The agent's note contains ${noteHidden.value} hidden character(s), shown highlighted as \\u escapes.` : ''
  const ok = await confirmation.value?.ask(
    'Approve this draft?',
    approveConfirmText(draft, deploy) + hidden,
    deploy ? 'Approve and deploy' : 'Approve and save')
  if (!ok) return
  pending.value = true
  refusedIssues.value = []
  feedback.value = { kind: 'pending', text: deploy ? 'Saving and deploying…' : 'Saving…' }
  try {
    const result = await drafts.approve(draft.id, deploy)
    applied.value = result
    feedback.value = applyOutcome(result, 'Approved')
  } catch (e) {
    if (e instanceof DraftActionError) {
      refusedIssues.value = e.flowIssues
      feedback.value = { kind: 'error', text: `Not approved: ${visibleText(e.message)}` }
    } else {
      feedback.value = { kind: 'error', text: 'Could not approve the draft. Try again.' }
    }
  } finally {
    pending.value = false
  }
  await focusOutcome()
}

async function reject() {
  const draft = d.value
  if (!draft || pending.value || !isPending.value) return
  const why = reason.value.trim()
  const ok = await confirmation.value?.ask(
    'Reject this draft?',
    `Nothing is saved. The agent “${visibleText(draft.tokenName || draft.tokenId)}” is told the draft was rejected${why ? ', with your reason' : ''}.`,
    'Reject draft')
  if (!ok) return
  pending.value = true
  feedback.value = { kind: 'pending', text: 'Rejecting…' }
  try {
    await drafts.reject(draft.id, why)
    feedback.value = { kind: 'success', text: 'Draft rejected. The agent sees it the next time it checks the draft.' }
  } catch (e) {
    feedback.value = { kind: 'error', text: `Not rejected: ${visibleText(e instanceof Error ? e.message : 'try again')}` }
  } finally {
    pending.value = false
  }
  await focusOutcome()
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-4xl space-y-6">
      <AgentNav />

      <template v-if="auth.authEnabled">
        <RouterLink to="/agents/drafts" class="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-4 hover:underline">
          <ArrowLeft class="h-4 w-4" aria-hidden="true" />All drafts
        </RouterLink>

        <div id="draft-outcome" ref="outcome" tabindex="-1" class="space-y-2 focus:outline-none">
          <OperationFeedback :message="feedback" @dismiss="feedback = null" />
          <p v-if="applied" class="text-sm">
            <RouterLink :to="`/editor/${encodeURIComponent(applied.flowId)}`" class="font-medium text-primary underline underline-offset-2">Open the flow in the editor</RouterLink>
          </p>
        </div>

        <p v-if="loading && !d" role="status" class="text-sm text-muted-foreground">Loading the draft…</p>
        <p v-else-if="notFound" role="alert" class="rounded-md bg-muted p-6 text-center text-sm">
          This draft does not exist. Decided drafts are kept for 30 days.
        </p>
        <div v-else-if="loadError && !d" role="alert" class="space-y-2 text-sm text-destructive">
          <p>{{ loadError }}</p>
          <Button variant="outline" size="sm" @click="load()"><RefreshCw class="mr-2 h-4 w-4" aria-hidden="true" />Retry</Button>
        </div>

        <template v-if="d && state">
          <!-- What it changes, and whether it can still be decided. -->
          <section aria-labelledby="draft-heading" class="space-y-3 rounded-lg border bg-card p-4 shadow-sm">
            <div class="flex flex-wrap items-center gap-2">
              <h2 id="draft-heading" ref="heading" tabindex="-1" class="min-w-0 break-words text-lg font-semibold focus:outline-none">
                <template v-if="d.isNew">Draft of a new flow: </template><template v-else>Draft for </template><AgentText :text="d.flowName || d.flowId" />
              </h2>
              <Badge :variant="state.tone === 'pending' ? 'default' : state.tone === 'blocked' ? 'destructive' : 'secondary'">{{ state.label }}</Badge>
            </div>
            <dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
              <div><dt class="text-xs text-muted-foreground">Written by the agent token</dt><dd class="break-words"><AgentText :text="d.tokenName || d.tokenId" /></dd></div>
              <div><dt class="text-xs text-muted-foreground">Written</dt><dd :title="formatDateTime(d.createdAt)">{{ formatRelative(d.createdAt, now) }}</dd></div>
              <div><dt class="text-xs text-muted-foreground">Version</dt><dd>{{ draftVersionText(d) }}</dd></div>
              <div v-if="!d.isNew"><dt class="text-xs text-muted-foreground">Flow</dt><dd>{{ d.running ? 'Running' : 'Not running' }}</dd></div>
              <div v-if="d.decidedAt"><dt class="text-xs text-muted-foreground">Decided</dt><dd :title="formatDateTime(d.decidedAt)">{{ formatRelative(d.decidedAt, now) }}</dd></div>
            </dl>
            <div
              v-if="state.tone !== 'pending'"
              :role="state.tone === 'blocked' ? 'alert' : 'note'"
              class="flex items-start gap-2 rounded-md border p-3 text-sm"
              :class="state.tone === 'blocked' ? 'border-destructive/40 bg-destructive/10' : 'bg-muted'"
            >
              <component :is="state.tone === 'blocked' ? AlertTriangle : d.status === 'approved' ? CheckCircle2 : Info" class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
              <div class="min-w-0 space-y-1">
                <p>{{ state.explanation }}</p>
                <p v-if="d.reason" class="whitespace-pre-wrap break-words">Reason: <AgentText :text="d.reason" /></p>
                <p v-if="d.status === 'approved' && d.flowId">
                  <RouterLink :to="`/editor/${encodeURIComponent(d.flowId)}`" class="font-medium text-primary underline underline-offset-2">Open the flow</RouterLink>
                </p>
              </div>
            </div>
            <a v-if="isPending" href="#draft-decision" class="inline-block text-sm text-primary underline underline-offset-2">Go to the decision</a>
          </section>

          <!-- The agent's note. -->
          <section v-if="d.note" aria-labelledby="note-heading" class="space-y-1">
            <h3 id="note-heading" class="text-sm font-semibold">Note written by the agent</h3>
            <p class="text-xs text-muted-foreground">Shown as text, exactly as the agent sent it{{ noteHidden ? `; ${noteHidden} hidden character(s) are highlighted as \\u escapes` : '' }}.</p>
            <p class="whitespace-pre-wrap break-words rounded-md border p-3 text-sm"><AgentText :text="d.note" /></p>
          </section>

          <!-- A decided draft's risk, alerts and changes are recomputed against the current version. -->
          <p v-if="!isPending && basis" id="draft-basis" role="note" class="flex items-start gap-2 rounded-md border bg-muted p-3 text-sm">
            <Info class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />{{ basis.note }}
          </p>

          <!-- Risk before → after. -->
          <section v-if="risk" aria-labelledby="risk-heading" class="space-y-2">
            <h3 id="risk-heading" class="text-sm font-semibold">Risk</h3>
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <RiskBadge :risk="String(d.diff.riskBefore)" prefix="Before" />
              <ArrowRight class="h-4 w-4 text-muted-foreground" aria-hidden="true" />
              <RiskBadge :risk="String(d.diff.riskAfter)" prefix="After" />
              <span :class="risk.direction === 'raised' && 'font-semibold text-destructive'">{{ risk.text }}.</span>
            </div>
          </section>

          <!-- Alerts, before the changes. -->
          <section v-if="basis" aria-labelledby="alerts-heading" :aria-describedby="isPending ? undefined : 'draft-basis'" class="space-y-2">
            <h3 id="alerts-heading" class="flex items-center gap-2 text-sm font-semibold">
              <component :is="draftIcon('AlertTriangle')" class="h-4 w-4 text-amber-700 dark:text-amber-400" aria-hidden="true" />
              Alerts ({{ d.diff.alerts?.length ?? 0 }})
            </h3>
            <p v-if="isPending" class="text-xs text-muted-foreground">{{ basis.note }}</p>
            <DraftAlerts :alerts="d.diff.alerts" :names="names" :fields="fieldsNow" :proposed="proposed" :empty-text="basis.emptyAlerts" />
          </section>

          <!-- Lint issues now, and those of a refused approval. -->
          <section aria-labelledby="issues-heading" class="space-y-2">
            <h3 id="issues-heading" class="text-sm font-semibold">Issues: {{ issueCountsLabel(counts) }}</h3>
            <ul v-if="extraIssues.length || issues.length" class="space-y-1 text-sm">
              <li v-for="(i, n) in [...extraIssues, ...issues]" :key="`${n}-${i.path}-${i.code}`" class="flex items-start gap-2 rounded-md border p-2"
                :class="i.severity === 'error' ? 'border-destructive/40' : 'border-amber-500/40'">
                <component :is="draftIcon(severityInfo(i.severity).icon)" class="mt-0.5 h-4 w-4 shrink-0" :class="i.severity === 'error' ? 'text-destructive' : 'text-amber-700 dark:text-amber-400'" aria-hidden="true" />
                <div class="min-w-0">
                  <p><span class="font-semibold">{{ severityInfo(i.severity).label }}</span> <span class="break-all font-mono text-xs text-muted-foreground"><AgentText :text="issueWhere(i)" /> · {{ i.code }}</span></p>
                  <p class="whitespace-pre-wrap break-words"><AgentText :text="i.message" /></p>
                </div>
              </li>
            </ul>
            <p v-else class="text-sm text-muted-foreground">The draft passes the same checks deploy runs.</p>
          </section>

          <!-- The changes. -->
          <section v-if="basis" aria-labelledby="changes-heading" :aria-describedby="isPending ? undefined : 'draft-basis'" class="space-y-2">
            <h3 id="changes-heading" class="text-sm font-semibold">{{ basis.heading }}</h3>
            <p class="text-xs text-muted-foreground">Credentials are never shown; settings marked Destination say where the flow sends data or connects.</p>
            <DraftDiff :diff="d.diff" :names="names" label="Changes" />
          </section>

          <section v-if="d.diffApproved" aria-labelledby="approved-heading" class="space-y-3 rounded-lg border border-amber-500/40 p-4">
            <h3 id="approved-heading" class="text-sm font-semibold">Compared with the version you last approved (version {{ d.lastApprovedVersion }})</h3>
            <p class="text-sm text-muted-foreground">
              The current version was saved by an agent, not approved by you. These are the changes since the version you
              approved: what an agent changed before this draft is included.
            </p>
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <RiskBadge :risk="String(d.diffApproved.riskBefore)" prefix="Approved" />
              <ArrowRight class="h-4 w-4 text-muted-foreground" aria-hidden="true" />
              <RiskBadge :risk="String(d.diffApproved.riskAfter)" prefix="With this draft" />
            </div>
            <DraftAlerts
              :alerts="d.diffApproved.alerts" :names="names" :fields="fieldsApproved" :proposed="proposed"
              empty-text="No alerts compared with the version you approved."
            />
            <DraftDiff :diff="d.diffApproved" :names="names" label="Since the version you approved" />
          </section>

          <details class="rounded-md border p-3 text-sm" @toggle="showFlow = ($event.target as HTMLDetailsElement).open">
            <summary class="cursor-pointer font-medium">Proposed flow (JSON as the agent wrote it; credentials hidden)</summary>
            <pre v-if="showFlow" dir="ltr" class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs [unicode-bidi:bidi-override]"><AgentText :text="flowJson" /></pre>
          </details>

          <!-- The decision. -->
          <section v-if="isPending" id="draft-decision" aria-labelledby="decision-heading" class="space-y-4 rounded-lg border bg-card p-4 shadow-sm">
            <h3 id="decision-heading" class="font-semibold">Decision</h3>
            <div class="space-y-3">
              <div class="flex items-start gap-3">
                <input id="draft-deploy" v-model="deployNow" type="checkbox" class="mt-1 h-4 w-4 accent-primary"
                  :disabled="pending || d.running || !!d.stale" aria-describedby="draft-deploy-help" />
                <div>
                  <label for="draft-deploy" class="text-sm font-medium">Deploy now</label>
                  <p id="draft-deploy-help" class="text-xs text-muted-foreground">
                    {{ d.running
                      ? 'The flow is running, so approving always redeploys it with the new version.'
                      : 'Start the flow with the new version right after saving it. Otherwise it is saved and stays stopped.' }}
                  </p>
                </div>
              </div>
              <p v-if="block" id="approve-block" role="note" class="flex items-start gap-2 text-sm text-amber-700 dark:text-amber-400">
                <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />{{ block }}
              </p>
              <Button :disabled="pending || !!block" :aria-describedby="block ? 'approve-block' : undefined" @click="approve">
                {{ willDeploy ? 'Approve and deploy…' : 'Approve and save…' }}
              </Button>
            </div>
            <div class="space-y-2 border-t pt-4">
              <Label for="draft-reason">Reason for rejecting (optional, shown to the agent)</Label>
              <Textarea id="draft-reason" v-model="reason" :rows="2" :disabled="pending" class="font-sans" placeholder="What the agent should change" />
              <p class="text-xs" :class="reason.length > MAX_REASON ? 'text-destructive' : 'text-muted-foreground'">{{ reason.length }} / {{ MAX_REASON }} characters</p>
              <Button variant="outline" :disabled="pending || reason.length > MAX_REASON" @click="reject">Reject…</Button>
            </div>
          </section>
        </template>
      </template>
    </div>
    <ConfirmDialog ref="confirmation" />
  </main>
</template>
