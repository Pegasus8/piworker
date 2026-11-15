// Code generated from the server's Go response types. DO NOT EDIT.
// Regenerate: go test -run TestWireTypesAreCurrent ./internal/api -update
//
// The JSON shapes the server sends. The types the interface uses are the
// hand-written ones in index.ts; wire.check.ts keeps both in step.

/** storage.FlowRun */
export interface FlowRun {
  id: string
  flowId: string
  startedAt: string
  finishedAt?: string
  status: string
  nodeCount: number
  errorCount: number
  actor?: string
  flowVersion?: number
}

/** storage.NodeEventRecord */
export interface NodeEventRecord {
  id: number
  runId: string
  flowId: string
  nodeId: string
  nodeType: string
  phase: string
  msgId?: string
  durationMs?: number
  error?: string
  createdAt: string
}

/** events.Event */
export interface Event {
  flowId: string
  nodeId: string
  nodeType?: string
  phase: string
  msgId?: string
  corrId?: string
  durMs?: number
  error?: string
  debug?: unknown
  ts?: number
}

/** service.DeploymentView */
export interface DeploymentView {
  version: number
  actorKind: string
  tokenName?: string
  deployedAt: string
}

/** service.VersionEntry */
export interface VersionEntry {
  version: number
  actorKind: string
  tokenId?: string
  tokenName?: string
  adminApproved: boolean
  note?: string
  draftId?: string
  createdAt: string
  deployed: boolean
}

/** service.VersionList */
export interface VersionList {
  current: number
  lastAdminApproved: number
  versions: VersionEntry[]
}

/** flow.CallResult */
export interface CallResult {
  status: string
  runId: string
  result?: unknown
  error?: CallError
  durationMs: number
}

/** flow.CallError */
export interface CallError {
  nodeId?: string
  nodeType?: string
  message: string
}

/** api.TLSInfo */
export interface TLSInfo {
  mode: string
  caFingerprint: string
  sans: string[]
  notAfter: string | null
  caCertUrl: string
  httpsPort: number | null
  httpPort: number | null
  warning?: string
}

/** api.LLMConnectionsResponse */
export interface LLMConnectionsResponse {
  connections: LLMConnectionItem[]
}

/** api.LLMConnectionItem */
export interface LLMConnectionItem {
  name: string
  provider: string
  baseUrl: string
  hasApiKey: boolean
  model: string
  dailyTokenLimit: number
  usageToday: Usage
}

/** llm.Usage */
export interface Usage {
  inputTokens: number
  outputTokens: number
  requests: number
}

/** api.LLMConnectionTest */
export interface LLMConnectionTest {
  ok: boolean
  error?: string
  models?: ModelInfo[]
}

/** llm.ModelInfo */
export interface ModelInfo {
  id: string
  name?: string
}

/** api.LLMModelsResponse */
export interface LLMModelsResponse {
  models: ModelInfo[]
}

/** api.LLMConversationsResponse */
export interface LLMConversationsResponse {
  conversations: ConversationInfo[]
}

/** llm.ConversationInfo */
export interface ConversationInfo {
  key: string
  turns: number
  updatedAt: string
}

/** api.LLMConversation */
export interface LLMConversation {
  key: string
  turns: Turn[]
}

/** llm.Turn */
export interface Turn {
  role: string
  text: string
  at: string
}

/** api.KnowledgeCollectionsResponse */
export interface KnowledgeCollectionsResponse {
  collections: CollectionInfo[]
}

/** llm.CollectionInfo */
export interface CollectionInfo {
  name: string
  documents: number
  model: string
  dimensions: number
  updatedAt: string
}

/** api.KnowledgeDocumentPage */
export interface KnowledgeDocumentPage {
  documents: Document[]
  total: number
}

/** llm.Document */
export interface Document {
  id: string
  text: string
  metadata?: Record<string, unknown>
  model: string
  createdAt: string
}

/** service.AssistantFlowResult */
export interface AssistantFlowResult {
  flow: AssistantFlow
  notes: string
  issues: Issue[]
  usage: Usage
}

/** service.AssistantFlow */
export interface AssistantFlow {
  name: string
  description: string
  nodes: Node[]
  connections: Connection[]
}

/** flow.Node */
export interface Node {
  id: string
  type: string
  category: string
  position: Position
  config: Record<string, unknown>
  inputs: Port[]
  outputs: Port[]
  enabled: boolean
}

/** flow.Position */
export interface Position {
  x: number
  y: number
}

/** types.Port */
export interface Port {
  id: string
  name: string
  dataType: string
  multiple: boolean
  required: boolean
}

/** flow.Connection */
export interface Connection {
  id: string
  sourceNode: string
  sourcePort: string
  targetNode: string
  targetPort: string
}

/** flow.Issue */
export interface Issue {
  path: string
  code: string
  severity: string
  message: string
}
