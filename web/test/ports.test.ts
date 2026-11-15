import { expect, test } from 'bun:test'
import { ERROR_PORT, edgesAfterConfig, outputPorts, sourcePorts, validConnection } from '../src/lib/ports'
import type { FlowNode, NodeTypeConfig } from '../src/types'

const node = (id: string, nodeType: string) => ({ id, type: 'process', position: { x: 0, y: 0 }, data: { nodeType, config: {} } }) as unknown as FlowNode
const types: Record<string, Partial<NodeTypeConfig>> = {
  timer: { outputs: ['output'] as any, inputs: [] as any },
  log: { inputs: [{ id: 'input', name: 'Input', multiple: false }] as any, outputs: [] as any }
}
const getType = (id: string) => types[id] as NodeTypeConfig | undefined
const nodes = [node('a', 'timer'), node('b', 'log')]
const edge = { id: 'e1', source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }

test('a new connection that duplicates an existing one is refused', () => {
  expect(validConnection({ source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }, nodes, [edge], getType)).toBe(false)
})

// Vue Flow re-validates the edges it is given with isValidConnection: an
// existing edge must not count as its own duplicate, or the canvas drops
// every connection (they were saved and ran, but were never drawn).
test('an existing edge is valid against the list that contains it', () => {
  expect(validConnection(edge, nodes, [edge], getType)).toBe(true)
})

// The error port (ADR-0036): every node that processes messages has it
// besides its declared outputs; a trigger has none.
test('the error port is a valid source on processing and action nodes only', () => {
  const kinds: Record<string, Partial<NodeTypeConfig>> = {
    timer: { category: 'trigger', outputs: ['output'] as any, inputs: [] as any },
    http: { category: 'action', inputs: ['input'] as any, outputs: ['output'] as any },
    log: { category: 'action', inputs: [{ id: 'input', name: 'Input' }] as any, outputs: [] as any }
  }
  const get = (id: string) => kinds[id] as NodeTypeConfig | undefined
  const all = [node('t', 'timer'), node('h', 'http'), node('l', 'log')]
  expect(validConnection({ source: 'h', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 't', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(false)
  expect(sourcePorts(get('http')).map(p => p.id)).toEqual(['output', ERROR_PORT])
  expect(sourcePorts(get('timer')).map(p => p.id)).toEqual(['output'])
  expect(sourcePorts(undefined)).toEqual([])
})

// Outputs from a setting (ADR-0040): the classifier's categories become
// ports after its static outputs, and the error port stays last.
const classify = { id: 'process-llm-classify', category: 'process', inputs: [{ id: 'input', name: 'Input' }], outputs: [{ id: 'other', name: 'Other' }], outputsFrom: 'categories' } as unknown as NodeTypeConfig

test('a setting adds one output port per item after the static outputs', () => {
  expect(outputPorts(classify, { categories: ['bug', 'question'] }).map(p => p.id)).toEqual(['other', 'bug', 'question'])
  expect(outputPorts(classify, { categories: ['bug'] }).map(p => p.name)).toEqual(['Other', 'bug'])
  expect(sourcePorts(classify, { categories: ['bug'] }).map(p => p.id)).toEqual(['other', 'bug', ERROR_PORT])
  // No config, an unset or unreadable setting: the static outputs only.
  expect(outputPorts(classify).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(classify, {}).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(classify, { categories: 'bug, question' }).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(undefined, { categories: ['bug'] })).toEqual([])
})

// The same ports as the server's node.ConfigOutputs: empty, repeated and
// non-string items give none, and no item shadows a static port or the
// error port.
test('setting ports skip empty, repeated and reserved items', () => {
  const ids = outputPorts(classify, { categories: ['bug', '', 'bug', 3, 'other', ERROR_PORT, 'praise'] }).map(p => p.id)
  expect(ids).toEqual(['other', 'bug', 'praise'])
  expect(sourcePorts(classify, { categories: [ERROR_PORT] }).map(p => p.id)).toEqual(['other', ERROR_PORT])
})

test('a type without outputsFrom ignores the settings', () => {
  const http = { category: 'action', inputs: ['input'], outputs: ['output'] } as unknown as NodeTypeConfig
  expect(outputPorts(http, { categories: ['bug'] }).map(p => p.id)).toEqual(['output'])
})

test('a connection may leave a port the node settings give it', () => {
  const all = [{ ...node('c', 'classify'), data: { nodeType: 'classify', config: { categories: ['bug'] } } } as unknown as FlowNode, node('l', 'log')]
  const get = (id: string) => (id === 'classify' ? classify : types[id]) as NodeTypeConfig | undefined
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'bug', targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'other', targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'praise', targetHandle: 'input' }, all, [], get)).toBe(false)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(true)
})

// Editing the setting: the edges leaving a port that is gone are removed;
// an item renamed in place (typing in its row) keeps its edges.
test('edges follow the ports when the setting changes', () => {
  const edges = [
    { id: 'e1', source: 'c', target: 'l', sourceHandle: 'bug', targetHandle: 'input' },
    { id: 'e2', source: 'c', target: 'l', sourceHandle: 'question', targetHandle: 'input' },
    { id: 'e3', source: 'c', target: 'l', sourceHandle: 'other', targetHandle: 'input' },
    { id: 'e4', source: 'c', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' },
    { id: 'e5', source: 'x', target: 'c', sourceHandle: 'bug', targetHandle: 'input' }
  ]
  const before = { categories: ['bug', 'question'] }
  // Removed: its edge goes, the others stay (also edges of other nodes).
  expect(edgesAfterConfig('c', classify, before, { categories: ['question'] }, edges).map(e => e.id)).toEqual(['e2', 'e3', 'e4', 'e5'])
  // Renamed in place: the edge follows the new name.
  const renamed = edgesAfterConfig('c', classify, before, { categories: ['bugs', 'question'] }, edges)
  expect(renamed.find(e => e.id === 'e1')?.sourceHandle).toBe('bugs')
  expect(renamed).toHaveLength(5)
  // Reordered: every port is still there.
  expect(edgesAfterConfig('c', classify, before, { categories: ['question', 'bug'] }, edges)).toBe(edges)
  // Unchanged ports: the same array (nothing to record or sync).
  expect(edgesAfterConfig('c', classify, before, { ...before, instructions: 'x' }, edges)).toBe(edges)
  // A renamed item that now repeats another one is a removal.
  expect(edgesAfterConfig('c', classify, before, { categories: ['question', 'question'] }, edges).map(e => e.id)).toEqual(['e2', 'e3', 'e4', 'e5'])
  // Types without outputsFrom keep their edges.
  expect(edgesAfterConfig('c', types.log as NodeTypeConfig, before, {}, edges)).toBe(edges)
})
