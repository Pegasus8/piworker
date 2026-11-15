import type { FlowNode, FlowEdge, NodeTypeConfig } from '@/types'
import type { ItemRenames } from '@/lib/configValues'
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

/**
 * The least distance between the centres of two handles on a node's side,
 * in px: handles are 12px wide, so neighbours stay apart and each can be
 * picked by pointer.
 */
export const HANDLE_SPACING = 20

/**
 * The least height of a node, in px, for its handles: they are spread
 * evenly over a side (at (i+1)/(n+1) of its height), so a node with many
 * ports (a classifier with up to 20 categories) grows to keep them
 * HANDLE_SPACING apart. Below a node's natural height it changes nothing.
 */
export function nodeMinHeight(inputs: number, outputs: number): number {
  return (Math.max(inputs, outputs) + 1) * HANDLE_SPACING
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
export function edgesAfterConfig<E extends EdgeLike>(nodeId: string, type: NodeTypeConfig | undefined, before: Record<string, unknown>, after: Record<string, unknown>, edges: E[]): E[] {
  return followPorts(nodeId, type, before, after, edges).edges
}

type EdgeLike = Pick<FlowEdge, 'source' | 'sourceHandle' | 'target' | 'targetHandle'>

/**
 * edgesAfterConfig as the editor runs it while a list setting is typed
 * (ADR-0040): renames are what ListInput reported about the items (lib/
 * configValues.ts followRows), oldest first, and parked are the edges
 * kept aside for this node. The edges leaving a port that is gone are
 * parked instead of dropped: its row may be blank or repeat another for a
 * moment. An edge follows its item's new name; it comes back from aside
 * when its row names an item again; it goes for good when its row is
 * removed or another item takes its name. Only items are renamed: an edge
 * never moves onto, or off, a declared output or the error port. Returns
 * edges itself when they do not change.
 */
export function followPorts<E extends EdgeLike>(
  nodeId: string, type: NodeTypeConfig | undefined, before: Record<string, unknown>, after: Record<string, unknown>,
  edges: E[], parked: readonly E[] = [], renames: readonly ItemRenames[] = []
): { edges: E[]; parked: E[] } {
  if (!type?.outputsFrom) return { edges, parked: [...parked] }
  const declared = ports(type.outputs)
  const now = new Set(sourcePorts(type, after).map(p => p.id))
  const itemsBefore = new Set(settingPorts(type, declared, before).map(p => p.id))
  const itemsAfter = new Set(settingPorts(type, declared, after).map(p => p.id))
  // Where the rows say an item went (undefined: they said nothing of it).
  const follow = (port: string): string | null | undefined => {
    let at: string | null | undefined
    for (const batch of renames) {
      const to = batch.get(at ?? port)
      if (to === undefined) continue
      if (to === null) return null
      at = to
    }
    return at
  }
  const renamed = renamedPort(type, before, after, now, itemsAfter)
  const same = (a: E, b: E) => a.source === b.source && a.target === b.target && a.sourceHandle === b.sourceHandle && a.targetHandle === b.targetHandle
  let changed = false
  const out: E[] = []
  const aside: E[] = []
  // An edge on its item's new name, or aside while that name is no port.
  const place = (edge: E, to: string) => {
    const moved = to === edge.sourceHandle ? edge : { ...edge, sourceHandle: to }
    if (!itemsAfter.has(to)) aside.push(moved)
    else if (!out.some(e => same(e, moved))) out.push(moved)
  }
  for (const edge of edges) {
    const port = edge.sourceHandle ?? ''
    if (edge.source !== nodeId) {
      out.push(edge)
      continue
    }
    const to = itemsBefore.has(port) ? follow(port) : undefined
    if (to === null) {
      changed = true
    } else if (to !== undefined && to !== port) {
      place(edge, to)
      changed = true
    } else if (now.has(port)) {
      out.push(edge)
    } else {
      changed = true
      if (renamed && port === renamed.from) place(edge, renamed.to)
      else aside.push(edge)
    }
  }
  for (const edge of parked) {
    const port = edge.sourceHandle ?? ''
    const to = follow(port)
    if (to === null) continue
    if (to !== undefined) {
      const before = out.length
      place(edge, to)
      if (out.length !== before) changed = true
    } else if (!itemsAfter.has(port) || itemsBefore.has(port)) {
      // Still no port of its own. A name that became an item again without
      // its row saying so is another item's: the edge goes.
      aside.push(edge)
    }
  }
  return { edges: changed ? out : edges, parked: aside }
}

// The one item that changed in place: same number of items, exactly one
// position differs, its old port is gone and its new name is an item now.
function renamedPort(type: NodeTypeConfig, before: Record<string, unknown>, after: Record<string, unknown>, now: Set<string>, items: Set<string>): { from: string; to: string } | null {
  const a = before[type.outputsFrom!], b = after[type.outputsFrom!]
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return null
  const changed = a.flatMap((item, i) => item !== b[i] ? [i] : [])
  if (changed.length !== 1) return null
  const from = a[changed[0]], to = b[changed[0]]
  if (typeof from !== 'string' || typeof to !== 'string' || now.has(from) || !items.has(to)) return null
  // A name another item already had is not this item's port.
  if (a.includes(to)) return null
  return { from, to }
}
