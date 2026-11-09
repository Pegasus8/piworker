import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import { watchSyncEffect } from 'vue'
mock.module('../src/stores/nodeTypes', () => ({
  useNodeTypesStore: () => ({ fetchNodeTypes: async () => {}, getById: () => undefined })
}))
const apiMock: {
  getFlow: (id: string) => Promise<unknown>
  getFlows: () => Promise<any[]>
  updateFlow: (id: string, flow: any) => Promise<any>
  deployFlow: (id: string, version?: number) => Promise<boolean>
  toggleFlow: (id: string, enabled: boolean, version?: number) => Promise<boolean>
  getFlowVersions: (id: string, limit?: number) => Promise<any>
  rollbackFlow: (id: string) => Promise<any>
} = {
  getFlow: async () => ({}), getFlows: async () => [], updateFlow: async (_id, flow) => flow, deployFlow: async () => true,
  toggleFlow: async (_id, enabled) => enabled, getFlowVersions: async () => ({ current: 1, lastAdminApproved: 1, versions: [] }),
  rollbackFlow: async () => ({ flowId: 'a', version: 2, deployed: false })
}
mock.module('../src/composables/useApi', () => ({ useApi: () => apiMock }))
const { useFlowsStore, FlowVersionConflictError } = await import('../src/stores/flows')

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const backendFlow = (id: string) => ({
  id, name: id, state: 'inactive', running: false, connections: [],
  nodes: [{ id: `${id}-node`, type: 'trigger-manual', category: 'input', position: { x: 0, y: 0 }, config: {} }]
})

test('switching flows quickly ignores the response of the flow left behind', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  const pending: Record<string, ReturnType<typeof deferred<unknown>>> = { a: deferred(), b: deferred() }
  apiMock.getFlow = (id: string) => pending[id].promise

  const first = store.fetchFlow('a')
  const second = store.fetchFlow('b')
  pending.b.resolve(backendFlow('b'))
  await second
  pending.a.resolve(backendFlow('a'))
  await first

  expect(store.currentFlow?.id).toBe('b')
  expect(store.nodes.map(n => n.id)).toEqual(['b-node'])
  expect(store.loading).toBe(false)
})

test('starting a new flow discards a load still in flight', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  const pending = deferred<unknown>()
  apiMock.getFlow = () => pending.promise

  const load = store.fetchFlow('a')
  store.initNewFlow()
  pending.resolve(backendFlow('a'))
  await load

  expect(store.currentFlow?.id).toBe('')
  expect(store.nodes).toHaveLength(0)
  expect(store.loading).toBe(false)
})

test('a stale failure does not overwrite the current flow with an error', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  const pending: Record<string, ReturnType<typeof deferred<unknown>>> = { a: deferred(), b: deferred() }
  apiMock.getFlow = (id: string) => pending[id].promise

  const first = store.fetchFlow('a')
  const second = store.fetchFlow('b')
  pending.b.resolve(backendFlow('b'))
  await second
  pending.a.reject(new Error('network down'))
  await first

  expect(store.currentFlow?.id).toBe('b')
  expect(store.error).toBeNull()
})

test('selected node configuration stays reactive and removing a node clears its connections', () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  store.initNewFlow()
  store.addNode({ id: 'source', type: 'trigger', position: { x: 10, y: 20 }, data: {
    label: 'Timer', nodeType: 'trigger-interval', category: 'trigger', icon: 'Timer', config: { interval: 1 }
  } })
  store.selectNode('source')
  const seen: unknown[] = []
  const stop = watchSyncEffect(() => { seen.push(store.selectedNode?.data.config.interval) })
  try {
    expect(seen.at(-1)).toBe(1)
    store.updateNodeConfig('source', { interval: 5 })
    expect(seen.at(-1)).toBe(5)
    store.updateNode('source', { position: { x: 30, y: 40 } })
    expect(store.selectedNode?.position).toEqual({ x: 30, y: 40 })
    store.addEdge({ id: 'connection', source: 'source', target: 'target', animated: true })
    expect(store.edges).toHaveLength(1)
    store.removeNode('source')
    expect(store.selectedNode).toBeNull()
    expect(store.edges).toHaveLength(0)
    expect(store.isDirty).toBe(true)
  } finally { stop() }
})

// An axios-shaped rejection.
function httpError(status: number, data: unknown) {
  return Object.assign(new Error(`Request failed with status code ${status}`), { response: { status, data } })
}

test('saving sends the version the editor loaded and keeps the new one', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 3 })
  let sent: any
  apiMock.updateFlow = async (_id, flow) => { sent = flow; return { ...flow, version: 4 } }
  await store.fetchFlow('a')
  store.isDirty = true

  await store.saveFlow()

  expect(sent.version).toBe(3)
  expect(store.currentFlow?.version).toBe(4)
  expect(store.isDirty).toBe(false)
})

test('a stale save offers no silent overwrite: it reports the current version', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 3 })
  apiMock.updateFlow = async () => {
    throw httpError(409, { success: false, error: 'The flow was changed since you loaded it', data: { currentVersion: 5 } })
  }
  await store.fetchFlow('a')
  store.isDirty = true

  let caught: unknown
  try { await store.saveFlow() } catch (e) { caught = e }

  expect(caught).toBeInstanceOf(FlowVersionConflictError)
  expect((caught as InstanceType<typeof FlowVersionConflictError>).currentVersion).toBe(5)
  expect(store.isDirty).toBe(true)
  expect(store.currentFlow?.version).toBe(3)
  expect(store.nodes.map(n => n.id)).toEqual(['a-node'])
})

test('other conflicts (a running flow) are ordinary save errors', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 1 })
  apiMock.updateFlow = async () => { throw httpError(409, { success: false, error: 'Cannot update a running flow; stop it first' }) }
  await store.fetchFlow('a')

  let caught: unknown
  try { await store.saveFlow() } catch (e) { caught = e }

  expect(caught).toBeInstanceOf(Error)
  expect(caught).not.toBeInstanceOf(FlowVersionConflictError)
})

test('deploying the open flow sends the version the editor shows', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 3 })
  const calls: Array<[string, number | undefined]> = []
  apiMock.deployFlow = async (id, version) => { calls.push([id, version]); return true }
  await store.fetchFlow('a')

  await store.deployFlow()
  await store.deployFlow('other') // a flow the editor does not show: no version

  expect(calls).toEqual([['a', 3], ['other', undefined]])
  expect(store.currentFlow?.running).toBe(true)
})

test('a stale deploy reports the current version and deploys nothing', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 3 })
  apiMock.deployFlow = async () => {
    throw httpError(409, { success: false, error: 'The flow was changed since you loaded it', data: { currentVersion: 4 } })
  }
  await store.fetchFlow('a')

  let caught: unknown
  try { await store.deployFlow() } catch (e) { caught = e }

  expect(caught).toBeInstanceOf(FlowVersionConflictError)
  expect((caught as InstanceType<typeof FlowVersionConflictError>).currentVersion).toBe(4)
  expect(store.currentFlow?.running).toBeFalsy()
  expect(store.isDeploying).toBe(false)
})

const listItem = (id: string, over: Record<string, unknown> = {}) => ({
  id, name: id, state: 'inactive', running: false, nodeCount: 1, createdAt: '', updatedAt: '', version: 3,
  deployment: { version: 2, actorKind: 'agent', tokenName: 'laptop', deployedAt: '2026-11-08T12:00:00Z' }, agentChanged: true, ...over
})

test('turning a flow on from the list sends the version the list shows; an editor deploy replaces the agent marker', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlows = async () => [listItem('a')]
  const calls: Array<[string, boolean, number | undefined]> = []
  apiMock.toggleFlow = async (id, enabled, version) => { calls.push([id, enabled, version]); return enabled }
  await store.fetchFlows()

  await store.toggleFlow('a', true)
  expect(calls).toEqual([['a', true, 3]])
  expect(store.flows[0].running).toBe(true)
  expect(store.flows[0].deployment).toMatchObject({ actorKind: 'editor', version: 3 })

  // Stopping keeps the marker (it names the last deployment) and sends no version.
  await store.toggleFlow('a', false)
  expect(calls[1]).toEqual(['a', false, undefined])
  expect(store.flows[0].deployment).toMatchObject({ actorKind: 'editor' })
})

test('turning on a flow saved since the list loaded is a version conflict, not a deploy', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlows = async () => [listItem('a')]
  apiMock.toggleFlow = async () => {
    throw httpError(409, { success: false, error: 'The flow was changed since you loaded it', data: { currentVersion: 4 } })
  }
  await store.fetchFlows()

  let caught: unknown
  try { await store.toggleFlow('a', true) } catch (e) { caught = e }

  expect(caught).toBeInstanceOf(FlowVersionConflictError)
  expect((caught as InstanceType<typeof FlowVersionConflictError>).currentVersion).toBe(4)
  expect(store.flows[0].running).toBe(false)
  expect(store.flows[0].deployment).toMatchObject({ actorKind: 'agent' })
})

test('an editor save is a version you approved: the agent change mark clears, the deployment stays', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  const deployment = { version: 3, actorKind: 'agent', tokenName: 'laptop', deployedAt: '2026-11-08T12:00:00Z' }
  apiMock.getFlow = async () => ({ ...backendFlow('a'), version: 3, deployment, agentChanged: true })
  apiMock.getFlows = async () => [listItem('a')]
  apiMock.updateFlow = async (_id, flow) => ({ ...flow, version: 4 })
  await store.fetchFlows()
  await store.fetchFlow('a')
  store.isDirty = true

  await store.saveFlow()

  expect(store.currentFlow?.agentChanged).toBe(false)
  expect(store.currentFlow?.deployment).toEqual(deployment)
  expect(store.flows[0]).toMatchObject({ version: 4, agentChanged: false })
})

test('the version history of a flow is read through the API', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  let asked: [string, number | undefined] | null = null
  apiMock.getFlowVersions = async (id, limit) => { asked = [id, limit]; return { current: 5, lastAdminApproved: 3, versions: [] } }
  const list = await store.fetchVersions('a')
  expect(asked!).toEqual(['a', 50])
  expect(list.lastAdminApproved).toBe(3)
})

test('rolling back updates the list, and reloads the flow the editor shows', async () => {
  setActivePinia(createPinia())
  const store = useFlowsStore()
  apiMock.getFlows = async () => [listItem('a', { running: true, state: 'running' }), listItem('b')]
  let loads = 0
  apiMock.getFlow = async () => { loads++; return { ...backendFlow('a'), version: loads > 1 ? 6 : 5, agentChanged: loads === 1 } }
  apiMock.rollbackFlow = async (id) => ({ flowId: id, version: 6, deployed: true })
  await store.fetchFlows()
  await store.fetchFlow('a')

  const result = await store.rollbackFlow('a')

  expect(result).toEqual({ flowId: 'a', version: 6, deployed: true })
  expect(store.flows[0]).toMatchObject({ version: 6, agentChanged: false, running: true })
  expect(store.flows[0].deployment).toMatchObject({ actorKind: 'editor', version: 6 })
  expect(loads).toBe(2)
  expect(store.currentFlow?.version).toBe(6)
  expect(store.isDirty).toBe(false)

  // A failed redeploy leaves the flow stopped (its running intent kept: failed).
  apiMock.rollbackFlow = async (id) => ({ flowId: id, version: 4, deployed: false, deployError: { status: 400, message: 'bad' } })
  await store.rollbackFlow('b')
  expect(store.flows[1]).toMatchObject({ version: 4, agentChanged: false, running: false })
  expect(loads).toBe(2)
})
