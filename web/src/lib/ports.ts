import type { FlowNode, FlowEdge, NodeTypeConfig } from '@/types'
export interface Port { id: string; name: string; dataType?: string; multiple?: boolean; required?: boolean }
export function ports(value: unknown[] = []): Port[] {
  return value.map(p => typeof p === 'string' ? { id: p, name: p, dataType: 'any' } : p as Port)
}
/**
 * The output every node that processes messages has besides the ones its
 * type declares (ADR-0036): when the node fails, the failure leaves through
 * it as a message. A trigger never fails on a message, so it has none.
 */
export const ERROR_PORT = 'error'
export function hasErrorPort(type: Pick<NodeTypeConfig, 'category'> | undefined): boolean {
  return !!type && type.category !== 'trigger'
}
/** The ports a connection can start from: the declared outputs, then the error port. */
export function sourcePorts(type: NodeTypeConfig | undefined): Port[] {
  if (!type) return []
  const declared = ports(type.outputs)
  return hasErrorPort(type) ? [...declared, { id: ERROR_PORT, name: ERROR_PORT, dataType: 'object' }] : declared
}
export function validConnection(connection: Pick<FlowEdge, 'source' | 'target' | 'sourceHandle' | 'targetHandle'> & { id?: string }, nodes: FlowNode[], edges: FlowEdge[], getType: (id: string) => NodeTypeConfig | undefined): boolean {
  const { source, target, sourceHandle, targetHandle } = connection
  if (!source || !target || source === target) return false
  const from = nodes.find(n => n.id === source), to = nodes.find(n => n.id === target)
  if (!from || !to) return false
  const output = sourcePorts(getType(from.data.nodeType)).find(p => p.id === sourceHandle)
  const input = ports(getType(to.data.nodeType)?.inputs).find(p => p.id === targetHandle)
  if (!output || !input) return false
  // An edge never duplicates itself: Vue Flow re-validates existing edges.
  return !edges.some(e => (connection.id === undefined || e.id !== connection.id) && e.source === source && e.target === target && e.sourceHandle === sourceHandle && e.targetHandle === targetHandle)
}
