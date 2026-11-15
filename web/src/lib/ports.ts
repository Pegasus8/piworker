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

type PortsOf = Pick<NodeTypeConfig, 'outputs' | 'outputsFrom'>

/**
 * The output ports a node's settings give it (ADR-0040): one per item of
 * the setting its type names in outputsFrom (the classifier's categories),
 * the same ones the server's node.ConfigOutputs gives: string items as they
 * are, without empty or repeated ones, and none that would shadow a
 * declared output or the error port.
 */
function settingPorts(type: PortsOf, declared: Port[], config: Record<string, unknown> | undefined): Port[] {
  const items = type.outputsFrom ? config?.[type.outputsFrom] : undefined
  if (!Array.isArray(items)) return []
  const seen = new Set([ERROR_PORT, ...declared.map(p => p.id)])
  const out: Port[] = []
  for (const item of items) {
    if (typeof item !== 'string' || item === '' || seen.has(item)) continue
    seen.add(item)
    out.push({ id: item, name: item, dataType: 'any' })
  }
  return out
}

/**
 * A node's outputs: the ones its type declares, then the ones its settings
 * give it. Every place that reads a node's outputs goes through this (or
 * sourcePorts), so the canvas, the connections dialog and the checks agree.
 */
export function outputPorts(type: PortsOf | undefined, config?: Record<string, unknown>): Port[] {
  if (!type) return []
  const declared = ports(type.outputs)
  return [...declared, ...settingPorts(type, declared, config)]
}

/** The ports a connection can start from: the node's outputs, then the error port. */
export function sourcePorts(type: NodeTypeConfig | undefined, config?: Record<string, unknown>): Port[] {
  if (!type) return []
  const outputs = outputPorts(type, config)
  return hasErrorPort(type) ? [...outputs, { id: ERROR_PORT, name: ERROR_PORT, dataType: 'object' }] : outputs
}

export function validConnection(connection: Pick<FlowEdge, 'source' | 'target' | 'sourceHandle' | 'targetHandle'> & { id?: string }, nodes: FlowNode[], edges: FlowEdge[], getType: (id: string) => NodeTypeConfig | undefined): boolean {
  const { source, target, sourceHandle, targetHandle } = connection
  if (!source || !target || source === target) return false
  const from = nodes.find(n => n.id === source), to = nodes.find(n => n.id === target)
  if (!from || !to) return false
  const output = sourcePorts(getType(from.data.nodeType), from.data.config).find(p => p.id === sourceHandle)
  const input = ports(getType(to.data.nodeType)?.inputs).find(p => p.id === targetHandle)
  if (!output || !input) return false
  // An edge never duplicates itself: Vue Flow re-validates existing edges.
  return !edges.some(e => (connection.id === undefined || e.id !== connection.id) && e.source === source && e.target === target && e.sourceHandle === sourceHandle && e.targetHandle === targetHandle)
}

/**
 * The edges after a node's settings change from before to after: the edges
 * leaving a port the node no longer has are removed, and when one item was
 * renamed in place (typed over in its row) the edges leaving it follow the
 * new name, so fixing a category's spelling keeps its connections. Returns
 * edges itself when nothing changes.
 */
export function edgesAfterConfig<E extends Pick<FlowEdge, 'source' | 'sourceHandle'>>(nodeId: string, type: NodeTypeConfig | undefined, before: Record<string, unknown>, after: Record<string, unknown>, edges: E[]): E[] {
  if (!type?.outputsFrom) return edges
  const now = new Set(sourcePorts(type, after).map(p => p.id))
  const renamed = renamedPort(type, before, after, now)
  let changed = false
  const out: E[] = []
  for (const edge of edges) {
    const port = edge.sourceHandle ?? ''
    if (edge.source !== nodeId || now.has(port)) out.push(edge)
    else if (renamed && port === renamed.from) {
      out.push({ ...edge, sourceHandle: renamed.to })
      changed = true
    } else changed = true
  }
  return changed ? out : edges
}

// The one item that changed in place: same number of items, exactly one
// position differs, its old port is gone and its new name is a port now.
function renamedPort(type: NodeTypeConfig, before: Record<string, unknown>, after: Record<string, unknown>, now: Set<string>): { from: string; to: string } | null {
  const a = before[type.outputsFrom!], b = after[type.outputsFrom!]
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return null
  const changed = a.flatMap((item, i) => item !== b[i] ? [i] : [])
  if (changed.length !== 1) return null
  const from = a[changed[0]], to = b[changed[0]]
  if (typeof from !== 'string' || typeof to !== 'string' || now.has(from) || !now.has(to)) return null
  // A name another item already had is not this item's port.
  if (a.includes(to)) return null
  return { from, to }
}
