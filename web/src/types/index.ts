import type { Node, Edge } from '@vue-flow/core'
import type { SuggestSource } from '@/lib/suggestions'
import type { FlowIssue } from '@/lib/drafts'

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
   * A setting can raise this node to privileged (the server's riskCanRise:
   * a URL or broker taken from the message, or an opt-in that widens access).
   */
  riskCanRise?: boolean
  inputs?: unknown[]
  outputs?: unknown[]
  /**
   * The list setting whose items are extra output ports after outputs
   * (ADR-0040, the classifier's categories); read the ports of a node with
   * outputPorts / sourcePorts (lib/ports.ts), never outputs alone.
   */
  outputsFrom?: string
  configSchema: ConfigField[]
}

export interface ConfigField {
  key: string
  label: string
  /** list: a list of strings (ConfigProperty type "array"), edited as rows. */
  type: 'text' | 'number' | 'integer' | 'select' | 'boolean' | 'textarea' | 'cron' | 'keyvalue' | 'json' | 'list'
  required?: boolean
  default?: any
  options?: { label: string; value: string }[]
  placeholder?: string
  /** The unit a number is stored in (milliseconds, seconds, bytes…). */
  unit?: string
  /** A credential: never shown outside its field. */
  sensitive?: boolean
  /** Where a free-text setting (or each item of a list) finds suggestions; the value stays free text. */
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
// The types below that mirror a server response are checked against the
// Go structs they come from (wire.gen.ts, wire.check.ts).

export type NodePhase = 'running' | 'success' | 'error' | 'debug'

// FlowEvent is one live node-execution event delivered over the SSE stream.
export interface FlowEvent {
  flowId: string
  nodeId: string
  /** The server omits it when empty. */
  nodeType?: string
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
  actor?: string  /** The stored version of the definition the run ran (absent: unknown). */
  flowVersion?: number
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

// --- LLM connections, memory, knowledge base and the flow assistant ---
// (docs/design/llm.md §6). Admin session only; none of it is on MCP.

export type LLMProvider = 'anthropic' | 'openai'

/** What a connection used (today, for the connections list). */
export interface LLMUsage {
  inputTokens: number
  outputTokens: number
  requests: number
}

/** An LLM connection as listed: the API key is write-only, only whether one is stored. */
export interface LLMConnection {
  name: string
  provider: LLMProvider
  /** '' uses the provider's default endpoint. */
  baseUrl: string
  hasApiKey: boolean
  /** The default model of the nodes that name none ('' when unset). */
  model: string
  /** Input plus output tokens per local day (0: no limit). */
  dailyTokenLimit: number
  usageToday: LLMUsage
}

/** The body of PUT /api/llm/connections/{name} and of its test. */
export interface LLMConnectionRequest {
  provider: LLMProvider
  baseUrl: string
  /** Omitted: keep the stored key; '': clear it. May hold {{secret.NAME}} references. */
  apiKey?: string
  model: string
  dailyTokenLimit: number
}

/** A model a connection offers (llm.ModelInfo). */
export interface LLMModel {
  id: string
  /** The display name, when the provider has one. */
  name?: string
}

/** POST /api/llm/connections/{name}/test: whether the models could be listed (spends no tokens). */
export interface LLMConnectionTest {
  ok: boolean
  error?: string
  models?: LLMModel[]
}

/** A stored conversation (llm.ConversationInfo). */
export interface LLMConversationInfo {
  key: string
  turns: number
  updatedAt: string
}

/** One message of a stored conversation (llm.Turn); text only. */
export interface LLMTurn {
  role: 'user' | 'assistant'
  text: string
  at: string
}

export interface LLMConversation {
  key: string
  turns: LLMTurn[]
}

/** A knowledge collection (llm.CollectionInfo). */
export interface KnowledgeCollection {
  name: string
  documents: number
  /** The embedding model every document of it was embedded with. */
  model: string
  dimensions: number
  updatedAt: string
}

/** One document of a collection (llm.Document; the embedding is never sent). */
export interface KnowledgeDocument {
  id: string
  text: string
  metadata?: Record<string, unknown>
  model: string
  createdAt: string
}

/** GET /api/llm/knowledge/{collection}: a page of documents and how many there are. */
export interface KnowledgeDocumentPage {
  documents: KnowledgeDocument[]
  total: number
}

/** A port of a stored node (types.Port). */
export interface StoredPort {
  id: string
  name: string
  dataType: string
  multiple: boolean
  required: boolean
}

/**
 * A node of a flow as the server stores it (flow.Node): what the assistant
 * proposes, every field set (the registered category and ports, the outputs
 * its config gives it, a laid-out position).
 */
export interface StoredFlowNode {
  id: string
  type: string
  /** input, processing or output ('' for an unregistered type). */
  category: string
  position: { x: number; y: number }
  config: Record<string, unknown>
  enabled: boolean
  inputs: StoredPort[]
  outputs: StoredPort[]
}

/** A connection as the server stores it (flow.Connection). */
export interface StoredFlowConnection {
  id: string
  sourceNode: string
  sourcePort: string
  targetNode: string
  targetPort: string
}

/** A flow definition the assistant reads (the canvas, unsaved) or proposes. */
export interface AssistantFlow {
  name: string
  description: string
  nodes: StoredFlowNode[]
  connections: StoredFlowConnection[]
}

/** POST /api/llm/assistant/flow (docs/design/llm.md §7). */
export interface AssistantFlowRequest {
  connection: string
  /** Empty or omitted: the connection's model. */
  model?: string
  description: string
  /** The editor's current definition, for "change this flow". */
  flow?: AssistantFlow
}

/** The assistant's proposal: nothing is saved, deployed or run. */
export interface AssistantFlowResult {
  flow: AssistantFlow
  notes: string
  /** What flow.Lint still finds in the proposal. */
  issues: FlowIssue[]
  usage: LLMUsage
}
