import { beforeAll, beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'
import type { AssistantFlow, NodeTypeConfig, StoredFlowNode } from '../src/types'

// The node types the store sees.
const port = (id: string) => ({ id, name: id, dataType: 'any' })
const nodeTypes: Record<string, NodeTypeConfig> = {
  'trigger-manual': { id: 'trigger-manual', name: 'Manual trigger', category: 'trigger', icon: 'Play', description: '', risk: 'safe', inputs: [], outputs: [port('output')], configSchema: [] },
  'process-llm': { id: 'process-llm', name: 'LLM prompt', category: 'process', icon: 'Sparkles', description: '', risk: 'external', inputs: [port('input')], outputs: [port('output')], configSchema: [] }
}
mock.module('../src/stores/nodeTypes', () => ({
  useNodeTypesStore: () => ({ fetchNodeTypes: async () => {}, getById: (id: string) => nodeTypes[id] })
}))
// bun keeps a mocked module's export names for the whole run, so every
// mock of useApi exports the same names (apiClient too).
mock.module('../src/composables/useApi', () => ({ apiClient: {}, useApi: () => ({}) }))
const { useFlowsStore } = await import('../src/stores/flows')
const { setLocale, msg } = await import('../src/i18n')

beforeAll(() => setLocale('en'))
beforeEach(() => setActivePinia(createPinia()))

function stored(id: string, type: string, category: string, position: { x: number; y: number }, config: Record<string, unknown> = {}): StoredFlowNode {
  return { id, type, category, position, config, enabled: true, inputs: [], outputs: [] }
}
function proposal(): AssistantFlow {
  return {
    name: 'Morning summary',
    description: 'Summarises the news',
    nodes: [
      stored('t', 'trigger-manual', 'input', { x: 0, y: 0 }),
      stored('llm', 'process-llm', 'processing', { x: 270, y: 0 }, { prompt: '{{payload}}' }),
      stored('x', 'process-teleport', '', { x: 540, y: 0 })
    ],
    connections: [
      { id: 'c1', sourceNode: 't', sourcePort: 'output', targetNode: 'llm', targetPort: 'input' },
      { id: 'c2', sourceNode: 'llm', sourcePort: 'output', targetNode: 'x', targetPort: 'input' }
    ]
  }
}

test('a proposal on an empty canvas becomes editor nodes, as a loaded flow does, in one undo step', () => {
  const store = useFlowsStore()
  store.initNewFlow()
  const applied = store.applyProposal(proposal(), 'replace')
  expect(applied).toEqual({ nodes: 2, connections: 1, skippedTypes: ['process-teleport'], skippedConnections: 1 })
  expect(store.nodes.map(n => [n.id, n.type, n.data.label, n.data.icon, n.data.category, n.data.nodeType])).toEqual([
    ['t', 'trigger', 'Manual trigger', 'Play', 'trigger', 'trigger-manual'],
    ['llm', 'process', 'LLM prompt', 'Sparkles', 'process', 'process-llm']
  ])
  expect(store.nodes[1].data.config).toEqual({ prompt: '{{payload}}' })
  expect(store.edges).toEqual([{ id: 'c1', source: 't', target: 'llm', sourceHandle: 'output', targetHandle: 'input', animated: true }])
  expect(store.isDirty).toBe(true)

  store.undo()
  expect(store.nodes).toHaveLength(0)
  expect(store.edges).toHaveLength(0)
  expect(store.canUndo).toBe(false)
  store.redo()
  expect(store.nodes).toHaveLength(2)
})

test('a new, still untitled flow takes the proposal’s name and description', () => {
  const store = useFlowsStore()
  store.initNewFlow()
  store.applyProposal(proposal(), 'replace')
  expect(store.currentFlow?.name).toBe('Morning summary')
  expect(store.currentFlow?.description).toBe('Summarises the news')
})

test('a flow that already has a name keeps it', () => {
  const store = useFlowsStore()
  store.initNewFlow('Kitchen lights')
  store.applyProposal(proposal(), 'replace')
  expect(store.currentFlow?.name).toBe('Kitchen lights')

  const saved = useFlowsStore()
  saved.initNewFlow()
  saved.currentFlow!.id = 'f1'
  saved.currentFlow!.name = msg().editor.untitled
  saved.applyProposal(proposal(), 'replace')
  expect(saved.currentFlow?.name).toBe(msg().editor.untitled)
})

test('adding places the proposal beside the current nodes with new IDs; undo removes all of it', () => {
  const store = useFlowsStore()
  store.initNewFlow()
  store.applyProposal(proposal(), 'replace')
  store.applyProposal(proposal(), 'add')
  expect(store.nodes).toHaveLength(4)
  expect(new Set(store.nodes.map(n => n.id)).size).toBe(4)
  const [, , trigger, llm] = store.nodes
  expect(trigger.position.x).toBeGreaterThan(270)
  expect(store.edges).toHaveLength(2)
  expect(store.edges[1]).toMatchObject({ source: trigger.id, target: llm.id, sourceHandle: 'output', targetHandle: 'input' })
  // The canvas's settings and the added ones are separate objects.
  expect(llm.data.config).not.toBe(store.nodes[1].data.config)

  store.undo()
  expect(store.nodes.map(n => n.id)).toEqual(['t', 'llm'])
  expect(store.edges.map(e => e.id)).toEqual(['c1'])
})

test('a change of the current flow replaces its nodes, deselecting one that is gone; undo brings them back', () => {
  const store = useFlowsStore()
  store.initNewFlow()
  store.applyProposal(proposal(), 'replace')
  store.selectNode('llm')
  const changed = proposal()
  changed.nodes = [changed.nodes[0]]
  changed.connections = []
  store.applyProposal(changed, 'replace')
  expect(store.nodes.map(n => n.id)).toEqual(['t'])
  expect(store.edges).toHaveLength(0)
  expect(store.selectedNodeId).toBeNull()
  store.undo()
  expect(store.nodes.map(n => n.id)).toEqual(['t', 'llm'])
})

test('a proposal with no node of a known type changes nothing', () => {
  const store = useFlowsStore()
  store.initNewFlow()
  const unknown = proposal()
  unknown.nodes = [unknown.nodes[2]]
  expect(store.applyProposal(unknown, 'replace')).toEqual({ nodes: 0, connections: 0, skippedTypes: ['process-teleport'], skippedConnections: 2 })
  expect(store.canUndo).toBe(false)
  expect(store.isDirty).toBe(false)
  expect(store.currentFlow?.name).toBe(msg().editor.untitled)
})
