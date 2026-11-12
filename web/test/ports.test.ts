import { expect, test } from 'bun:test'
import { validConnection } from '../src/lib/ports'
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
