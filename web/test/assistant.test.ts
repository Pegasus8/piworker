import { beforeAll, expect, test } from 'bun:test'
import { setLocale } from '../src/i18n'
import {
  MAX_DESCRIPTION_CHARS,
  applyMode,
  assistantError,
  canvasFingerprint,
  changeSummary,
  currentDefinition,
  descriptionLength,
  issueLocation,
  prepareProposal,
  proposalView
} from '../src/lib/assistant'
import type { AssistantFlow, FlowEdge, FlowNode, NodeTypeConfig, StoredFlowConnection, StoredFlowNode } from '../src/types'

beforeAll(() => setLocale('en'))

const port = (id: string) => ({ id, name: id, dataType: 'any' })
const types: Record<string, NodeTypeConfig> = {
  'trigger-manual': { id: 'trigger-manual', name: 'Manual trigger', category: 'trigger', icon: 'Play', description: '', risk: 'safe', inputs: [], outputs: [port('output')], configSchema: [] },
  'process-llm': { id: 'process-llm', name: 'LLM prompt', category: 'process', icon: 'Sparkles', description: '', risk: 'external', inputs: [port('input')], outputs: [port('output')], configSchema: [] },
  'process-llm-agent': { id: 'process-llm-agent', name: 'AI agent', category: 'process', icon: 'Bot', description: '', risk: 'external', riskCanRise: true, inputs: [port('input')], outputs: [port('output')], configSchema: [] },
  'action-command': { id: 'action-command', name: 'Run command', category: 'action', icon: 'Terminal', description: '', risk: 'privileged', inputs: [port('input')], outputs: [], configSchema: [] }
}
const getType = (id: string) => types[id]

function stored(id: string, type: string, category: string, position = { x: 0, y: 0 }, config: Record<string, unknown> = {}): StoredFlowNode {
  return { id, type, category, position, config, enabled: true, inputs: [], outputs: [] }
}
const conn = (id: string, sourceNode: string, targetNode: string, sourcePort = 'output', targetPort = 'input'): StoredFlowConnection =>
  ({ id, sourceNode, sourcePort, targetNode, targetPort })

// A trigger, a prompt and a node of a type this editor does not know.
function proposal(): AssistantFlow {
  return {
    name: 'Morning summary',
    description: 'Summarises the news',
    nodes: [
      stored('t', 'trigger-manual', 'input', { x: 0, y: 0 }),
      stored('llm', 'process-llm', 'processing', { x: 270, y: 30 }, { prompt: '{{payload}}', schema: { type: 'object' } }),
      stored('x', 'process-teleport', '', { x: 540, y: 0 })
    ],
    connections: [conn('c1', 't', 'llm'), conn('c2', 'llm', 'x')]
  }
}

function canvasNode(id: string, type: string, position: { x: number; y: number }, config: Record<string, any> = {}): FlowNode {
  const t = types[type]
  return { id, type: t.category, position, data: { label: t.name, nodeType: type, category: t.category, icon: t.icon, config, enabled: true } }
}

test('the description is counted in characters, as the server does, after trimming', () => {
  expect(descriptionLength('  héllo 👋 ')).toBe(7)
  expect(MAX_DESCRIPTION_CHARS).toBe(4000)
})

// An empty canvas takes the proposal as it is; so does a change of the
// current flow (the proposal is the whole changed flow). Otherwise it is a
// second flow, placed beside the nodes already there.
test('an empty canvas or a change of the current flow is replaced; otherwise the proposal is added', () => {
  expect(applyMode(0, false)).toBe('replace')
  expect(applyMode(0, true)).toBe('replace')
  expect(applyMode(3, true)).toBe('replace')
  expect(applyMode(3, false)).toBe('add')
})

test('the current definition is the canvas in the stored shape, without sharing its settings', () => {
  const nodes = [canvasNode('a', 'trigger-manual', { x: 1, y: 2 }), canvasNode('b', 'process-llm', { x: 300, y: 2 }, { prompt: 'hi' })]
  nodes[1].data.enabled = false
  const edges: FlowEdge[] = [{ id: 'e1', source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }, { id: 'e2', source: 'a', target: 'b' }]
  const def = currentDefinition({ name: 'Mine', description: undefined }, nodes, edges)
  expect(def.name).toBe('Mine')
  expect(def.description).toBe('')
  expect(def.nodes.map(n => [n.id, n.type, n.category, n.enabled])).toEqual([['a', 'trigger-manual', 'input', true], ['b', 'process-llm', 'processing', false]])
  expect(def.nodes[0].position).toEqual({ x: 1, y: 2 })
  expect(def.nodes[1].config).toEqual({ prompt: 'hi' })
  def.nodes[1].config.prompt = 'changed'
  expect(nodes[1].data.config.prompt).toBe('hi')
  expect(def.connections).toEqual([
    { id: 'e1', sourceNode: 'a', sourcePort: 'output', targetNode: 'b', targetPort: 'input' },
    { id: 'e2', sourceNode: 'a', sourcePort: 'output', targetNode: 'b', targetPort: 'input' }
  ])
  expect(currentDefinition(null, [], []).name).toBe('')
})

// A change of the current flow is built from the canvas as it was sent:
// once the nodes, their settings or positions, or the connections change,
// applying it would undo those edits (the dialog refuses it). The flow's
// name is not part of it: the proposal never renames a saved flow.
test('the canvas fingerprint changes with the nodes, their settings and the connections', () => {
  const nodes = () => [canvasNode('a', 'trigger-manual', { x: 1, y: 2 }), canvasNode('b', 'process-llm', { x: 300, y: 2 }, { prompt: 'hi' })]
  const edges = (): FlowEdge[] => [{ id: 'e1', source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }]
  const sent = canvasFingerprint(nodes(), edges())
  expect(canvasFingerprint(nodes(), edges())).toBe(sent)
  const edited = nodes()
  edited[1].data.config = { prompt: 'hello' }
  expect(canvasFingerprint(edited, edges())).not.toBe(sent)
  const moved = nodes()
  moved[0].position = { x: 50, y: 2 }
  expect(canvasFingerprint(moved, edges())).not.toBe(sent)
  const disabled = nodes()
  disabled[1].data.enabled = false
  expect(canvasFingerprint(disabled, edges())).not.toBe(sent)
  expect(canvasFingerprint([...nodes(), canvasNode('c', 'process-llm', { x: 600, y: 2 })], edges())).not.toBe(sent)
  expect(canvasFingerprint(nodes(), [])).not.toBe(sent)
  expect(canvasFingerprint(nodes(), [{ ...edges()[0], targetHandle: 'other' }])).not.toBe(sent)
})

test('the proposal view names each node by its type, flags unknown types and counts what will be placed', () => {
  const view = proposalView(proposal(), getType)
  expect(view.nodes.map(n => [n.id, n.label, n.known, n.risk])).toEqual([
    ['t', 'Manual trigger', true, 'safe'],
    ['llm', 'LLM prompt', true, 'external'],
    ['x', 'process-teleport', false, '']
  ])
  expect(view.nodes[1].icon).toBe('Sparkles')
  expect(view.nodes[2].icon).toBe('HelpCircle')
  expect(view.unknownTypes).toEqual(['process-teleport'])
  expect(view.connections).toBe(1)
  expect(view.skippedConnections).toBe(1)
  // The highest risk of what will be placed (the unknown node is left out).
  expect(view.risk).toBe('external')
})

test('the highest risk counts privileged above external, and notes a node whose settings can raise it', () => {
  const f = proposal()
  f.nodes.push(stored('agent', 'process-llm-agent', 'processing'), stored('cmd', 'action-command', 'output'))
  const view = proposalView(f, getType)
  expect(view.risk).toBe('privileged')
  expect(view.nodes.find(n => n.id === 'agent')?.riskCanRise).toBe(true)
  expect(proposalView({ ...f, nodes: [] }, getType).risk).toBe('')
})

test('a change of the current flow is summarised by the IDs of the nodes kept, added and removed', () => {
  const placed = proposalView(proposal(), getType).nodes.filter(n => n.known)
  expect(changeSummary([{ id: 't' }, { id: 'old' }], placed)).toEqual({ kept: 1, added: 1, removed: 1 })
})

test('replacing keeps the proposal’s IDs and positions, and leaves out unknown types with their connections', () => {
  const f = proposal()
  const prepared = prepareProposal(f, [], getType, 'replace')
  expect(prepared.nodes.map(n => n.id)).toEqual(['t', 'llm'])
  expect(prepared.nodes[1].position).toEqual({ x: 270, y: 30 })
  expect(prepared.connections).toEqual([conn('c1', 't', 'llm')])
  expect(prepared.skippedTypes).toEqual(['process-teleport'])
  expect(prepared.skippedConnections).toBe(1)
  // The settings are copies: applying twice never shares an object.
  ;(prepared.nodes[1].config.schema as Record<string, unknown>).type = 'array'
  expect((f.nodes[1].config.schema as Record<string, unknown>).type).toBe('object')
})

test('a node without a category takes its type’s; a missing position is the origin', () => {
  const f = proposal()
  f.nodes[0].category = ''
  ;(f.nodes[1] as Partial<StoredFlowNode>).position = undefined
  const prepared = prepareProposal(f, [], getType, 'replace')
  expect(prepared.nodes[0].category).toBe('input')
  expect(prepared.nodes[1].position).toEqual({ x: 0, y: 0 })
})

// The model may repeat or omit an ID: the canvas needs unique ones. The
// first node keeps a repeated ID (connections name it); the others get new
// ones, and so do repeated or empty connection IDs.
test('repeated or empty IDs get new ones; connections to missing nodes are left out', () => {
  const f = proposal()
  f.nodes = [stored('t', 'trigger-manual', 'input'), stored('t', 'process-llm', 'processing'), stored('', 'process-llm', 'processing')]
  f.connections = [conn('c', 't', 't'), conn('c', 't', 'gone'), conn('', 't', 't')]
  let n = 0
  const prepared = prepareProposal(f, [], getType, 'replace', prefix => `${prefix}_${++n}`)
  const ids = prepared.nodes.map(node => node.id)
  expect(ids[0]).toBe('t')
  expect(new Set(ids).size).toBe(3)
  expect(ids.every(Boolean)).toBe(true)
  expect(prepared.connections.map(c => c.targetNode)).toEqual(['t', 't'])
  expect(new Set(prepared.connections.map(c => c.id)).size).toBe(2)
  expect(prepared.connections.every(c => c.id)).toBe(true)
  expect(prepared.skippedConnections).toBe(1)
})

test('adding gives every node and connection a new ID and rewrites the connections’ endpoints', () => {
  // The generator's first ID is already on the canvas: it is not reused.
  const existing = [canvasNode('t', 'trigger-manual', { x: 0, y: 100 }), canvasNode('node_1', 'process-llm', { x: 600, y: 160 })]
  const taken = ['t', 'node_1']
  let n = 0
  const prepared = prepareProposal(proposal(), existing, getType, 'add', prefix => `${prefix}_${++n}`)
  const ids = prepared.nodes.map(node => node.id)
  expect(ids.some(id => taken.includes(id))).toBe(false)
  expect(new Set(ids).size).toBe(2)
  const [trigger, llm] = prepared.nodes
  expect(prepared.connections).toHaveLength(1)
  expect(prepared.connections[0].sourceNode).toBe(trigger.id)
  expect(prepared.connections[0].targetNode).toBe(llm.id)
  expect(prepared.connections[0].id).not.toBe('c1')
  expect(prepared.skippedTypes).toEqual(['process-teleport'])
})

test('added nodes go to the right of the current ones, top-aligned, keeping their own layout', () => {
  const existing = [canvasNode('a', 'trigger-manual', { x: -40, y: 100 }), canvasNode('b', 'process-llm', { x: 600, y: 160 })]
  const prepared = prepareProposal(proposal(), existing, getType, 'add')
  const [trigger, llm] = prepared.nodes
  expect(trigger.position.x).toBeGreaterThan(600)
  expect(trigger.position.y).toBe(100)
  expect(llm.position.x - trigger.position.x).toBe(270)
  expect(llm.position.y - trigger.position.y).toBe(30)
})

test('an issue is located by the node’s name and ID and the setting, or by the connection’s ends', () => {
  const f = proposal()
  f.connections.push(conn('a/b', 't', 'llm'))
  const at = (path: string) => issueLocation({ path, code: 'x', severity: 'error', message: '' }, f, getType)
  expect(at('/nodes/llm/config/prompt')).toBe('LLM prompt (llm) · prompt')
  expect(at('/nodes/x')).toBe('process-teleport (x)')
  expect(at('/connections/c1')).toBe('Manual trigger → LLM prompt')
  expect(at('/connections/a~1b')).toBe('Manual trigger → LLM prompt')
  expect(at('/nodes/missing')).toBe('/nodes/missing')
  expect(at('')).toBe('Flow')
  expect(at('/name')).toBe('/name')
})

test('errors: a cancelled request says so, otherwise the server’s message or why it failed', () => {
  expect(assistantError(new Error('canceled'), true)).toBe('Cancelled. Nothing was changed.')
  expect(assistantError({ response: { status: 409, data: { error: 'the flow assistant is busy' } } }, false)).toBe('the flow assistant is busy')
  expect(assistantError({ response: { status: 502, data: {} } }, false)).toBe('The flow assistant could not propose a flow. Try again.')
  expect(assistantError(new Error('Network Error'), false)).toBe('Could not reach PiWorker. Check your connection and try again.')
})
