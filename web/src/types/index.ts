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

export interface FlowNode extends Node {
  data: {
    label: string
    nodeType: string
    category: NodeCategory
    icon: string
    config: Record<string, any>
  }
}

export interface Flow {
  id: string
  name: string
  description?: string
  enabled: boolean
  running?: boolean
  nodes: FlowNode[]
  edges: Edge[]
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
