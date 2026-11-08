import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

type Call = { method: string; url: string; body?: unknown; params?: unknown }
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
  get: (url: string, config?: { params?: unknown }) => respond({ method: 'GET', url, params: config?.params }),
  post: (url: string, body?: unknown) => respond({ method: 'POST', url, body }),
  put: (url: string, body?: unknown) => respond({ method: 'PUT', url, body }),
  delete: (url: string) => respond({ method: 'DELETE', url })
}
const legacy = {
  getFlows: async () => [{ id: 'f1', name: 'Garage' }, { id: 'f2', name: 'Lights' }],
  getTLSInfo: async () => ({ mode: 'auto', caFingerprint: 'SHA256:AB', sans: [], notAfter: null, caCertUrl: '/piworker-ca.crt' })
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

test('revoke all revokes every active token and reports the ones that failed', async () => {
  let listed = [token('a'), token('b'), token('c', 'expired'), token('d', 'revoked')]
  routes['GET /agent/tokens'] = () => ({ tokens: listed })
  routes['DELETE /agent/tokens/a'] = () => null
  routes['DELETE /agent/tokens/b'] = () => { throw httpError(500) }
  const store = useAgentStore()
  await store.fetchTokens()
  const result = await store.revokeAllTokens()
  expect(result).toEqual({ revoked: 1, failed: ['token b'] })
  const deletes = calls.filter(c => c.method === 'DELETE').map(c => c.url)
  expect(deletes).toEqual(['/agent/tokens/a', '/agent/tokens/b'])
  listed = []
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
})

test('approvals are listed by status and keep the pending count for the badge', async () => {
  routes['GET /agent/approvals'] = (c) => ({ approvals: [approval('x')], pending: 3, echo: c.params })
  const store = useAgentStore()
  await store.fetchApprovals('all')
  expect(calls[0].params).toEqual({ status: 'all' })
  expect(store.approvals.map(a => a.id)).toEqual(['x'])
  expect(store.pendingCount).toBe(3)
  await store.refreshPendingCount()
  expect(calls[1].params).toEqual({ status: 'pending', limit: 1 })
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

test('the audit pages backwards with beforeId and appends', async () => {
  const page = (from: number, n: number) => Array.from({ length: n }, (_, i) => ({ id: from - i, ts: '2026-11-08T12:00:00Z', tokenId: 'a', tokenName: 'laptop', tool: 't', outcome: 'completed', durMs: 5, privileged: false }))
  let next = page(100, 50)
  routes['GET /agent/audit'] = () => ({ entries: next, rejections: [{ tokenId: 'a', at: '2026-11-08T12:00:00Z', reason: 'busy', count: 4 }] })
  const store = useAgentStore()
  await store.fetchAudit({ tool: 'garage_open' })
  expect(calls[0].params).toEqual({ tool: 'garage_open', limit: 50 })
  expect(store.audit).toHaveLength(50)
  expect(store.auditHasMore).toBe(true)
  expect(store.rejections[0].count).toBe(4)
  next = page(50, 3)
  await store.loadMoreAudit()
  expect(calls[1].params).toEqual({ tool: 'garage_open', beforeId: 51, limit: 50 })
  expect(store.audit).toHaveLength(53)
  expect(store.auditHasMore).toBe(false)
  // A new search starts over.
  next = page(10, 1)
  await store.fetchAudit({})
  expect(calls[2].params).toEqual({ limit: 50 })
  expect(store.audit.map(e => e.id)).toEqual([10])
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
  routes['GET /agent/approvals'] = () => ({ approvals: [], pending: 2 })
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
