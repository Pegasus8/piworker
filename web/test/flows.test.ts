import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import { watchSyncEffect } from 'vue'
mock.module('../src/stores/nodeTypes', () => ({
  useNodeTypesStore: () => ({ fetchNodeTypes: async () => {}, getById: () => undefined })
}))
const apiMock: { getFlow: (id: string) => Promise<unknown> } = { getFlow: async () => ({}) }
mock.module('../src/composables/useApi', () => ({ useApi: () => apiMock }))
const { useFlowsStore } = await import('../src/stores/flows')

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
