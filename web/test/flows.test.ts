import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import { watchSyncEffect } from 'vue'
mock.module('../src/stores/nodeTypes', () => ({
  useNodeTypesStore: () => ({ fetchNodeTypes: async () => {}, getById: () => undefined })
}))
const apiMock: {
  getFlow: (id: string) => Promise<unknown>
  updateFlow: (id: string, flow: any) => Promise<any>
  deployFlow: (id: string, version?: number) => Promise<boolean>
} = { getFlow: async () => ({}), updateFlow: async (_id, flow) => flow, deployFlow: async () => true }
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
