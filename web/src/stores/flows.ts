import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Flow, FlowNode, FlowEdge, FlowListItem, NodeCategory, VersionList } from '@/types'
import type { ApplyResult } from '@/lib/drafts'
import { useApi } from '@/composables/useApi'
import { ports, validConnection } from '@/lib/ports'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { autoEdge, nextPosition } from '@/lib/autoConnect'
import { msg } from '@/i18n'

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

/** A save or deploy refused because the flow was saved elsewhere (an
 * approved agent draft, another tab) after the editor loaded it. Nothing was
 * overwritten or deployed. */
export class FlowVersionConflictError extends Error {
  constructor(readonly currentVersion: number, message: string) {
    super(message)
    this.name = 'FlowVersionConflictError'
  }
}

/** The 409 of a stale save or deploy carries data.currentVersion; other
 * 409s (a running flow) do not. */
function versionConflict(e: unknown): FlowVersionConflictError | null {
  const response = (e as { response?: { status?: number; data?: any } })?.response
  const current = response?.data?.data?.currentVersion
  if (response?.status !== 409 || typeof current !== 'number') return null
  return new FlowVersionConflictError(current, String(response.data?.error || msg().flowTools.errors.versionConflict))
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
  const edges = ref<FlowEdge[]>([])
  const selectedNodeId = ref<string | null>(null)
  const isDirty = ref(false)

  // Undo/redo of canvas changes: snapshots of nodes and edges taken before
  // each change. Consecutive edits of one node's settings share one step.
  type Snapshot = { nodes: FlowNode[]; edges: FlowEdge[] }
  const HISTORY_LIMIT = 100
  const past = ref<Snapshot[]>([])
  const future = ref<Snapshot[]>([])
  let lastGroup: { key: string; at: number } | null = null
  const canUndo = computed(() => past.value.length > 0)
  const canRedo = computed(() => future.value.length > 0)
  function snapshot(): Snapshot {
    return JSON.parse(JSON.stringify({ nodes: nodes.value, edges: edges.value }))
  }
  function record(group?: string) {
    const now = Date.now()
    if (group && lastGroup?.key === group && now - lastGroup.at < 2000) { lastGroup.at = now; return }
    lastGroup = group ? { key: group, at: now } : null
    past.value = [...past.value.slice(-(HISTORY_LIMIT - 1)), snapshot()]
    future.value = []
  }
  function restore(state: Snapshot) {
    nodes.value = state.nodes
    edges.value = state.edges
    if (selectedNodeId.value && !nodes.value.some(n => n.id === selectedNodeId.value)) selectedNodeId.value = null
    isDirty.value = true
    lastGroup = null
  }
  function undo() {
    const previous = past.value[past.value.length - 1]
    if (!previous) return
    future.value = [...future.value, snapshot()]
    past.value = past.value.slice(0, -1)
    restore(previous)
  }
  function redo() {
    const next = future.value[future.value.length - 1]
    if (!next) return
    past.value = [...past.value, snapshot()]
    future.value = future.value.slice(0, -1)
    restore(next)
  }
  function clearHistory() {
    past.value = []
    future.value = []
    lastGroup = null
  }

  const invalidConnections = computed(() => edges.value.filter(edge => !validConnection(edge, nodes.value, edges.value.filter(other => other.id !== edge.id), nodeTypesStore.getById)))

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
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.fetchFlows
    } finally {
      loading.value = false
    }
  }

  // Incremented on every fetchFlow call; a response whose token is no longer
  // current belongs to a flow the user already navigated away from.
  let fetchFlowToken = 0

  async function fetchFlow(id: string) {
    const token = ++fetchFlowToken
    const isCurrent = () => token === fetchFlowToken
    loading.value = true
    error.value = null
    try {
      // api.getFlow already unwraps the { flow, running } envelope and returns
      // the flow object with .running set.
      const flow = (await api.getFlow(id)) as any

      // Ensure node-type metadata is loaded before transforming; otherwise nodes
      // would render with raw type strings and a generic icon if node-types are
      // still in flight (race between node-types load and flow load).
      await nodeTypesStore.fetchNodeTypes()
      if (!isCurrent()) return

      currentFlow.value = flow
      // Transform backend nodes to Vue Flow format
      // Note: Backend uses 'connections', frontend uses 'edges'
      nodes.value = transformNodes(flow.nodes || [])
      edges.value = transformEdges(flow.connections || flow.edges || [])
      isDirty.value = false
      clearHistory()
    } catch (e) {
      if (!isCurrent()) return
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.fetchFlow
    } finally {
      if (isCurrent()) loading.value = false
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
          config: node.config || {},
          enabled: node.enabled ?? true
        }
      } as FlowNode
    })
  }

  // Transform backend edges/connections to Vue Flow format
  function transformEdges(backendEdges: any[]): FlowEdge[] {
    return backendEdges.map((edge: any) => ({
      id: edge.id,
      source: edge.sourceNode || edge.source,
      target: edge.targetNode || edge.target,
      sourceHandle: edge.sourcePort ?? edge.sourceHandle ?? 'output',
      targetHandle: edge.targetPort ?? edge.targetHandle ?? 'input',
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
      enabled: node.data.enabled ?? true,
      inputs: ports(nodeTypesStore.getById(node.data.nodeType)?.inputs),
      outputs: ports(nodeTypesStore.getById(node.data.nodeType)?.outputs)
    }))
  }

  // Reverse transform: frontend edges → backend connections format
  function reverseTransformEdges(frontendEdges: FlowEdge[]): any[] {
    return frontendEdges.map(edge => ({
      id: edge.id,
      sourceNode: edge.source,
      sourcePort: edge.sourceHandle ?? 'output',
      targetNode: edge.target,
      targetPort: edge.targetHandle ?? 'input'
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
        state: flow.state,
        running: false,
        nodeCount: 0,
        createdAt: flow.createdAt,
        updatedAt: flow.updatedAt
      })
      return flow
    } catch (e) {
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.createFlow
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
        // The version this editor loaded: the server refuses the save (409)
        // when another one was saved since, instead of overwriting it.
        version: currentFlow.value.version,
        nodes: reverseTransformNodes(nodes.value),
        connections: reverseTransformEdges(edges.value)
      }
      // Remove frontend-only 'edges' key; backend uses 'connections'
      delete (payload as any).edges
      const prevRunning = currentFlow.value.running
      let updatedFlow: Flow
      try {
        updatedFlow = await api.updateFlow(currentFlow.value.id, payload as any)
      } catch (e) {
        throw versionConflict(e) ?? e
      }
      // The update response carries the flow but not its live running state
      // or its marks; preserve them so the editor's deploy/stop toggle stays
      // accurate. An editor save is a version the admin approved.
      updatedFlow.running = prevRunning
      updatedFlow.deployment = currentFlow.value.deployment ?? null
      updatedFlow.agentChanged = false
      currentFlow.value = updatedFlow
      isDirty.value = false

      // Update in list
      const idx = flows.value.findIndex(f => f.id === updatedFlow.id)
      if (idx !== -1) {
        flows.value[idx] = {
          ...flows.value[idx],
          name: updatedFlow.name,
          description: updatedFlow.description,
          state: updatedFlow.state,
          nodeCount: updatedFlow.nodes.length,
          version: updatedFlow.version,
          agentChanged: false,
          updatedAt: updatedFlow.updatedAt
        }
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.saveFlow
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
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.deleteFlow
      throw e
    } finally {
      loading.value = false
    }
  }

  async function toggleFlow(id: string, enabled: boolean) {
    loading.value = true
    error.value = null
    try {
      // Turning a flow on sends the version the list shows: the server
      // refuses (409) to deploy another one saved since (an approved draft,
      // an agent's version) instead of running what nobody saw.
      const item = flows.value.find(f => f.id === id)
      let running: boolean
      try {
        running = await api.toggleFlow(id, enabled, enabled ? item?.version : undefined)
      } catch (e) {
        throw versionConflict(e) ?? e
      }
      applyRunningState(id, running)
      // Turning on deploys only a flow that was not running, and the list
      // may be older than an agent's deploy: read who deployed it instead
      // of guessing from the list.
      if (enabled) await refreshFlowState(id)
    } catch (e) {
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.toggleFlow
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
      // Deploying the flow the editor shows sends its version: the server
      // refuses (409) to deploy another one saved since (an approved agent
      // draft, another tab) instead of running what nobody saw here.
      const version = currentFlow.value?.id === flowId ? currentFlow.value.version : undefined
      let running: boolean
      try {
        running = await api.deployFlow(flowId, version)
      } catch (e) {
        throw versionConflict(e) ?? e
      }
      if (running) markEditorDeployment(flowId, version ?? flows.value.find(f => f.id === flowId)?.version)
      applyRunningState(flowId, running)
    } catch (e) {
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.deployFlow
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
      error.value = e instanceof Error ? e.message : msg().flowTools.errors.stopFlow
      throw e
    } finally {
      isStopping.value = false
    }
  }

  // A deploy from the editor replaces the last deployment marker (the server
  // records it as the editor's); stopping keeps it.
  function markEditorDeployment(flowId: string, version: number | undefined) {
    const mark = { version: version ?? 0, actorKind: 'editor', deployedAt: new Date().toISOString() }
    if (currentFlow.value?.id === flowId) currentFlow.value.deployment = { ...mark }
    const item = flows.value.find(f => f.id === flowId)
    if (item) item.deployment = { ...mark }
  }

  // Version history (newest first) and rollback to the version the admin
  // last approved (docs/design/mcp.md §10).
  async function fetchVersions(id: string, limit = 50): Promise<VersionList> {
    return api.getFlowVersions(id, limit)
  }

  // Rolling back saves the version the admin last approved as a new version
  // and redeploys a running flow. The flow the editor shows is reloaded (the
  // caller confirms discarding unsaved changes first).
  async function rollbackFlow(id: string): Promise<ApplyResult> {
    const result = await api.rollbackFlow(id)
    const item = flows.value.find(f => f.id === id)
    if (item) {
      item.version = result.version
      item.agentChanged = false
    }
    // A refused redeploy may have left the old version running (refused
    // before stopping it) or stopped it with its running intent kept: read
    // the flow back. If that fails, claim only what the result says.
    if (!await refreshFlowState(id) && result.deployed) {
      applyRunningState(id, true)
      markEditorDeployment(id, result.version)
    }
    if (currentFlow.value?.id === id) {
      isDirty.value = false
      await fetchFlow(id)
    }
    return result
  }

  // Reads one flow's live state into the list: whether it runs, its version
  // and marks (GET /api/flows/{id}). A stored running intent without a
  // running instance is a failed deploy, as the read views show it. Returns
  // false, changing nothing, when the flow cannot be read.
  async function refreshFlowState(id: string): Promise<boolean> {
    let flow: Flow
    try {
      flow = await api.getFlow(id)
    } catch {
      return false
    }
    const idx = flows.value.findIndex(f => f.id === id)
    if (idx === -1) return true
    const running = flow.running === true
    const item = flows.value[idx]
    flows.value[idx] = {
      ...item,
      running,
      state: running ? 'running' : flow.state === 'running' ? 'failed' : flow.state ?? item.state,
      version: typeof flow.version === 'number' ? flow.version : item.version,
      deployment: flow.deployment ?? null,
      agentChanged: flow.agentChanged === true
    }
    return true
  }

  // Apply the running state returned by deploy/stop directly to local state,
  // avoiding a redundant full getFlow round-trip just to read one boolean.
  function applyRunningState(flowId: string, running: boolean) {
    if (currentFlow.value?.id === flowId) {
      currentFlow.value.running = running
      currentFlow.value.state = running ? 'running' : 'inactive'
    }
    const idx = flows.value.findIndex(f => f.id === flowId)
    if (idx !== -1) {
      flows.value[idx].running = running
      flows.value[idx].state = running ? 'running' : 'inactive'
    }
  }

  // Node operations
  function addNode(node: FlowNode) {
    record()
    nodes.value.push(node)
    isDirty.value = true
  }

  // Adds a node after another (or alone) and connects them when both have
  // a port for it: one undo step.
  function addNodeAfter(node: FlowNode, afterId: string | null) {
    record()
    const after = afterId ? nodes.value.find(n => n.id === afterId) : undefined
    node.position = nextPosition(after, nodes.value)
    nodes.value.push(node)
    const edge = after ? autoEdge(after, node, nodes.value, edges.value, nodeTypesStore.getById) : null
    if (edge) edges.value = [...edges.value, edge]
    isDirty.value = true
    return edge
  }

  function updateNode(id: string, updates: Partial<FlowNode>) {
    const idx = nodes.value.findIndex(n => n.id === id)
    if (idx !== -1) {
      record(updates.position ? undefined : `node:${id}`)
      nodes.value[idx] = { ...nodes.value[idx], ...updates }
      isDirty.value = true
    }
  }

  function updateNodeConfig(id: string, config: Record<string, any>) {
    const idx = nodes.value.findIndex(n => n.id === id)
    if (idx !== -1) {
      record(`config:${id}`)
      nodes.value[idx].data.config = config
      isDirty.value = true
    }
  }

  function removeNode(id: string) {
    record()
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
  function addEdge(edge: FlowEdge) {
    // Prevent duplicate edges
    const exists = edges.value.some(
      e => e.source === edge.source && e.target === edge.target &&
           e.sourceHandle === edge.sourceHandle && e.targetHandle === edge.targetHandle
    )
    if (!exists) {
      record()
      edges.value = [...edges.value, edge]
      isDirty.value = true
    }
  }

  function removeEdge(id: string) {
    if (!edges.value.some(e => e.id === id)) return
    record()
    edges.value = edges.value.filter(e => e.id !== id)
    isDirty.value = true
  }

  // Reset state
  // Abandon any fetchFlow still in flight so its late response cannot replace
  // the flow the editor switched to.
  function discardPendingFlowFetch() {
    fetchFlowToken++
    loading.value = false
  }

  function resetCurrentFlow() {
    discardPendingFlowFetch()
    currentFlow.value = null
    nodes.value = []
    edges.value = []
    selectedNodeId.value = null
    isDirty.value = false
    clearHistory()
  }

  function initNewFlow(name?: string) {
    discardPendingFlowFetch()
    currentFlow.value = {
      id: '',
      name: name?.trim() || msg().editor.untitled,
      description: '',
      state: 'inactive',
      nodes: [],
      edges: [],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    }
    nodes.value = []
    edges.value = []
    selectedNodeId.value = null
    isDirty.value = false
    clearHistory()
  }

  return {
    // State
    flows,
    currentFlow,
    nodes,
    edges,
    invalidConnections,
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
    fetchVersions,
    rollbackFlow,
    resetCurrentFlow,
    initNewFlow,

    // History
    canUndo,
    canRedo,
    undo,
    redo,

    // Node operations
    addNode,
    addNodeAfter,
    updateNode,
    updateNodeConfig,
    removeNode,
    selectNode,

    // Edge operations
    addEdge,
    removeEdge
  }
})
