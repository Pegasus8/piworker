import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Edge } from '@vue-flow/core'
import type { Flow, FlowNode, FlowListItem, NodeCategory } from '@/types'
import { useApi } from '@/composables/useApi'
import { useNodeTypesStore } from '@/stores/nodeTypes'

// Map backend categories to frontend categories
const backendCategoryMap: Record<string, NodeCategory> = {
  input: 'trigger',
  processing: 'process',
  output: 'action'
}

// Map frontend categories back to backend categories
const frontendCategoryMap: Record<string, string> = {
  trigger: 'input',
  process: 'processing',
  action: 'output'
}

export const useFlowsStore = defineStore('flows', () => {
  const api = useApi()
  const nodeTypesStore = useNodeTypesStore()

  const flows = ref<FlowListItem[]>([])
  const currentFlow = ref<Flow | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const isDeploying = ref(false)
  const isStopping = ref(false)

  // Current flow state
  const nodes = ref<FlowNode[]>([])
  const edges = ref<Edge[]>([])
  const selectedNodeId = ref<string | null>(null)
  const isDirty = ref(false)

  const selectedNode = computed(() => {
    if (!selectedNodeId.value) return null
    return nodes.value.find(n => n.id === selectedNodeId.value) || null
  })

  // Flow list operations
  async function fetchFlows() {
    loading.value = true
    error.value = null
    try {
      flows.value = await api.getFlows()
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to fetch flows'
    } finally {
      loading.value = false
    }
  }

  async function fetchFlow(id: string) {
    loading.value = true
    error.value = null
    try {
      let flowData = await api.getFlow(id)

      // Handle nested structure: API returns { flow: {...}, running: true }
      // Extract the inner flow object if present
      let flow: any
      let running = false

      if (flowData && (flowData as any).flow && (flowData as any).flow.id) {
        // Nested structure: { flow: {...}, running: bool }
        flow = (flowData as any).flow
        running = (flowData as any).running ?? false
      } else {
        // Direct structure: the flow object itself
        flow = flowData
        running = (flowData as any).running ?? false
      }

      flow.running = running
      currentFlow.value = flow

      // Ensure node-type metadata is loaded before transforming; otherwise nodes
      // would render with raw type strings and a generic icon if node-types are
      // still in flight (race between node-types load and flow load).
      await nodeTypesStore.fetchNodeTypes()

      // Transform backend nodes to Vue Flow format
      // Note: Backend uses 'connections', frontend uses 'edges'
      nodes.value = transformNodes(flow.nodes || [])
      edges.value = transformEdges(flow.connections || flow.edges || [])
      isDirty.value = false
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to fetch flow'
    } finally {
      loading.value = false
    }
  }

  // Transform backend nodes to Vue Flow format with full data structure
  function transformNodes(backendNodes: any[]): FlowNode[] {
    return backendNodes.map((node: any) => {
      // Get node type info from registry
      const nodeType = nodeTypesStore.getById(node.type)

      // Map backend category to frontend category
      const category = backendCategoryMap[node.category] || node.category as NodeCategory

      return {
        id: node.id,
        type: category, // Vue Flow uses this to select the component
        position: node.position || { x: 0, y: 0 },
        data: {
          label: nodeType?.name || node.type,
          nodeType: node.type,
          category: category,
          icon: nodeType?.icon || 'HelpCircle',
          config: node.config || {}
        }
      } as FlowNode
    })
  }

  // Transform backend edges/connections to Vue Flow format
  function transformEdges(backendEdges: any[]): Edge[] {
    return backendEdges.map((edge: any) => ({
      id: edge.id,
      source: edge.sourceNode || edge.source,
      target: edge.targetNode || edge.target,
      animated: true
    }))
  }

  // Reverse transform: frontend nodes → backend format
  function reverseTransformNodes(frontendNodes: FlowNode[]): any[] {
    return frontendNodes.map(node => ({
      id: node.id,
      type: node.data.nodeType,
      category: frontendCategoryMap[node.data.category] || node.data.category,
      position: node.position,
      config: node.data.config || {},
      enabled: true
    }))
  }

  // Reverse transform: frontend edges → backend connections format
  function reverseTransformEdges(frontendEdges: Edge[]): any[] {
    return frontendEdges.map(edge => ({
      id: edge.id,
      sourceNode: edge.source,
      sourcePort: 'output',
      targetNode: edge.target,
      targetPort: 'input'
    }))
  }

  async function createFlow(name: string, description?: string) {
    loading.value = true
    error.value = null
    try {
      const flow = await api.createFlow({ name, description })
      flows.value.push({
        id: flow.id,
        name: flow.name,
        description: flow.description,
        enabled: flow.enabled,
        running: false,
        nodeCount: 0,
        createdAt: flow.createdAt,
        updatedAt: flow.updatedAt
      })
      return flow
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to create flow'
      throw e
    } finally {
      loading.value = false
    }
  }

  async function saveFlow() {
    if (!currentFlow.value) return

    loading.value = true
    error.value = null
    try {
      const payload = {
        ...currentFlow.value,
        nodes: reverseTransformNodes(nodes.value),
        connections: reverseTransformEdges(edges.value)
      }
      // Remove frontend-only 'edges' key; backend uses 'connections'
      delete (payload as any).edges
      const prevRunning = currentFlow.value.running
      const updatedFlow = await api.updateFlow(currentFlow.value.id, payload as any)
      // The update response carries the flow but not its live running state;
      // preserve it so the editor's deploy/stop toggle stays accurate.
      updatedFlow.running = prevRunning
      currentFlow.value = updatedFlow
      isDirty.value = false

      // Update in list
      const idx = flows.value.findIndex(f => f.id === updatedFlow.id)
      if (idx !== -1) {
        flows.value[idx] = {
          ...flows.value[idx],
          name: updatedFlow.name,
          description: updatedFlow.description,
          enabled: updatedFlow.enabled,
          nodeCount: updatedFlow.nodes.length,
          updatedAt: updatedFlow.updatedAt
        }
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to save flow'
      throw e
    } finally {
      loading.value = false
    }
  }

  async function deleteFlow(id: string) {
    loading.value = true
    error.value = null
    try {
      await api.deleteFlow(id)
      flows.value = flows.value.filter(f => f.id !== id)
      if (currentFlow.value?.id === id) {
        currentFlow.value = null
        nodes.value = []
        edges.value = []
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to delete flow'
      throw e
    } finally {
      loading.value = false
    }
  }

  async function toggleFlow(id: string, enabled: boolean) {
    loading.value = true
    error.value = null
    try {
      await api.toggleFlow(id, enabled)
      const idx = flows.value.findIndex(f => f.id === id)
      if (idx !== -1) {
        flows.value[idx].enabled = enabled
      }
      if (currentFlow.value?.id === id) {
        currentFlow.value.enabled = enabled
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to toggle flow'
      throw e
    } finally {
      loading.value = false
    }
  }

  async function deployFlow(id?: string) {
    const flowId = id || currentFlow.value?.id
    if (!flowId) return

    isDeploying.value = true
    error.value = null
    try {
      const running = await api.deployFlow(flowId)
      applyRunningState(flowId, running)
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to deploy flow'
      throw e
    } finally {
      isDeploying.value = false
    }
  }

  async function stopFlow(id?: string) {
    const flowId = id || currentFlow.value?.id
    if (!flowId) return

    isStopping.value = true
    error.value = null
    try {
      const running = await api.stopFlow(flowId)
      applyRunningState(flowId, running)
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to stop flow'
      throw e
    } finally {
      isStopping.value = false
    }
  }

  // Apply the running state returned by deploy/stop directly to local state,
  // avoiding a redundant full getFlow round-trip just to read one boolean.
  function applyRunningState(flowId: string, running: boolean) {
    if (currentFlow.value?.id === flowId) {
      currentFlow.value.running = running
    }
    const idx = flows.value.findIndex(f => f.id === flowId)
    if (idx !== -1) {
      flows.value[idx].running = running
    }
  }

  // Node operations
  function addNode(node: FlowNode) {
    nodes.value.push(node)
    isDirty.value = true
  }

  function updateNode(id: string, updates: Partial<FlowNode>) {
    const idx = nodes.value.findIndex(n => n.id === id)
    if (idx !== -1) {
      nodes.value[idx] = { ...nodes.value[idx], ...updates }
      isDirty.value = true
    }
  }

  function updateNodeConfig(id: string, config: Record<string, any>) {
    const idx = nodes.value.findIndex(n => n.id === id)
    if (idx !== -1) {
      nodes.value[idx].data.config = config
      isDirty.value = true
    }
  }

  function removeNode(id: string) {
    nodes.value = nodes.value.filter(n => n.id !== id)
    edges.value = edges.value.filter(e => e.source !== id && e.target !== id)
    if (selectedNodeId.value === id) {
      selectedNodeId.value = null
    }
    isDirty.value = true
  }

  function selectNode(id: string | null) {
    selectedNodeId.value = id
  }

  // Edge operations
  function addEdge(edge: Edge) {
    // Prevent duplicate edges
    const exists = edges.value.some(
      e => e.source === edge.source && e.target === edge.target &&
           e.sourceHandle === edge.sourceHandle && e.targetHandle === edge.targetHandle
    )
    if (!exists) {
      edges.value.push(edge)
      isDirty.value = true
    }
  }

  function removeEdge(id: string) {
    edges.value = edges.value.filter(e => e.id !== id)
    isDirty.value = true
  }

  // Reset state
  function resetCurrentFlow() {
    currentFlow.value = null
    nodes.value = []
    edges.value = []
    selectedNodeId.value = null
    isDirty.value = false
  }

  function initNewFlow() {
    currentFlow.value = {
      id: '',
      name: 'Untitled Flow',
      description: '',
      enabled: false,
      nodes: [],
      edges: [],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    }
    nodes.value = []
    edges.value = []
    selectedNodeId.value = null
    isDirty.value = false
  }

  return {
    // State
    flows,
    currentFlow,
    nodes,
    edges,
    selectedNodeId,
    selectedNode,
    loading,
    error,
    isDirty,
    isDeploying,
    isStopping,

    // Flow operations
    fetchFlows,
    fetchFlow,
    createFlow,
    saveFlow,
    deleteFlow,
    toggleFlow,
    deployFlow,
    stopFlow,
    resetCurrentFlow,
    initNewFlow,

    // Node operations
    addNode,
    updateNode,
    updateNodeConfig,
    removeNode,
    selectNode,

    // Edge operations
    addEdge,
    removeEdge
  }
})
