// Agent access (docs/design/mcp.md §7, §9, §10): tokens and their grants, the
// published tools, the agent deploy level, the approval queue and the audit
// log. Every call uses the editor's cookie session; agent tokens never work
// here. The plaintext of a new token is returned to the caller once and never
// stored.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiClient, useApi } from '@/composables/useApi'
import { createVisibilityPoller, type Poller, type PollerOptions } from '@/lib/poller'
import {
  auditParams,
  buildCreateTokenRequest,
  toolKey,
  type AgentApproval,
  type AgentDeployLevel,
  type AgentSettings,
  type AgentToken,
  type AgentTool,
  type ApprovalDecision,
  type AuditEntry,
  type AuditFilters,
  type AuditRejection,
  type CreateTokenForm,
  type CreatedAgentToken,
  type SnippetTLS
} from '@/lib/agentAccess'

/** How often the navigation badge refreshes the pending approvals count. */
export const APPROVAL_POLL_MS = 10_000
/** Audit rows per page. */
export const AUDIT_PAGE_SIZE = 50
const MAX_RECENT_DECISIONS = 10

export type ApprovalFilter = 'pending' | 'approved' | 'rejected' | 'expired' | 'all'

export interface GrantChange {
  action: 'confirm' | 'suspend' | 'remove' | 'add'
  flowId: string
  triggerNodeId: string
  fingerprint?: string
  password?: string
}

/** The server's error message, or fallback. */
export function apiErrorMessage(e: unknown, fallback: string): string {
  const message = (e as { response?: { data?: { error?: unknown } } })?.response?.data?.error
  return typeof message === 'string' && message ? message : fallback
}

function status(e: unknown): number | undefined {
  return (e as { response?: { status?: number } })?.response?.status
}

const path = (id: string) => encodeURIComponent(id)

export const useAgentStore = defineStore('agent', () => {
  const tokens = ref<AgentToken[]>([])
  const tools = ref<AgentTool[]>([])
  const toolsError = ref('')
  const settings = ref<AgentSettings | null>(null)
  const approvals = ref<AgentApproval[]>([])
  const pendingCount = ref(0)
  const recentDecisions = ref<ApprovalDecision[]>([])
  const agentApiAvailable = ref(true)
  const audit = ref<AuditEntry[]>([])
  const rejections = ref<AuditRejection[]>([])
  const auditHasMore = ref(false)
  const auditFilters = ref<AuditFilters>({})
  const flowNames = ref<Record<string, string>>({})
  const tls = ref<SnippetTLS | null>(null)

  const activeTokens = computed(() => tokens.value.filter(t => t.status === 'active'))
  const toolIndex = computed(() => new Map(tools.value.map(t => [toolKey(t.flowId, t.triggerNodeId), t])))
  function toolFor(flowId: string | undefined, triggerNodeId: string): AgentTool | undefined {
    return flowId ? toolIndex.value.get(toolKey(flowId, triggerNodeId)) : undefined
  }

  // Tokens and grants.
  async function fetchTokens() {
    const { data } = await apiClient.get('/agent/tokens')
    tokens.value = data.data?.tokens ?? []
  }

  async function createToken(form: CreateTokenForm): Promise<CreatedAgentToken> {
    const { data } = await apiClient.post('/agent/tokens', buildCreateTokenRequest(form))
    const created = data.data as CreatedAgentToken
    try { await fetchTokens() } catch { /* the token exists; the list refreshes later */ }
    return created
  }

  async function revokeToken(id: string) {
    await apiClient.delete(`/agent/tokens/${path(id)}`)
    await fetchTokens()
  }

  /** Revokes every active token; tokens that could not be revoked are named. */
  async function revokeAllTokens(): Promise<{ revoked: number; failed: string[] }> {
    let revoked = 0
    const failed: string[] = []
    for (const t of activeTokens.value) {
      try {
        await apiClient.delete(`/agent/tokens/${path(t.id)}`)
        revoked++
      } catch {
        failed.push(t.name)
      }
    }
    try { await fetchTokens() } catch { /* shown on the next refresh */ }
    return { revoked, failed }
  }

  async function updateGrant(tokenId: string, change: GrantChange) {
    const body: Record<string, string> = { action: change.action, flowId: change.flowId, triggerNodeId: change.triggerNodeId }
    if (change.fingerprint) body.fingerprint = change.fingerprint
    if (change.password) body.password = change.password
    const { data } = await apiClient.post(`/agent/tokens/${path(tokenId)}/grants`, body)
    const updated = data.data as AgentToken
    const i = tokens.value.findIndex(t => t.id === updated.id)
    if (i >= 0) tokens.value[i] = updated
    else tokens.value.unshift(updated)
  }

  // Published tools, for grant pickers. The endpoint may be missing on older
  // servers: that reads as "no tools published yet".
  async function fetchTools() {
    toolsError.value = ''
    try {
      const { data } = await apiClient.get('/agent/tools')
      tools.value = data.data?.tools ?? []
    } catch (e) {
      tools.value = []
      if (status(e) !== 404) toolsError.value = apiErrorMessage(e, 'Could not load the published tools.')
    }
  }

  // Agent deploy level.
  async function fetchSettings() {
    const { data } = await apiClient.get('/agent/settings')
    settings.value = data.data
  }

  async function saveSettings(agentDeploy: AgentDeployLevel, password: string) {
    const { data } = await apiClient.put('/agent/settings', { agentDeploy, password })
    settings.value = data.data
  }

  // Approvals.
  async function fetchApprovals(filter: ApprovalFilter = 'pending') {
    const { data } = await apiClient.get('/agent/approvals', { params: { status: filter } })
    approvals.value = data.data?.approvals ?? []
    pendingCount.value = data.data?.pending ?? 0
  }

  async function refreshPendingCount() {
    try {
      const { data } = await apiClient.get('/agent/approvals', { params: { status: 'pending', limit: 1 } })
      pendingCount.value = data.data?.pending ?? 0
      agentApiAvailable.value = true
    } catch (e) {
      if (status(e) === 404) {
        // No agent API on this server (authentication is off): stop asking.
        agentApiAvailable.value = false
        pendingCount.value = 0
        poller?.stop()
        return
      }
      throw e
    }
  }

  async function decide(id: string, approve: boolean): Promise<ApprovalDecision> {
    const { data } = await apiClient.post(`/agent/approvals/${path(id)}/${approve ? 'approve' : 'reject'}`)
    const decision = data.data as ApprovalDecision
    const i = approvals.value.findIndex(a => a.id === id)
    if (i >= 0) approvals.value[i] = decision.approval
    recentDecisions.value = [decision, ...recentDecisions.value.filter(d => d.approval.id !== id)].slice(0, MAX_RECENT_DECISIONS)
    void refreshPendingCount().catch(() => {})
    return decision
  }

  // The pending badge polls while at least one view watches it.
  let poller: Poller | null = null
  let watchers = 0
  function watchApprovals(opts: Partial<PollerOptions> = {}): () => void {
    watchers++
    if (watchers === 1) {
      poller = createVisibilityPoller(refreshPendingCount, { intervalMs: APPROVAL_POLL_MS, ...opts })
      poller.start()
    }
    let released = false
    return () => {
      if (released) return
      released = true
      watchers--
      if (watchers === 0) {
        poller?.stop()
        poller = null
      }
    }
  }

  // Audit.
  async function loadAudit(beforeId?: number) {
    const { data } = await apiClient.get('/agent/audit', { params: auditParams(auditFilters.value, AUDIT_PAGE_SIZE, beforeId) })
    const entries: AuditEntry[] = data.data?.entries ?? []
    audit.value = beforeId ? [...audit.value, ...entries] : entries
    if (!beforeId) rejections.value = data.data?.rejections ?? []
    auditHasMore.value = entries.length === AUDIT_PAGE_SIZE
  }

  async function fetchAudit(filters: AuditFilters) {
    auditFilters.value = { ...filters }
    await loadAudit()
  }

  async function loadMoreAudit() {
    const last = audit.value.at(-1)
    if (last) await loadAudit(last.id)
  }

  // Context from the rest of the API.
  async function fetchFlowNames() {
    const flows = await useApi().getFlows()
    flowNames.value = Object.fromEntries(flows.map(f => [f.id, f.name]))
  }

  async function fetchTLS() {
    const info = await useApi().getTLSInfo()
    tls.value = { mode: info.mode, caFingerprint: info.caFingerprint }
  }

  return {
    tokens, tools, toolsError, settings, approvals, pendingCount, recentDecisions, agentApiAvailable,
    audit, rejections, auditHasMore, auditFilters, flowNames, tls,
    activeTokens, toolFor,
    fetchTokens, createToken, revokeToken, revokeAllTokens, updateGrant,
    fetchTools, fetchSettings, saveSettings,
    fetchApprovals, refreshPendingCount, decide, watchApprovals,
    fetchAudit, loadMoreAudit,
    fetchFlowNames, fetchTLS
  }
})
