import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import { watchSyncEffect } from 'vue'
mock.module('../src/stores/nodeTypes', () => ({ useNodeTypesStore: () => ({}) }))
const { useFlowsStore } = await import('../src/stores/flows')

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
