import type { Node, Edge } from '@vue-flow/core'

export type NodeCategory = 'trigger' | 'process' | 'action'

export interface NodeTypeConfig {
  id: string
  name: string
  category: NodeCategory
  icon: string
  description: string
  documentation?: string
  configSchema: ConfigField[]
}

export interface ConfigField {
  key: string
  label: string
  type: 'text' | 'number' | 'select' | 'boolean' | 'textarea' | 'cron'
  required?: boolean
  default?: any
  options?: { label: string; value: string }[]
  placeholder?: string
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
    config: Record<string, any>
  }
}

export type FlowEdge = Pick<Edge, 'id' | 'source' | 'target' | 'sourceHandle' | 'targetHandle' | 'type' | 'animated'>

export interface Flow {
  id: string
  name: string
  description?: string
  enabled: boolean
  running?: boolean
  nodes: FlowNode[]
  edges: FlowEdge[]
  createdAt: string
  updatedAt: string
}

export interface FlowListItem {
  id: string
  name: string
  description?: string
  enabled: boolean
  running: boolean
  nodeCount: number
  createdAt: string
  updatedAt: string
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
