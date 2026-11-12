import { expect, test } from 'bun:test'
import { nextPosition, autoEdge } from '../src/lib/autoConnect'
import type { FlowNode, NodeTypeConfig } from '../src/types'

const node = (id: string, nodeType: string, x = 0, y = 0) => ({ id, type: 'process', position: { x, y }, data: { nodeType, config: {} } }) as unknown as FlowNode
const types: Record<string, Partial<NodeTypeConfig>> = {
  timer: { outputs: ['output'], inputs: [] },
  switch: { inputs: ['input'], outputs: [{ id: 'true', name: 'true' }, { id: 'false', name: 'false' }] },
  log: { inputs: ['input'], outputs: [] }
}
const getType = (id: string) => types[id] as NodeTypeConfig | undefined

test('a node added after another goes to its right, below anything already there', () => {
  const a = node('a', 'timer', 0, 0)
  expect(nextPosition(a, [a])).toEqual({ x: 270, y: 0 })
  const b = node('b', 'log', 270, 0)
  expect(nextPosition(a, [a, b])).toEqual({ x: 270, y: 135 })
})
test('without a node to follow, nodes fill a grid', () => {
  expect(nextPosition(undefined, [])).toEqual({ x: 0, y: 0 })
  expect(nextPosition(undefined, [node('a', 'timer'), node('b', 'log', 270)])).toEqual({ x: 540, y: 0 })
})
test('the new node is connected from the first output to its first input', () => {
  const a = node('a', 'switch'), b = node('b', 'log')
  expect(autoEdge(a, b, [a, b], [], getType)).toMatchObject({ source: 'a', target: 'b', sourceHandle: 'true', targetHandle: 'input' })
})
test('nothing is connected when either side has no port', () => {
  const a = node('a', 'log'), b = node('b', 'log'), t = node('t', 'timer')
  expect(autoEdge(a, b, [a, b], [], getType)).toBeNull()
  expect(autoEdge(a, t, [a, t], [], getType)).toBeNull()
})
