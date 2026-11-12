import type { FlowEdge, FlowNode, NodeTypeConfig } from '@/types'
import { ports, validConnection } from '@/lib/ports'

// Where a node added from the palette goes, and the connection it gets: a
// node added while another is selected follows it (to its right, below
// anything already there) and is connected from its first output.
const STEP_X = 270
const STEP_Y = 135
const NEAR = 100

function occupied(position: { x: number; y: number }, nodes: FlowNode[]) {
  return nodes.some(n => Math.abs(n.position.x - position.x) < NEAR && Math.abs(n.position.y - position.y) < NEAR)
}

export function nextPosition(after: FlowNode | undefined, nodes: FlowNode[]): { x: number; y: number } {
  if (after) {
    const position = { x: after.position.x + STEP_X, y: after.position.y }
    while (occupied(position, nodes)) position.y += STEP_Y
    return position
  }
  const right = nodes.reduce((max, n) => Math.max(max, n.position.x), -STEP_X)
  const position = { x: nodes.length ? right + STEP_X : 0, y: 0 }
  while (occupied(position, nodes)) position.y += STEP_Y
  return position
}

export function autoEdge(source: FlowNode, target: FlowNode, nodes: FlowNode[], edges: FlowEdge[], getType: (id: string) => NodeTypeConfig | undefined): FlowEdge | null {
  const output = ports(getType(source.data.nodeType)?.outputs)[0]
  const input = ports(getType(target.data.nodeType)?.inputs)[0]
  if (!output || !input) return null
  const edge = { id: `edge_${source.id}_${target.id}_${Date.now()}`, source: source.id, target: target.id, sourceHandle: output.id, targetHandle: input.id, animated: true }
  return validConnection(edge, nodes, edges, getType) ? edge : null
}
