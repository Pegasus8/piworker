// The flow assistant as the editor handles it (docs/design/llm.md §7–§8,
// ADR-0041): the definition a "change the current flow" request sends, what
// the dialog shows of a proposal, and the proposal made ready for the canvas
// (unknown node types left out, unique IDs, placed beside the nodes already
// there). Pure helpers: the request is useApi().assistFlow, placing the
// proposal is the flows store's applyProposal (one undo step).
import { msg } from '@/i18n'
import { errorMessage } from '@/lib/llm'
import { riskInfo, type FlowIssue } from '@/lib/drafts'
import type { AssistantFlow, Flow, FlowEdge, FlowNode, NodeCategory, NodeTypeConfig, StoredFlowConnection, StoredFlowNode } from '@/types'

type GetType = (id: string) => NodeTypeConfig | undefined

/** The longest description the server takes (service.MaxAssistantDescriptionChars), in characters. */
export const MAX_DESCRIPTION_CHARS = 4000

/** The description's length as the server counts it: characters (not UTF-16 units) once trimmed. */
export function descriptionLength(text: string): number {
  return [...text.trim()].length
}

// The editor's node categories and the stored ones (flow.Node.Category).
const STORED_CATEGORY: Record<NodeCategory, string> = { trigger: 'input', process: 'processing', action: 'output' }

/**
 * How a proposal reaches the canvas: it replaces the nodes of an empty
 * canvas, and of the current flow when the proposal is a change of it (it
 * is then the whole changed flow, with the IDs of the nodes it kept);
 * otherwise it is added beside them with new IDs.
 */
export type ApplyMode = 'replace' | 'add'
export function applyMode(canvasNodes: number, changesCurrent: boolean): ApplyMode {
  return canvasNodes === 0 || changesCurrent ? 'replace' : 'add'
}

/**
 * The canvas in the stored shape, unsaved changes included, for "change the
 * current flow". The settings are copies; credentials go along (the server
 * withholds them from the model and puts them back on the nodes it keeps).
 * Ports are left empty: the assistant reads only IDs, types, positions,
 * settings and whether a node is enabled.
 */
export function currentDefinition(flow: Pick<Flow, 'name' | 'description'> | null, nodes: readonly FlowNode[], edges: readonly FlowEdge[]): AssistantFlow {
  return {
    name: flow?.name ?? '',
    description: flow?.description ?? '',
    nodes: nodes.map(n => ({
      id: n.id,
      type: n.data.nodeType,
      category: STORED_CATEGORY[n.data.category] ?? n.data.category,
      position: { x: n.position.x, y: n.position.y },
      config: clone(n.data.config ?? {}),
      enabled: n.data.enabled ?? true,
      inputs: [],
      outputs: []
    })),
    connections: edges.map(e => ({
      id: e.id,
      sourceNode: e.source,
      sourcePort: e.sourceHandle ?? 'output',
      targetNode: e.target,
      targetPort: e.targetHandle ?? 'input'
    }))
  }
}

/**
 * What a "change the current flow" proposal was built from: the canvas's
 * nodes (types, positions, settings, enabled) and connections, as the
 * request sent them. When the canvas no longer matches, applying the
 * proposal would silently undo the edits made since (AssistantDialog
 * refuses it and offers to propose again). The flow's name and description
 * are left out: placing a proposal never renames a saved flow.
 */
export function canvasFingerprint(nodes: readonly FlowNode[], edges: readonly FlowEdge[]): string {
  const { nodes: sentNodes, connections } = currentDefinition(null, nodes, edges)
  return JSON.stringify({ nodes: sentNodes, connections })
}

/** A node of the proposal as the dialog lists it. */
export interface ProposalNode {
  id: string
  type: string
  /** The type's name, or the type ID when this editor does not know it. */
  label: string
  /** A lucide component name (lib/nodeIcons.ts). */
  icon: string
  /** The type's risk ('unknown' when the type has none, read as the highest; '' for an unknown type: it is left out). */
  risk: string
  /** A setting can raise this node to privileged (the type's riskCanRise). */
  riskCanRise: boolean
  /** Whether this editor knows the type; an unknown one is left out. */
  known: boolean
  enabled: boolean
}

export interface ProposalView {
  nodes: ProposalNode[]
  /** The types this editor does not know, once each: their nodes are left out. */
  unknownTypes: string[]
  /** The connections that will be placed (both ends are nodes of known types). */
  connections: number
  /** The connections left out with an unknown node (or naming no node). */
  skippedConnections: number
  /** The highest risk of the nodes that will be placed ('' when none). */
  risk: string
}

export function proposalView(flow: AssistantFlow, getType: GetType): ProposalView {
  const nodes = flow.nodes.map((n): ProposalNode => {
    const type = getType(n.type)
    return {
      id: n.id,
      type: n.type,
      label: type?.name || n.type,
      icon: type?.icon || 'HelpCircle',
      risk: type ? type.risk || 'unknown' : '',
      riskCanRise: type?.riskCanRise === true,
      known: !!type,
      enabled: n.enabled !== false
    }
  })
  const known = new Set(nodes.filter(n => n.known).map(n => n.id))
  const placed = flow.connections.filter(c => known.has(c.sourceNode) && known.has(c.targetNode)).length
  // An unknown level reads as the highest (riskInfo, lib/nodeRisk.ts).
  let risk = ''
  for (const n of nodes) {
    if (n.known && (risk === '' || riskInfo(n.risk).rank > riskInfo(risk).rank)) risk = n.risk
  }
  return {
    nodes,
    unknownTypes: [...new Set(nodes.filter(n => !n.known).map(n => n.type))],
    connections: placed,
    skippedConnections: flow.connections.length - placed,
    risk
  }
}

/** What a change of the current flow does to the canvas, by node ID (placed: the proposal's nodes that will be placed). */
export interface ChangeSummary { kept: number; added: number; removed: number }
export function changeSummary(current: readonly { id: string }[], placed: readonly { id: string }[]): ChangeSummary {
  const before = new Set(current.map(n => n.id))
  const after = new Set(placed.map(n => n.id))
  let kept = 0
  for (const id of after) if (before.has(id)) kept++
  return { kept, added: after.size - kept, removed: before.size - kept }
}

/** The proposal ready for the canvas, still in the stored shape (the store converts it as it loads a flow). */
export interface PreparedProposal {
  nodes: StoredFlowNode[]
  connections: StoredFlowConnection[]
  /** The unknown types whose nodes were left out, once each. */
  skippedTypes: string[]
  /** The connections left out with them (or naming no node of the proposal). */
  skippedConnections: number
}

// How far right of the rightmost node an added proposal starts: a node's
// width and a column's gap (lib/autoConnect.ts steps 270 per column).
const ADD_GAP_X = 360

let idCounter = 0
function defaultId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}_${++idCounter}`
}

/**
 * Makes a proposal ready for the canvas: nodes of types this editor does
 * not know are left out with the connections touching them; every setting
 * is a copy. Replacing keeps the proposal's IDs and positions, except a
 * repeated or empty ID, which gets a new one (the first node keeps a
 * repeated ID: the connections name it). Adding gives every node and
 * connection a new ID, rewrites the connections' ends and moves the whole
 * proposal to the right of the nodes on the canvas, top-aligned with them,
 * keeping its own layout. newId makes the IDs (tests pass their own); one
 * already in use is never returned.
 */
export function prepareProposal(
  flow: AssistantFlow, existing: readonly FlowNode[], getType: GetType, mode: ApplyMode,
  newId: (prefix: string) => string = defaultId
): PreparedProposal {
  // In use: the canvas's IDs when adding; the proposal's own when replacing
  // (a new ID never takes one a later node or connection keeps).
  const taken = new Set<string>(mode === 'add' ? existing.map(n => n.id) : [])
  const reserved = new Set<string>(mode === 'replace' ? [...flow.nodes.map(n => n.id), ...flow.connections.map(c => c.id)].filter(Boolean) : [])
  const fresh = (prefix: string) => {
    let id = newId(prefix)
    while (!id || taken.has(id) || reserved.has(id)) id = defaultId(prefix)
    taken.add(id)
    return id
  }

  // The proposal's node IDs to the canvas's (the first node of a repeated ID wins).
  const ids = new Map<string, string>()
  const skippedTypes: string[] = []
  const nodes: StoredFlowNode[] = []
  for (const n of flow.nodes) {
    const type = getType(n.type)
    if (!type) {
      if (!skippedTypes.includes(n.type)) skippedTypes.push(n.type)
      continue
    }
    const keep = mode === 'replace' && !!n.id && !taken.has(n.id)
    const id = keep ? n.id : fresh('node')
    taken.add(id)
    if (n.id && !ids.has(n.id)) ids.set(n.id, id)
    nodes.push({
      id,
      type: n.type,
      category: n.category || STORED_CATEGORY[type.category] || type.category,
      position: n.position ? { x: n.position.x, y: n.position.y } : { x: 0, y: 0 },
      config: clone(n.config ?? {}),
      enabled: n.enabled !== false,
      inputs: clone(n.inputs ?? []),
      outputs: clone(n.outputs ?? [])
    })
  }

  const edgeIds = new Set<string>()
  const connections: StoredFlowConnection[] = []
  for (const c of flow.connections) {
    const source = ids.get(c.sourceNode), target = ids.get(c.targetNode)
    if (!source || !target) continue
    const id = mode === 'replace' && c.id && !edgeIds.has(c.id) ? c.id : fresh('edge')
    edgeIds.add(id)
    connections.push({ id, sourceNode: source, sourcePort: c.sourcePort, targetNode: target, targetPort: c.targetPort })
  }

  if (mode === 'add' && existing.length && nodes.length) {
    const right = Math.max(...existing.map(n => n.position.x))
    const top = Math.min(...existing.map(n => n.position.y))
    const left = Math.min(...nodes.map(n => n.position.x))
    const proposalTop = Math.min(...nodes.map(n => n.position.y))
    const dx = right + ADD_GAP_X - left, dy = top - proposalTop
    for (const n of nodes) n.position = { x: n.position.x + dx, y: n.position.y + dy }
  }

  return { nodes, connections, skippedTypes, skippedConnections: flow.connections.length - connections.length }
}

// A JSON pointer reference token (RFC 6901), as flow.Lint escapes it.
const unescapePointer = (token: string) => token.replace(/~1/g, '/').replace(/~0/g, '~')

/**
 * Where a Lint issue of the proposal is: the node's name and ID (and the
 * setting), the ends of the connection, or the whole flow; the path itself
 * when it names nothing of the proposal.
 */
export function issueLocation(issue: FlowIssue, flow: AssistantFlow, getType: GetType): string {
  const path = issue.path ?? ''
  if (path === '' || path === '/') return msg().assistant.wholeFlow
  const name = (id: string) => {
    const n = flow.nodes.find(node => node.id === id)
    return n ? getType(n.type)?.name || n.type : id
  }
  const node = /^\/nodes\/([^/]+)(?:\/config\/(.+))?$/.exec(path)
  if (node) {
    const id = unescapePointer(node[1])
    if (!flow.nodes.some(n => n.id === id)) return path
    const where = `${name(id)} (${id})`
    return node[2] ? `${where} · ${unescapePointer(node[2])}` : where
  }
  const connection = /^\/connections\/([^/]+)$/.exec(path)
  if (connection) {
    const c = flow.connections.find(c => c.id === unescapePointer(connection[1]))
    if (c) return `${name(c.sourceNode)} → ${name(c.targetNode)}`
  }
  return path
}

/**
 * Why a request failed: cancelled by the admin, the server's message (a
 * busy assistant, a spent daily limit, the provider's error, the time
 * bound), or PiWorker could not be reached.
 */
export function assistantError(e: unknown, aborted: boolean): string {
  const t = msg().assistant
  if (aborted) return t.cancelled
  const answered = !!(e as { response?: unknown })?.response
  return errorMessage(e, answered ? t.failed : t.unreachable)
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}
