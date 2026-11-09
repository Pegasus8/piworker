import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

type Call = { method: string; url: string; body?: unknown; params?: unknown; headers?: Record<string, string> }
type Responder = (call: Call) => unknown
let calls: Call[] = []
let routes: Record<string, Responder> = {}

function httpError(status: number, error = 'failed') {
  return Object.assign(new Error(error), { response: { status, data: { success: false, error } } })
}

async function respond(call: Call) {
  calls.push(call)
  const handler = routes[`${call.method} ${call.url}`]
  if (!handler) throw httpError(404, 'not found')
  const data = handler(call)
  return { data: { success: true, data } }
}

const apiClient = {
  get: (url: string, config?: { params?: unknown; headers?: Record<string, string> }) =>
    respond({ method: 'GET', url, params: config?.params, ...(config?.headers ? { headers: config.headers } : {}) }),
  post: (url: string, body?: unknown) => respond({ method: 'POST', url, body }),
  put: (url: string, body?: unknown) => respond({ method: 'PUT', url, body }),
  delete: (url: string) => respond({ method: 'DELETE', url })
}
const legacy = {
  getFlows: async () => [{ id: 'f1', name: 'Garage' }, { id: 'f2', name: 'Lights' }],
  getTLSInfo: async () => ({ mode: 'auto', caFingerprint: 'SHA256:AB', sans: [], notAfter: null, caCertUrl: '/piworker-ca.crt' }),
  getSecrets: async (): Promise<unknown> => ({ secrets: [], shortBelow: 16 })
}
mock.module('../src/composables/useApi', () => ({ apiClient, useApi: () => legacy }))
const { useAgentStore } = await import('../src/stores/agent')

const token = (id: string, status = 'active', grants: unknown[] = []) => ({
  id, name: `token ${id}`, prefix: `pwk_${id}`, scopes: ['call'], privileged: false, grants, allTools: false,
  expiresAt: '2027-01-01T00:00:00Z', createdAt: '2026-11-01T00:00:00Z', status
})
const tool = (over: Record<string, unknown> = {}) => ({
  flowId: 'f1', flowName: 'Garage', triggerNodeId: 'n1', toolName: 'garage_open', title: 'Open garage',
  description: 'Opens the garage door', risk: 'external',
  annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: true },
  fingerprint: 'sha256:aaa', confirm: 'admin', inputSchema: { type: 'object' }, ...over
})
const approval = (id: string, over: Record<string, unknown> = {}) => ({
  id, kind: 'tool_call', tokenId: 'a', tokenName: 'laptop', flowId: 'f1', ref: 'n1', args: { door: 'main' },
  summary: '', status: 'pending', createdAt: '2026-11-08T12:00:00Z', expiresAt: '2026-11-08T12:15:00Z', tokenStatus: 'active', ...over
})

beforeEach(() => {
  setActivePinia(createPinia())
  calls = []
  routes = {}
})

test('lists tokens from the response envelope', async () => {
  routes['GET /agent/tokens'] = () => ({ tokens: [token('a'), token('b', 'revoked')] })
  const store = useAgentStore()
  await store.fetchTokens()
  expect(store.tokens.map(t => t.id)).toEqual(['a', 'b'])
  expect(store.activeTokens.map(t => t.id)).toEqual(['a'])
})

test('creating a token sends the strict body and returns the plaintext once', async () => {
  routes['POST /agent/tokens'] = () => ({ token: 'pwk_new_secret', info: token('new') })
  routes['GET /agent/tokens'] = () => ({ tokens: [token('new')] })
  const store = useAgentStore()
  const created = await store.createToken({
    name: ' laptop ', scopes: ['call'], privileged: false, allTools: false,
    tools: [tool() as any], expiresInDays: 90, password: 'pw'
  })
  expect(created.token).toBe('pwk_new_secret')
  const post = calls.find(c => c.method === 'POST')!
  expect(post.body).toEqual({
    name: 'laptop', scopes: ['call'], privileged: false, allTools: false,
    grants: [{ flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:aaa' }], expiresInDays: 90, password: 'pw'
  })
  // The list is refreshed, and the plaintext is never kept in the store.
  expect(store.tokens.map(t => t.id)).toEqual(['new'])
  expect(JSON.stringify(store.$state)).not.toContain('pwk_new_secret')
})

test('a missing or empty tools endpoint means no tools are published yet', async () => {
  const store = useAgentStore()
  await store.fetchTools()
  expect(store.tools).toEqual([])
  expect(store.toolsError).toBe('')
  routes['GET /agent/tools'] = () => ({ version: 3, tools: null })
  await store.fetchTools()
  expect(store.tools).toEqual([])
  routes['GET /agent/tools'] = () => ({ version: 4, tools: [tool(), tool({ triggerNodeId: 'n2', toolName: 'other' })] })
  await store.fetchTools()
  expect(store.tools).toHaveLength(2)
  expect(store.toolFor('f1', 'n2')?.toolName).toBe('other')
  expect(store.toolFor('f1', 'missing')).toBeUndefined()
  routes['GET /agent/tools'] = () => { throw httpError(500, 'Cannot list tools') }
  await store.fetchTools()
  expect(store.toolsError).toBe('Cannot list tools')
})

test('revoking one token encodes its id and refreshes the list', async () => {
  routes['DELETE /agent/tokens/a%2Fb'] = () => null
  routes['GET /agent/tokens'] = () => ({ tokens: [token('a/b', 'revoked')] })
  const store = useAgentStore()
  await store.revokeToken('a/b')
  expect(calls.map(c => `${c.method} ${c.url}`)).toEqual(['DELETE /agent/tokens/a%2Fb', 'GET /agent/tokens'])
  expect(store.tokens[0].status).toBe('revoked')
})

test('revoke all is one request that revokes every token at once, then refreshes the list', async () => {
  let listed = [token('a'), token('b'), token('c', 'expired'), token('d', 'revoked')]
  routes['GET /agent/tokens'] = () => ({ tokens: listed })
  routes['POST /agent/tokens/revoke-all'] = () => {
    listed = listed.map(t => ({ ...t, status: 'revoked' }))
    return { revoked: 3 }
  }
  const store = useAgentStore()
  await store.fetchTokens()
  calls = []
  expect(await store.revokeAllTokens()).toBe(3)
  expect(calls.map(c => `${c.method} ${c.url}`)).toEqual(['POST /agent/tokens/revoke-all', 'GET /agent/tokens'])
  expect(calls[0].body).toBeUndefined()
  expect(store.activeTokens).toEqual([])

  // A failure is reported to the caller; nothing is half done on the server.
  routes['POST /agent/tokens/revoke-all'] = () => { throw httpError(500, 'Cannot revoke the agent tokens') }
  await expect(store.revokeAllTokens()).rejects.toThrow('Cannot revoke the agent tokens')
})

test('grant changes send only the fields the action needs and update the token', async () => {
  const grant = { flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:aaa', suspended: true }
  routes['GET /agent/tokens'] = () => ({ tokens: [token('a', 'active', [grant])] })
  routes['POST /agent/tokens/a/grants'] = (c) => {
    const body = c.body as { action: string }
    return token('a', 'active', body.action === 'remove' ? [] : [{ ...grant, suspended: body.action === 'suspend' }])
  }
  const store = useAgentStore()
  await store.fetchTokens()
  await store.updateGrant('a', { action: 'confirm', flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:new', password: 'pw' })
  expect(calls.at(-1)!.body).toEqual({ action: 'confirm', flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:new', password: 'pw' })
  expect(store.tokens[0].grants[0].suspended).toBe(false)
  await store.updateGrant('a', { action: 'suspend', flowId: 'f1', triggerNodeId: 'n1' })
  expect(calls.at(-1)!.body).toEqual({ action: 'suspend', flowId: 'f1', triggerNodeId: 'n1' })
  expect(store.tokens[0].grants[0].suspended).toBe(true)
  await store.updateGrant('a', { action: 'remove', flowId: 'f1', triggerNodeId: 'n1' })
  expect(store.tokens[0].grants).toEqual([])
})

test('agent deploy settings are read and changed with the password', async () => {
  routes['GET /agent/settings'] = () => ({ agentDeploy: 'off', authorEnabled: false })
  routes['PUT /agent/settings'] = (c) => ({ agentDeploy: (c.body as { agentDeploy: string }).agentDeploy, authorEnabled: false })
  const store = useAgentStore()
  await store.fetchSettings()
  expect(store.settings?.agentDeploy).toBe('off')
  await store.saveSettings('low', 'pw')
  expect(calls.at(-1)!.body).toEqual({ agentDeploy: 'low', password: 'pw' })
  expect(store.settings?.agentDeploy).toBe('low')
  // Settings that cannot be read again are unknown, not the last ones seen.
  routes['GET /agent/settings'] = () => { throw httpError(503, 'Cannot read agent settings') }
  await expect(store.fetchSettings()).rejects.toThrow('Cannot read agent settings')
  expect(store.settings).toBeNull()
})

test('reads of the settings and TLS details in flight are shared by the views asking at once', async () => {
  routes['GET /agent/settings'] = () => ({ agentDeploy: 'off', authorEnabled: false, mcpEnabled: true, lanAccess: false })
  let tlsReads = 0
  const realTLS = legacy.getTLSInfo
  legacy.getTLSInfo = async () => { tlsReads++; return realTLS() }
  try {
    const store = useAgentStore()
    await Promise.all([store.fetchSettings(), store.fetchSettings(), store.fetchTLS(), store.fetchTLS()])
    expect(calls.filter(c => c.url === '/agent/settings')).toHaveLength(1)
    expect(tlsReads).toBe(1)
    expect(store.settings?.mcpEnabled).toBe(true)
    expect(store.tls?.mode).toBe('auto')
    // A later read asks again.
    await Promise.all([store.fetchSettings(), store.fetchTLS()])
    expect(calls.filter(c => c.url === '/agent/settings')).toHaveLength(2)
    expect(tlsReads).toBe(2)
    // A failed read fails every caller that shared it.
    routes['GET /agent/settings'] = () => { throw httpError(503, 'Cannot read agent settings') }
    const results = await Promise.allSettled([store.fetchSettings(), store.fetchSettings()])
    expect(results.map(r => r.status)).toEqual(['rejected', 'rejected'])
    expect(store.settings).toBeNull()
  } finally {
    legacy.getTLSInfo = realTLS
  }
})

test('the secrets are checked for short values once per page load, unknown when unreadable', async () => {
  const realSecrets = legacy.getSecrets
  let reads = 0
  legacy.getSecrets = async () => {
    reads++
    return { secrets: [{ name: 'pin', short: true }, { name: 'door', short: true }, { name: 'api_key', short: false }], shortBelow: 16 }
  }
  try {
    const store = useAgentStore()
    expect(store.secretList).toBeNull()
    expect(store.shortSecrets).toEqual({ short: null, text: 'Could not check the secrets for short values.' })
    await store.fetchSecretList()
    expect(reads).toBe(1)
    expect(store.shortSecrets).toEqual({ short: 2, text: '2 of 3 secrets are short (fewer than 16 characters): an agent could confirm a guess of them.' })
    // Nothing polls it: the count changes only when the page asks again.
    await new Promise(r => setTimeout(r, 20))
    expect(reads).toBe(1)
    // A list that cannot be read again is unknown, not the last one seen.
    legacy.getSecrets = async () => { throw httpError(500, 'Failed to read secrets') }
    await expect(store.fetchSecretList()).rejects.toThrow('Failed to read secrets')
    expect(store.secretList).toBeNull()
    expect(store.shortSecrets.short).toBeNull()
  } finally {
    legacy.getSecrets = realSecrets
  }
})

test('approvals are listed by status and keep the pending count for the badge', async () => {
  routes['GET /agent/approvals'] = (c) => ({ approvals: [approval('x')], pending: 3, echo: c.params })
  const store = useAgentStore()
  await store.fetchApprovals('all')
  expect(calls[0].params).toEqual({ status: 'all' })
  expect(store.approvals.map(a => a.id)).toEqual(['x'])
  expect(store.pendingCount).toBe(3)
  // The badge polls the lightweight count, which does not keep the session alive.
  routes['GET /agent/approvals/pending-count'] = () => ({ pending: 1 })
  await store.refreshPendingCount()
  expect(`${calls[1].method} ${calls[1].url}`).toBe('GET /agent/approvals/pending-count')
  expect(calls[1].params).toBeUndefined()
  expect(store.pendingCount).toBe(1)
})

test('a slower, older approvals response never replaces a newer one', async () => {
  // The pending list arrives after the "all" list asked for later.
  const gates: Record<string, () => void> = {}
  routes['GET /agent/approvals'] = (c) => ({ status: (c.params as { status: string }).status })
  const realGet = apiClient.get
  apiClient.get = async (url: string, config?: { params?: unknown }) => {
    const status = (config?.params as { status: string }).status
    await new Promise<void>(resolve => { gates[status] = resolve })
    const res = await realGet(url, config)
    const data = res.data.data as { status: string }
    return { data: { success: true, data: { approvals: [approval(data.status, { status: data.status === 'all' ? 'approved' : 'pending' })], pending: data.status === 'all' ? 1 : 5 } } }
  }
  try {
    const store = useAgentStore()
    const older = store.fetchApprovals('pending')
    const newer = store.fetchApprovals('all')
    await new Promise(r => setTimeout(r, 0))
    gates.all()
    await newer
    gates.pending()
    expect(await older).toBe(false)
    expect(store.approvals.map(a => a.id)).toEqual(['all'])
    expect(store.pendingCount).toBe(1)

    // A superseded request that fails is not reported either.
    routes['GET /agent/approvals'] = (c) => {
      if ((c.params as { status: string }).status === 'expired') throw httpError(500, 'Cannot read approvals')
      return { status: (c.params as { status: string }).status }
    }
    const failing = store.fetchApprovals('expired')
    const current = store.fetchApprovals('all')
    await new Promise(r => setTimeout(r, 0))
    gates.all()
    expect(await current).toBe(true)
    gates.expired()
    expect(await failing).toBe(false)
  } finally {
    apiClient.get = realGet
  }
})

// The Approvals page reloads its list when the badge count changes, which an
// agent can cause at will; those reloads are background requests, which do
// not extend the session (api.BackgroundRequestHeader).
test('a background reload of the approvals marks itself so the session is not extended', async () => {
  routes['GET /agent/approvals'] = () => ({ approvals: [approval('x')], pending: 1 })
  const store = useAgentStore()
  await store.fetchApprovals('pending', { background: true })
  expect(calls[0].headers).toEqual({ 'X-PiWorker-Background': '1' })
  expect(calls[0].params).toEqual({ status: 'pending' })
  await store.fetchApprovals('pending')
  expect(calls[1].headers).toBeUndefined()
})

test('a decision is not undone by a list requested before it', async () => {
  let release = () => {}
  routes['GET /agent/approvals'] = () => ({ approvals: [approval('x')], pending: 1 })
  routes['POST /agent/approvals/x/approve'] = () => ({ approval: approval('x', { status: 'approved' }), executed: true })
  const store = useAgentStore()
  await store.fetchApprovals('pending')
  const realGet = apiClient.get
  apiClient.get = async (url: string, config?: { params?: unknown }) => {
    await new Promise<void>(resolve => { release = resolve })
    return realGet(url, config)
  }
  try {
    const inFlight = store.fetchApprovals('pending')
    await new Promise(r => setTimeout(r, 0))
    const releaseList = release
    await store.decide('x', true)
    releaseList()
    expect(await inFlight).toBe(false)
    expect(store.approvals.find(a => a.id === 'x')?.status).toBe('approved')
  } finally {
    release()
    apiClient.get = realGet
  }
})

test('a server without the agent API stops the badge from polling', async () => {
  const store = useAgentStore()
  await store.refreshPendingCount()
  expect(store.agentApiAvailable).toBe(false)
  expect(store.pendingCount).toBe(0)
})

test('approving keeps the decision and its execution outcome', async () => {
  routes['GET /agent/approvals'] = () => ({ approvals: [approval('x'), approval('y')], pending: 2 })
  routes['POST /agent/approvals/x/approve'] = () => ({ approval: approval('x', { status: 'approved' }), executed: false, executionError: 'not available' })
  routes['POST /agent/approvals/y/reject'] = () => ({ approval: approval('y', { status: 'rejected' }), executed: false })
  const store = useAgentStore()
  await store.fetchApprovals('pending')
  const decision = await store.decide('x', true)
  expect(decision.executionError).toBe('not available')
  expect(store.approvals.find(a => a.id === 'x')?.status).toBe('approved')
  await store.decide('y', false)
  expect(store.recentDecisions.map(d => d.approval.id)).toEqual(['y', 'x'])
  expect(calls.filter(c => c.method === 'POST').map(c => c.url)).toEqual(['/agent/approvals/x/approve', '/agent/approvals/y/reject'])
})

const auditPage = (from: number, n: number) => Array.from({ length: n }, (_, i) => ({ id: from - i, ts: '2026-11-08T12:00:00Z', tokenId: 'a', tokenName: 'laptop', tool: 't', outcome: 'completed', durMs: 5, privileged: false }))
const total = (reason: string, count: number) => ({ tokenId: 'a', tokenName: 'laptop', reason, count, last: '2026-11-08T12:05:00Z' })
const isTotals = (c: Call) => c.url === '/agent/audit/rejections'

test('the audit pages backwards with beforeId and appends', async () => {
  let next = auditPage(100, 50)
  routes['GET /agent/audit'] = () => ({ entries: next, rejections: [] })
  routes['GET /agent/audit/rejections'] = () => ({ totals: [total('busy', 4)], truncated: false })
  const store = useAgentStore()
  await store.fetchAudit({ tool: 'garage_open' })
  const entryCalls = () => calls.filter(c => c.url === '/agent/audit')
  expect(entryCalls()[0].params).toEqual({ tool: 'garage_open', limit: 50 })
  expect(store.audit).toHaveLength(50)
  expect(store.auditHasMore).toBe(true)
  expect(store.rejections[0].count).toBe(4)
  next = auditPage(50, 3)
  await store.loadMoreAudit()
  expect(entryCalls()[1].params).toEqual({ tool: 'garage_open', beforeId: 51, limit: 50 })
  expect(store.audit).toHaveLength(53)
  expect(store.auditHasMore).toBe(false)
  // Paging does not reload the totals.
  expect(calls.filter(isTotals)).toHaveLength(1)
  // A new search starts over.
  next = auditPage(10, 1)
  await store.fetchAudit({})
  expect(entryCalls()[2].params).toEqual({ limit: 50 })
  expect(store.audit.map(e => e.id)).toEqual([10])
})

test('rejection totals are summed by the server, with only the filters they support', async () => {
  routes['GET /agent/audit'] = () => ({ entries: auditPage(5, 1), rejections: [] })
  routes['GET /agent/audit/rejections'] = () => ({ totals: [total('busy', 7), total('cooldown', 2)], truncated: false })
  const store = useAgentStore()
  const local = '2026-11-08T12:30'
  await store.fetchAudit({ tokenId: 'a', tool: 'garage_open', flowId: 'f1', outcome: 'busy', since: local, until: local })
  // The server applies only the token and the time window to rejections.
  const at = new Date(local).toISOString().replace(/\.\d{3}Z$/, 'Z')
  expect(calls.find(isTotals)!.params).toEqual({ tokenId: 'a', since: at, until: at })
  expect(store.rejections.map(r => [r.reason, r.count])).toEqual([['busy', 7], ['cooldown', 2]])
  expect(store.rejectionsTruncated).toBe(false)
  routes['GET /agent/audit/rejections'] = () => ({ totals: [total('busy', 1)], truncated: true })
  await store.fetchAudit({})
  expect(calls.filter(isTotals).at(-1)!.params).toEqual({})
  expect(store.rejectionsTruncated).toBe(true)
  // A server without the totals endpoint answers 404: nothing to show.
  delete routes['GET /agent/audit/rejections']
  await store.fetchAudit({})
  expect(store.rejections).toEqual([])
})

test('flow names and TLS details come from the existing API', async () => {
  const store = useAgentStore()
  await store.fetchFlowNames()
  expect(store.flowNames).toEqual({ f1: 'Garage', f2: 'Lights' })
  await store.fetchTLS()
  expect(store.tls?.mode).toBe('auto')
})

test('server error messages are preferred over the fallback', async () => {
  const { apiErrorMessage } = await import('../src/stores/agent')
  expect(apiErrorMessage(httpError(403, 'The password is incorrect'), 'fallback')).toBe('The password is incorrect')
  expect(apiErrorMessage(new Error('offline'), 'fallback')).toBe('fallback')
  expect(apiErrorMessage({ response: { data: { error: 42 } } }, 'fallback')).toBe('fallback')
})

test('the approval badge polls once for all watchers and stops with the last one', async () => {
  routes['GET /agent/approvals/pending-count'] = () => ({ pending: 2 })
  const doc = Object.assign(new EventTarget(), { visibilityState: 'visible' })
  let timers = 0
  const opts = { document: doc as any, setInterval: () => ++timers, clearInterval: () => { timers-- } }
  const store = useAgentStore()
  const stopA = store.watchApprovals(opts)
  const stopB = store.watchApprovals(opts)
  await new Promise(r => setTimeout(r, 0))
  expect(timers).toBe(1)
  expect(calls).toHaveLength(1)
  expect(store.pendingCount).toBe(2)
  stopA()
  expect(timers).toBe(1)
  stopB()
  expect(timers).toBe(0)
  stopB()
  expect(timers).toBe(0)
})
