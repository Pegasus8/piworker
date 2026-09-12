import type { FlowNode, FlowEdge, NodeTypeConfig } from '@/types'
export interface Port { id: string; name: string; dataType?: string; multiple?: boolean; required?: boolean }
export function ports(value: unknown[] = []): Port[] {
  return value.map(p => typeof p === 'string' ? { id: p, name: p, dataType: 'any' } : p as Port)
}
export function validConnection(connection: Pick<FlowEdge, 'source' | 'target' | 'sourceHandle' | 'targetHandle'>, nodes: FlowNode[], edges: FlowEdge[], getType: (id: string) => NodeTypeConfig | undefined): boolean {
  const { source, target, sourceHandle, targetHandle } = connection
  if (!source || !target || source === target) return false
  const from = nodes.find(n => n.id === source), to = nodes.find(n => n.id === target)
  if (!from || !to) return false
  const output = ports(getType(from.data.nodeType)?.outputs).find(p => p.id === sourceHandle)
  const input = ports(getType(to.data.nodeType)?.inputs).find(p => p.id === targetHandle)
  if (!output || !input) return false
  return !edges.some(e => e.source === source && e.target === target && e.sourceHandle === sourceHandle && e.targetHandle === targetHandle)
}
