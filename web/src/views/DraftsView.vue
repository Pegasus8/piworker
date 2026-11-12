<script setup lang="ts">
// Drafts agents wrote (docs/design/mcp.md §10): nothing an agent proposes
// runs or is saved until you approve it here. Names, notes and token names
// come from agents and are rendered as plain text only.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { AlertTriangle, Ban, Bot, CheckCircle2, Clock, FileDiff, Info, RefreshCw, XCircle } from 'lucide-vue-next'
import { Badge, Button, Label, Select } from '@/components/ui'
import AgentNav from '@/components/agent/AgentNav.vue'
import AgentText from '@/components/agent/AgentText.vue'
import RiskBadge from '@/components/agent/RiskBadge.vue'
import { useDraftsStore } from '@/stores/drafts'
import { useAgentStore, apiErrorMessage } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { messages as m } from '@/i18n'
import { formatDateTime, formatRelative, visibleText } from '@/lib/agentAccess'
import { draftState, draftVersionText, issueCountsLabel, type DraftFilter, type DraftSummary } from '@/lib/drafts'

const drafts = useDraftsStore()
const agent = useAgentStore()
const auth = useAuthStore()
const filter = ref<DraftFilter>(drafts.filter)
const loading = ref(false)
const loadingMore = ref(false)
const loadError = ref('')
const now = ref(new Date())
const filters = computed(() => [
  { value: 'pending', label: m.value.drafts.list.filterWaiting },
  { value: 'decided', label: m.value.drafts.list.filterDecided },
  { value: 'all', label: m.value.drafts.list.filterAll }
])

// Loads can overlap (filter, refresh, background reloads); the store shows
// only the latest list, and the page stays loading until none is in flight.
let loads = 0
async function load(opts: { background?: boolean } = {}) {
  if (!auth.authEnabled) return
  loads++
  loading.value = true
  if (!opts.background) loadError.value = ''
  try {
    if (await drafts.fetchDrafts(filter.value, opts)) loadError.value = ''
  } catch (e) {
    loadError.value = apiErrorMessage(e, m.value.drafts.list.loadFailed)
  } finally {
    loading.value = --loads > 0
  }
}

async function more() {
  if (loadingMore.value) return
  loadingMore.value = true
  try {
    await drafts.loadMore()
  } catch (e) {
    loadError.value = apiErrorMessage(e, m.value.drafts.list.loadMoreFailed)
  } finally {
    loadingMore.value = false
  }
}

let clock: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  clock = setInterval(() => { now.value = new Date() }, 30_000)
  void load()
})
onBeforeUnmount(() => clearInterval(clock))
watch(filter, () => load())
// New drafts arrive through the badge's polling. Agents can change the count
// at will, so this reload is a background request: an open tab alone must
// not keep the session alive. With pages loaded by Load more, the store
// skips it (it would replace them by the first page) and the page offers a
// refresh instead.
watch(() => agent.pendingDrafts, (count, before) => {
  if (count !== before && filter.value !== 'decided') void load({ background: true })
})

const stateIcon = (d: DraftSummary) => {
  const s = draftState(d)
  if (s.tone === 'blocked') return AlertTriangle
  if (d.status === 'pending') return Clock
  if (d.status === 'approved') return d.appliedBy === 'agent' ? Bot : CheckCircle2
  if (d.status === 'rejected') return XCircle
  return Ban
}
const stateVariant = (d: DraftSummary) => {
  const tone = draftState(d).tone
  return tone === 'pending' ? 'default' : tone === 'blocked' ? 'destructive' : 'secondary'
}
const reviewLabel = (d: DraftSummary) => visibleText(m.value.drafts.list.reviewLabel(d.isNew, d.flowName || d.flowId))
const alertLabel = (n: number) => m.value.drafts.list.alerts(n)
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-4xl space-y-6">
      <AgentNav />

      <template v-if="auth.authEnabled">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-lg font-semibold">{{ m.drafts.list.title }}</h2>
            <p class="text-sm text-muted-foreground">
              {{ m.drafts.list.intro }}
            </p>
          </div>
          <div class="flex items-end gap-2">
            <div class="space-y-1">
              <Label for="draft-filter" class="text-xs">{{ m.drafts.list.show }}</Label>
              <Select id="draft-filter" v-model="filter" :aria-label="m.drafts.list.statusLabel" :options="filters" class="h-9 w-36" />
            </div>
            <Button variant="outline" size="icon" :aria-label="m.drafts.list.refreshLabel" :disabled="loading" @click="load()"><RefreshCw class="h-4 w-4" /></Button>
          </div>
        </div>

        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
        <div v-if="drafts.outdated" role="status" class="flex flex-wrap items-center gap-2 rounded-md border bg-muted p-3 text-sm">
          <Info class="h-4 w-4 shrink-0" aria-hidden="true" />
          <span class="min-w-0 flex-1">{{ m.drafts.list.outdated }}</span>
          <Button variant="outline" size="sm" :disabled="loading" @click="load()"><RefreshCw class="mr-2 h-4 w-4" aria-hidden="true" />{{ m.drafts.list.refresh }}</Button>
        </div>
        <p v-if="loading && !drafts.drafts.length" role="status" class="text-sm text-muted-foreground">{{ m.drafts.list.loading }}</p>
        <p v-else-if="!loadError && !drafts.drafts.length" class="rounded-md bg-muted p-6 text-center text-sm text-muted-foreground">
          {{ filter !== 'pending' ? m.drafts.list.empty
            : agent.settings?.authorEnabled === false
              ? m.drafts.list.emptyAuthorOff
              : m.drafts.list.emptyWaiting }}
        </p>

        <article
          v-for="d in drafts.drafts"
          :key="d.id"
          :aria-label="visibleText(m.drafts.list.articleLabel(d.flowName || d.flowId))"
          class="space-y-3 rounded-lg border bg-card p-4 shadow-sm"
        >
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="min-w-0 break-words font-semibold"><AgentText :text="d.flowName || d.flowId" /></h3>
            <Badge v-if="d.isNew" variant="outline">{{ m.drafts.list.newFlow }}</Badge>
            <Badge :variant="stateVariant(d)" class="gap-1"><component :is="stateIcon(d)" class="h-3 w-3" aria-hidden="true" />{{ draftState(d).label }}</Badge>
            <RiskBadge :risk="String(d.risk)" :prefix="m.drafts.list.riskPrefix" />
            <Badge v-if="d.alertCount" variant="outline" class="gap-1 border-amber-500/60 text-amber-800 dark:text-amber-300">
              <AlertTriangle class="h-3 w-3" aria-hidden="true" />{{ alertLabel(d.alertCount) }}
            </Badge>
            <Badge v-if="d.issueCounts.error || d.issueCounts.warning" :variant="d.issueCounts.error ? 'destructive' : 'outline'">{{ issueCountsLabel(d.issueCounts) }}</Badge>
          </div>
          <dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
            <div><dt class="text-xs text-muted-foreground">{{ m.drafts.list.token }}</dt><dd class="break-words"><AgentText :text="d.tokenName || d.tokenId" /></dd></div>
            <div><dt class="text-xs text-muted-foreground">{{ m.drafts.list.written }}</dt><dd :title="formatDateTime(d.createdAt)">{{ formatRelative(d.createdAt, now) }}</dd></div>
            <div><dt class="text-xs text-muted-foreground">{{ m.drafts.list.version }}</dt><dd>{{ draftVersionText(d) }}</dd></div>
            <div v-if="d.decidedAt"><dt class="text-xs text-muted-foreground">{{ m.drafts.list.decided }}</dt><dd :title="formatDateTime(d.decidedAt)">{{ formatRelative(d.decidedAt, now) }}</dd></div>
          </dl>
          <p v-if="draftState(d).tone === 'blocked'" role="note" class="flex items-start gap-2 text-sm text-amber-700 dark:text-amber-400">
            <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />{{ draftState(d).explanation }}
          </p>
          <div v-if="d.note" class="space-y-1">
            <p class="text-xs text-muted-foreground">{{ m.drafts.list.note }}</p>
            <p class="whitespace-pre-wrap break-words rounded-md border p-2 text-sm"><AgentText :text="d.note" /></p>
          </div>
          <RouterLink
            :to="`/agents/drafts/${encodeURIComponent(d.id)}`"
            :aria-label="reviewLabel(d)"
            class="lab-button inline-flex h-9 items-center gap-2 rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <FileDiff class="h-4 w-4" aria-hidden="true" />{{ d.status === 'pending' && !d.stale ? m.drafts.list.review : m.drafts.list.view }}
          </RouterLink>
        </article>

        <div v-if="drafts.hasMore" class="flex justify-center">
          <Button variant="outline" :disabled="loadingMore" @click="more">{{ loadingMore ? m.common.loading : m.drafts.list.loadMore }}</Button>
        </div>
      </template>
    </div>
  </main>
</template>
