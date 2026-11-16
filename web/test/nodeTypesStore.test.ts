import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

let nodeTypes: unknown[] = []
// bun keeps a mocked module's export names for the whole run: export the
// same names as the other mocks of useApi.
mock.module('../src/composables/useApi', () => ({ apiClient: {}, useApi: () => ({ getNodeTypes: async () => ({ nodeTypes }) }) }))
// Other test files replace this module with fakes; load the real one.
const { useNodeTypesStore } = await import('../src/stores/nodeTypes.ts?real')

// GET /api/node-types (docs/design/llm.md §5): outputsFrom names the list
// setting whose items are extra output ports; array settings are lists.
test('node types keep outputsFrom and turn array settings into lists', async () => {
  nodeTypes = [
    {
      type: 'process-llm-classify', name: 'LLM classify', category: 'processing', icon: 'tags', risk: 'external',
      inputs: [{ id: 'input', name: 'Input' }], outputs: [{ id: 'other', name: 'Other' }], outputsFrom: 'categories',
      config: { required: ['categories'], properties: { categories: { type: 'array', title: 'Categories' }, connection: { type: 'string', options: 'llm-connections' } } }
    },
    { type: 'action-log', name: 'Log', category: 'output', icon: 'file-text', risk: 'safe', inputs: [{ id: 'input' }], outputs: [] }
  ]
  setActivePinia(createPinia())
  const store = useNodeTypesStore()
  await store.fetchNodeTypes()
  const classify = store.getById('process-llm-classify')!
  expect(classify.outputsFrom).toBe('categories')
  expect(classify.icon).toBe('Tags')
  expect(classify.configSchema.find(f => f.key === 'categories')?.type).toBe('list')
  expect(classify.configSchema.find(f => f.key === 'connection')?.suggest).toBe('llm-connections')
  expect(store.getById('action-log')!.outputsFrom).toBeUndefined()
})
