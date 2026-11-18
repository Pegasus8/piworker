// Agent drafts (docs/design/mcp.md §10): the editor lists the drafts agents
// wrote, reviews one (risk, alerts, issues, diff) and approves it, saving a
// new version and deploying it when asked or when the flow runs, or rejects
// it with a reason the agent sees. Approving and rejecting need the session
// only (ADR-0017). Everything a draft holds came from an agent: render it as
// text (AgentText), never as HTML.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiClient } from '@/composables/useApi'
import { msg } from '@/i18n'
import { BACKGROUND_HEADERS, apiErrorMessage, useAgentStore } from '@/stores/agent'
import type { ApplyResult, DraftDetail, DraftFilter, DraftSummary, FlowIssue } from '@/lib/drafts'

/** Drafts per page of the list. */
export const DRAFT_PAGE_SIZE = 50

/**
 * A refused approval or rejection, with what the server said: 409 conflict
 * (no longer pending, or out of date: currentVersion), 400 invalid
 * (flowIssues), 404 unknown. status is undefined when the server was not
 * reached.
 */
export class DraftActionError extends Error {
  constructor(
    message: string,
    readonly status: number | undefined,
    readonly code: string,
    readonly flowIssues: FlowIssue[],
    readonly currentVersion?: number
  ) {
    super(message)
    this.name = 'DraftActionError'
  }
}

function actionError(e: unknown, fallback: string): DraftActionError {
  const response = (e as { response?: { status?: number; data?: { data?: any } } })?.response
  const data = response?.data?.data ?? {}
  const issues = Array.isArray(data.flowIssues) ? data.flowIssues as FlowIssue[] : []
  const current = typeof data.currentVersion === 'number' ? data.currentVersion : undefined
  return new DraftActionError(apiErrorMessage(e, fallback), response?.status, typeof data.code === 'string' ? data.code : '', issues, current)
}

const path = (id: string) => encodeURIComponent(id)
const background = (yes?: boolean) => yes ? { headers: BACKGROUND_HEADERS } : {}

export const useDraftsStore = defineStore('drafts', () => {
  const drafts = ref<DraftSummary[]>([])
  const filter = ref<DraftFilter>('pending')
  const nextBefore = ref('')
  const detail = ref<DraftDetail | null>(null)

  const hasMore = computed(() => nextBefore.value !== '')
  /** A background reload was skipped to keep the pages loaded with Load more: offer a refresh. */
  const outdated = ref(false)
  // Pages shown (the first page and each Load more), and Load more
  // requests in flight.
  let pages = 0
  let loadingMore = 0

  // Lists can overlap (filter changes, refreshes, the badge's background
  // reloads); only the latest one is shown. Returns false, without throwing,
  // when the response was superseded, or when a background reload was
  // skipped: it would replace pages the admin loaded with Load more (or is
  // loading) by the first page, so it only marks the list outdated.
  let listSeq = 0
  async function fetchDrafts(status: DraftFilter = filter.value, opts: { background?: boolean } = {}): Promise<boolean> {
    if (opts.background && (pages > 1 || loadingMore > 0)) {
      outdated.value = true
      return false
    }
    const seq = ++listSeq
    try {
      const { data } = await apiClient.get('/agent/drafts', {
        params: { status, limit: DRAFT_PAGE_SIZE },
        ...background(opts.background)
      })
      if (seq !== listSeq) return false
      drafts.value = data.data?.drafts ?? []
      nextBefore.value = data.data?.nextBefore ?? ''
      filter.value = status
      pages = 1
      outdated.value = false
      return true
    } catch (e) {
      if (seq !== listSeq) return false
      throw e
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextBefore.value) return
    const seq = ++listSeq
    loadingMore++
    try {
      const { data } = await apiClient.get('/agent/drafts', {
        params: { status: filter.value, limit: DRAFT_PAGE_SIZE, beforeId: nextBefore.value }
      })
      if (seq !== listSeq) return
      drafts.value = [...drafts.value, ...(data.data?.drafts ?? [])]
      nextBefore.value = data.data?.nextBefore ?? ''
      pages++
    } finally {
      loadingMore--
    }
  }

  // One draft at a time: a late response for a draft the admin left is
  // ignored (null).
  let detailSeq = 0
  async function fetchDraft(id: string, opts: { background?: boolean } = {}): Promise<DraftDetail | null> {
    const seq = ++detailSeq
    const { data } = await apiClient.get(`/agent/drafts/${path(id)}`, background(opts.background))
    if (seq !== detailSeq) return null
    detail.value = data.data as DraftDetail
    syncSummary(detail.value)
    return detail.value
  }

  function clearDraft() {
    detailSeq++
    detail.value = null
  }

  // Keeps the list's entry of a draft in step with what was just read.
  function syncSummary(d: DraftDetail) {
    const i = drafts.value.findIndex(s => s.id === d.id)
    if (i < 0) return
    const { issues: _i, running: _r, diff: _d, diffApproved: _a, lastApprovedVersion: _l, flow: _f, ...summary } = d
    drafts.value[i] = { ...drafts.value[i], ...summary, note: drafts.value[i].note }
  }

  // After a decision (or a refusal that means the draft changed) the page
  // shows the draft as it is now; a failed reload keeps what is on screen.
  async function reload(id: string) {
    try { await fetchDraft(id) } catch { /* the next refresh shows it */ }
  }

  function refreshBadge() {
    void useAgentStore().refreshPendingCount().catch(() => {})
  }

  async function approve(id: string, deploy: boolean): Promise<ApplyResult> {
    let result: ApplyResult
    try {
      const { data } = await apiClient.post(`/agent/drafts/${path(id)}/approve`, { deploy })
      result = data.data as ApplyResult
    } catch (e) {
      const err = actionError(e, msg().drafts.approveFailed)
      if (err.status === 409 || err.status === 404) await reload(id)
      throw err
    }
    await reload(id)
    refreshBadge()
    return result
  }

  async function reject(id: string, reason: string): Promise<void> {
    const trimmed = reason.trim()
    try {
      await apiClient.post(`/agent/drafts/${path(id)}/reject`, trimmed ? { reason: trimmed } : {})
    } catch (e) {
      const err = actionError(e, msg().drafts.rejectFailed)
      if (err.status === 409 || err.status === 404) await reload(id)
      throw err
    }
    await reload(id)
    refreshBadge()
  }

  return { drafts, filter, nextBefore, hasMore, outdated, detail, fetchDrafts, loadMore, fetchDraft, clearDraft, approve, reject }
})
