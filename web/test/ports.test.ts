import { expect, test } from 'bun:test'
import { ERROR_PORT, sourcePorts, validConnection } from '../src/lib/ports'
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
