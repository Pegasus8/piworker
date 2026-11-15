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
