import type { Node, Edge } from '@vue-flow/core'
import type { SuggestSource } from '@/lib/zigbee'

export type NodeCategory = 'trigger' | 'process' | 'action'

export interface NodeTypeConfig {
  id: string
  name: string
  category: NodeCategory
  icon: string
  description: string
  documentation?: string
  /** What the type can affect outside the flow: safe, external or privileged. */
  risk?: string
  /**
   * A setting can raise this node to privileged (flow.Risk): its schema has
   * a destination (a URL or host taken from the message) or an opt-in that
   * widens access (riskSwitch).
   */
  escalates?: boolean
  inputs?: unknown[]
  outputs?: unknown[]
  configSchema: ConfigField[]
}

export interface ConfigField {
  key: string
  label: string
  type: 'text' | 'number' | 'integer' | 'select' | 'boolean' | 'textarea' | 'cron' | 'keyvalue' | 'json'
  required?: boolean
  default?: any
  options?: { label: string; value: string }[]
  placeholder?: string
  /** The unit a number is stored in (milliseconds, seconds, bytes…). */
  unit?: string
  /** A credential: never shown outside its field. */
  sensitive?: boolean
  /** Where a free-text setting finds suggestions (the value stays free text). */
  suggest?: SuggestSource
}

// The persisted model contains node identity, position and application data.
// Vue Flow's rendering attributes and component types belong to the canvas,
// not to the deeply reactive, serializable flow state.
export interface FlowNode extends Pick<Node, 'id' | 'type' | 'position'> {
  data: {
    label: string
    nodeType: string
    category: NodeCategory
    icon: string
    enabled?: boolean
    config: Record<string, any>
  }
}

export type FlowEdge = Pick<Edge, 'id' | 'source' | 'target' | 'sourceHandle' | 'targetHandle' | 'type' | 'animated'>

export interface Flow {
  id: string
  name: string
  description?: string
  state?: 'active' | 'inactive' | 'running' | 'failed'
  running?: boolean
  /** Server-controlled definition version; a save sends the one it loaded (stale → 409). */
  version?: number
  /** The last deployment (null: none recorded). */
  deployment?: DeploymentMark | null
  /** The current version was saved by an agent and not approved by the admin. */
  agentChanged?: boolean
  nodes: FlowNode[]
  edges: FlowEdge[]
  createdAt: string
  updatedAt: string
}

export interface FlowListItem {
  id: string
  name: string
  description?: string
  state?: 'active' | 'inactive' | 'running' | 'failed'
  running: boolean
  nodeCount: number
  /** The version the list shows; turning the flow on sends it (stale → 409). */
  version?: number
  deployment?: DeploymentMark | null
  agentChanged?: boolean
  createdAt: string
  updatedAt: string
}

// --- Versions and deployment marks (docs/design/mcp.md §10) ---

/** Who deployed a flow last, and which version (service.DeploymentView). */
export interface DeploymentMark {
  version: number
  actorKind: 'editor' | 'agent' | string
  /** The agent's token name (agents only). */
  tokenName?: string
  deployedAt: string
}

/** One stored version of a flow, newest first (GET /api/flows/{id}/versions). */
export interface VersionEntry {
  version: number
  actorKind: 'editor' | 'agent' | string
  tokenId?: string
  tokenName?: string
  /** Saved or approved by the admin (an agent's own version never is). */
  adminApproved: boolean
  note?: string
  draftId?: string
  createdAt: string
  /** The deployment marker names this version. */
  deployed: boolean
}

export interface VersionList {
  current: number
  /** The newest admin-approved version (0: none). */
  lastAdminApproved: number
  versions: VersionEntry[]
}

// --- Observability ---

export type NodePhase = 'running' | 'success' | 'error' | 'debug'

// FlowEvent is one live node-execution event delivered over the SSE stream.
export interface FlowEvent {
  flowId: string
  nodeId: string
  nodeType: string
  phase: NodePhase
  msgId?: string
  corrId?: string
  durMs?: number
  error?: string
  debug?: unknown
  ts?: number
}

// FlowRun is one persisted run (one trigger firing) of a flow.
export interface FlowRun {
  id: string
  flowId: string
  startedAt: string
  finishedAt?: string
  status: 'running' | 'success' | 'error'
  nodeCount: number
  errorCount: number
  /** Who started it: "editor", "token:<name>", or absent when the trigger fired (see lib/runActor). */
  actor?: string
}

// NodeEventRecord is one persisted node-execution event within a run.
export interface NodeEventRecord {
  id: number
  runId: string
  flowId: string
  nodeId: string
  nodeType: string
  phase: 'running' | 'success' | 'error'
  msgId?: string
  durationMs?: number
  error?: string
  createdAt: string
}

export interface NodeTestMessage {
  payload: unknown
  sourcePort?: string
  topic?: string
  meta?: Record<string, unknown>
  [key: string]: unknown
}
export interface NodeTestResult {
  outputs?: NodeTestMessage[] | null
  error?: string
  durMs?: number
}
/** The result of a tool call (flow.CallResult), e.g. the editor's "Test tool". */
export interface CallResult {
  status: 'completed' | 'completed_no_reply' | 'error' | 'running' | 'expired' | 'approval_required' | 'rejected'
  runId: string
  result?: unknown
  error?: { nodeId?: string; nodeType?: string; message: string }
  durationMs: number
}
export interface NodeTestDraft {
  mode: 'json' | 'text'
  payload: string
  topic: string
  metadata: string
}

/** GET /api/tls: how PiWorker serves HTTPS and what a device must trust. */
export interface TLSInfo {
  mode: 'auto' | 'custom' | 'off'
  caFingerprint: string
  sans: string[]
  notAfter: string | null
  caCertUrl: string
  /** Port HTTPS is served on; null when TLS is off or unknown. */
  httpsPort: number | null
  /** Plain HTTP port (redirect listener, or the only one with TLS off); null when none. */
  httpPort: number | null
  /** Set when the local CA is close to (or past) its expiry. */
  warning?: string
}
