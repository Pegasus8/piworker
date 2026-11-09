import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

type Call = { method: string; url: string; body?: unknown; params?: unknown; headers?: Record<string, string> }
type Responder = (call: Call) => unknown
let calls: Call[] = []
let routes: Record<string, Responder> = {}

function httpError(status: number, error = 'failed', data?: unknown) {
  return Object.assign(new Error(error), { response: { status, data: { success: false, error, ...(data ? { data } : {}) } } })
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
mock.module('../src/composables/useApi', () => ({ apiClient, useApi: () => ({}) }))
const { useDraftsStore, DraftActionError, DRAFT_PAGE_SIZE } = await import('../src/stores/drafts')
const { useAgentStore } = await import('../src/stores/agent')

const draft = (id: string, over: Record<string, unknown> = {}) => ({
  id, flowId: 'f1', flowName: 'Garage', isNew: false, baseVersion: 3, currentVersion: 3, stale: false, status: 'pending',
  risk: 'external', alertCount: 1, issueCounts: { error: 0, warning: 0 }, note: 'note', tokenId: 'tokA', tokenName: 'laptop',
  createdAt: '2026-11-08T12:00:00Z', decidedAt: null, ...over
})
const detail = (id: string, over: Record<string, unknown> = {}) => ({
  ...draft(id), issues: [], running: false, lastApprovedVersion: 3, diffApproved: null, flow: { id: 'f1', nodes: [] },
  diff: { nodes: [], connections: { added: [], removed: [] }, riskBefore: 'safe', riskAfter: 'external', alerts: [] }, ...over
})

beforeEach(() => {
  setActivePinia(createPinia())
  calls = []
  routes = {}
})

test('drafts are listed by status, newest first, and paged with nextBefore', async () => {
  routes['GET /agent/drafts'] = (c) => {
    const p = c.params as { beforeId?: string }
    return p.beforeId ? { drafts: [draft('c')], nextBefore: '' } : { drafts: [draft('a'), draft('b')], nextBefore: 'b' }
  }
  const store = useDraftsStore()
  expect(await store.fetchDrafts('all')).toBe(true)
  expect(calls[0].params).toEqual({ status: 'all', limit: DRAFT_PAGE_SIZE })
  expect(calls[0].headers).toBeUndefined()
  expect(store.drafts.map(d => d.id)).toEqual(['a', 'b'])
  expect(store.hasMore).toBe(true)
  await store.loadMore()
  expect(calls[1].params).toEqual({ status: 'all', limit: DRAFT_PAGE_SIZE, beforeId: 'b' })
  expect(store.drafts.map(d => d.id)).toEqual(['a', 'b', 'c'])
  expect(store.hasMore).toBe(false)
  // An empty or null page is no drafts.
  routes['GET /agent/drafts'] = () => ({ drafts: null, nextBefore: '' })
  await store.fetchDrafts('pending')
  expect(store.drafts).toEqual([])
  expect(store.filter).toBe('pending')
})

test('a background reload of the list marks itself so the session is not extended', async () => {
  routes['GET /agent/drafts'] = () => ({ drafts: [draft('a')], nextBefore: '' })
  const store = useDraftsStore()
  await store.fetchDrafts('pending', { background: true })
  expect(calls[0].headers).toEqual({ 'X-PiWorker-Background': '1' })
  routes['GET /agent/drafts/a'] = () => detail('a')
  await store.fetchDraft('a', { background: true })
  expect(calls[1].headers).toEqual({ 'X-PiWorker-Background': '1' })
  await store.fetchDraft('a')
  expect(calls[2].headers).toBeUndefined()
})

test('a background reload keeps the pages loaded with Load more and says the list is out of date', async () => {
  routes['GET /agent/drafts'] = (c) => {
    const p = c.params as { beforeId?: string }
    return p.beforeId ? { drafts: [draft('c')], nextBefore: '' } : { drafts: [draft('a'), draft('b')], nextBefore: 'b' }
  }
  const store = useDraftsStore()
  await store.fetchDrafts('all')
  // While only the first page is shown, a background reload replaces it.
  expect(await store.fetchDrafts('all', { background: true })).toBe(true)
  expect(store.outdated).toBe(false)
  await store.loadMore()
  expect(store.drafts.map(d => d.id)).toEqual(['a', 'b', 'c'])
  const before = calls.length

  // An agent saves a draft: the badge's count changes and the page asks
  // for a background reload. It would replace the list with the first
  // page, so it is skipped and the page offers a refresh instead.
  expect(await store.fetchDrafts('all', { background: true })).toBe(false)
  expect(calls.length).toBe(before)
  expect(store.drafts.map(d => d.id)).toEqual(['a', 'b', 'c'])
  expect(store.outdated).toBe(true)

  // Refreshing by hand reads the first page again.
  routes['GET /agent/drafts'] = () => ({ drafts: [draft('n'), draft('a')], nextBefore: 'a' })
  expect(await store.fetchDrafts('all')).toBe(true)
  expect(store.drafts.map(d => d.id)).toEqual(['n', 'a'])
  expect(store.outdated).toBe(false)
})

test('a background reload never discards a Load more in flight', async () => {
  routes['GET /agent/drafts'] = (c) => {
    const p = c.params as { beforeId?: string }
    return p.beforeId ? { drafts: [draft('c')], nextBefore: '' } : { drafts: [draft('a'), draft('b')], nextBefore: 'b' }
  }
  const store = useDraftsStore()
  await store.fetchDrafts('all')
  let release = () => {}
  const realGet = apiClient.get
  apiClient.get = async (url: string, config?: { params?: unknown; headers?: Record<string, string> }) => {
    if ((config?.params as { beforeId?: string })?.beforeId) await new Promise<void>(resolve => { release = resolve })
    return realGet(url, config)
  }
  try {
    const more = store.loadMore()
    await new Promise(r => setTimeout(r, 0))
    expect(await store.fetchDrafts('all', { background: true })).toBe(false)
    expect(store.outdated).toBe(true)
    release()
    await more
    expect(store.drafts.map(d => d.id)).toEqual(['a', 'b', 'c'])
  } finally {
    apiClient.get = realGet
  }
})

test('a slower, older list never replaces a newer one', async () => {
  const gates: Record<string, () => void> = {}
  routes['GET /agent/drafts'] = (c) => ({ drafts: [draft((c.params as { status: string }).status)], nextBefore: '' })
  const realGet = apiClient.get
  apiClient.get = async (url: string, config?: { params?: unknown }) => {
    const status = (config?.params as { status: string }).status
    await new Promise<void>(resolve => { gates[status] = resolve })
    return realGet(url, config)
  }
  try {
    const store = useDraftsStore()
    const older = store.fetchDrafts('pending')
    const newer = store.fetchDrafts('all')
    await new Promise(r => setTimeout(r, 0))
    gates.all()
    expect(await newer).toBe(true)
    gates.pending()
    expect(await older).toBe(false)
    expect(store.drafts.map(d => d.id)).toEqual(['all'])
    expect(store.filter).toBe('all')
  } finally {
    apiClient.get = realGet
  }
})

test('a draft is read by its encoded ID; a late read of another draft is ignored', async () => {
  let release = () => {}
  routes['GET /agent/drafts/a%2Fb'] = () => detail('a/b')
  routes['GET /agent/drafts/c'] = () => detail('c')
  const store = useDraftsStore()
  const realGet = apiClient.get
  apiClient.get = async (url: string, config?: { params?: unknown }) => {
    if (url.endsWith('a%2Fb')) await new Promise<void>(resolve => { release = resolve })
    return realGet(url, config)
  }
  try {
    const first = store.fetchDraft('a/b')
    await new Promise(r => setTimeout(r, 0))
    await store.fetchDraft('c')
    release()
    expect(await first).toBeNull()
    expect(store.detail?.id).toBe('c')
  } finally {
    apiClient.get = realGet
  }
})

test('approving sends the deploy choice, then refreshes the draft and the badge', async () => {
  let status = 'pending'
  routes['GET /agent/drafts/d1'] = () => detail('d1', { status, appliedVersion: status === 'approved' ? 4 : undefined })
  routes['POST /agent/drafts/d1/approve'] = () => { status = 'approved'; return { flowId: 'f1', version: 4, deployed: true } }
  routes['GET /agent/approvals/pending-count'] = () => ({ pending: 0, drafts: 0 })
  const store = useDraftsStore()
  await store.fetchDraft('d1')
  const result = await store.approve('d1', true)
  expect(result).toEqual({ flowId: 'f1', version: 4, deployed: true })
  expect(calls.find(c => c.method === 'POST')!.body).toEqual({ deploy: true })
  expect(store.detail?.status).toBe('approved')
  await new Promise(r => setTimeout(r, 0))
  expect(calls.some(c => c.url === '/agent/approvals/pending-count')).toBe(true)
  // Approving without deploying says so.
  routes['POST /agent/drafts/d2/approve'] = () => ({ flowId: 'f1', version: 5, deployed: false })
  routes['GET /agent/drafts/d2'] = () => detail('d2', { status: 'approved' })
  await store.approve('d2', false)
  expect(calls.filter(c => c.method === 'POST').at(-1)!.body).toEqual({ deploy: false })
})

test('a stale or decided draft is a conflict with the current version, and the page reloads it', async () => {
  let reads = 0
  routes['GET /agent/drafts/d1'] = () => { reads++; return detail('d1', reads > 1 ? { stale: true, currentVersion: 5 } : {}) }
  routes['POST /agent/drafts/d1/approve'] = () => {
    throw httpError(409, 'The flow was changed since the draft was written', { code: 'conflict', currentVersion: 5 })
  }
  const store = useDraftsStore()
  await store.fetchDraft('d1')
  let caught: unknown
  try { await store.approve('d1', false) } catch (e) { caught = e }
  expect(caught).toBeInstanceOf(DraftActionError)
  const err = caught as InstanceType<typeof DraftActionError>
  expect(err.status).toBe(409)
  expect(err.code).toBe('conflict')
  expect(err.currentVersion).toBe(5)
  expect(err.message).toBe('The flow was changed since the draft was written')
  expect(store.detail?.stale).toBe(true)
  expect(reads).toBe(2)
})

test('an approval the server finds invalid lists the issues; the draft stays pending', async () => {
  const issues = [{ path: '/nodes/a/config/token', code: 'undefined_secret', severity: 'error', message: 'secret BOT is not defined' }]
  routes['GET /agent/drafts/d1'] = () => detail('d1')
  routes['POST /agent/drafts/d1/approve'] = () => { throw httpError(400, 'The draft cannot be deployed', { code: 'invalid_arguments', flowIssues: issues }) }
  const store = useDraftsStore()
  await store.fetchDraft('d1')
  let caught: unknown
  try { await store.approve('d1', true) } catch (e) { caught = e }
  const err = caught as InstanceType<typeof DraftActionError>
  expect(err).toBeInstanceOf(DraftActionError)
  expect(err.status).toBe(400)
  expect(err.flowIssues).toEqual(issues as any)
  expect(err.currentVersion).toBeUndefined()
  expect(store.detail?.status).toBe('pending')
  // Anything else keeps the server's message, or a fallback.
  routes['POST /agent/drafts/d1/approve'] = () => { throw new Error('offline') }
  try { await store.approve('d1', true) } catch (e) { caught = e }
  expect((caught as InstanceType<typeof DraftActionError>).message).toBe('Could not approve the draft. Try again.')
  expect((caught as InstanceType<typeof DraftActionError>).status).toBeUndefined()
})

test('rejecting sends the trimmed reason, or none', async () => {
  routes['POST /agent/drafts/d1/reject'] = () => ({})
  routes['GET /agent/drafts/d1'] = () => detail('d1', { status: 'rejected' })
  const store = useDraftsStore()
  await store.reject('d1', '  Too broad  ')
  expect(calls[0].body).toEqual({ reason: 'Too broad' })
  expect(store.detail?.status).toBe('rejected')
  await store.reject('d1', '   ')
  expect(calls.filter(c => c.method === 'POST').at(-1)!.body).toEqual({})
  routes['POST /agent/drafts/d1/reject'] = () => { throw httpError(409, 'The draft is not pending', { code: 'conflict' }) }
  await expect(store.reject('d1', '')).rejects.toBeInstanceOf(DraftActionError)
})

test('the badge reads the pending drafts from the same passive count, as a background request', async () => {
  routes['GET /agent/approvals/pending-count'] = () => ({ pending: 2, drafts: 3 })
  const agent = useAgentStore()
  await agent.refreshPendingCount()
  expect(calls[0].headers).toEqual({ 'X-PiWorker-Background': '1' })
  expect(agent.pendingCount).toBe(2)
  expect(agent.pendingDrafts).toBe(3)
  // An older server without drafts counts none.
  routes['GET /agent/approvals/pending-count'] = () => ({ pending: 1 })
  await agent.refreshPendingCount()
  expect(agent.pendingDrafts).toBe(0)
  // No agent API: nothing pending.
  delete routes['GET /agent/approvals/pending-count']
  agent.pendingDrafts = 4
  await agent.refreshPendingCount()
  expect(agent.pendingDrafts).toBe(0)
})
