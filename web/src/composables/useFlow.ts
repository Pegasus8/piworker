import { useVueFlow } from '@vue-flow/core'
import type { Connection, Edge } from '@vue-flow/core'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import type { FlowNode, NodeCategory } from '@/types'

function generateNodeId(): string {
  return `node_${crypto.randomUUID()}`
}

export function useFlow() {
  const flowsStore = useFlowsStore()
  const nodeTypesStore = useNodeTypesStore()

  const {
    onConnect,
    addNodes,
    addEdges,
    project,
    getNodes,
    fitView
  } = useVueFlow()

  // Handle connection between nodes
  onConnect((connection: Connection) => {
    if (connection.source && connection.target) {
      const edge: Edge = {
        id: `edge_${connection.source}_${connection.target}`,
        source: connection.source,
        target: connection.target,
        sourceHandle: connection.sourceHandle || undefined,
        targetHandle: connection.targetHandle || undefined,
        animated: true
      }
      flowsStore.addEdge(edge)
      addEdges([edge])
    }
  })

  // Validate connections
  function isValidConnection(connection: Connection): boolean {
    const sourceNode = flowsStore.nodes.find(n => n.id === connection.source)
    const targetNode = flowsStore.nodes.find(n => n.id === connection.target)

    if (!sourceNode || !targetNode) return false

    // Cannot connect to self
    if (connection.source === connection.target) return false

    // Check if connection already exists
    const exists = flowsStore.edges.some(
      e => e.source === connection.source &&
           e.target === connection.target
    )
    if (exists) return false

    return true
  }

  // Create a new node from palette drop
  function createNode(
    nodeTypeId: string,
    position: { x: number; y: number }
  ): FlowNode | null {
    const nodeType = nodeTypesStore.getById(nodeTypeId)
    if (!nodeType) return null

    const id = generateNodeId()

    // Build default config from schema
    const config: Record<string, any> = {}
    nodeType.configSchema.forEach(field => {
      if (field.default !== undefined) {
        config[field.key] = field.default
      }
    })

    const node: FlowNode = {
      id,
      type: nodeType.category,
      position,
      data: {
        label: nodeType.name,
        nodeType: nodeType.id,
        category: nodeType.category,
        icon: nodeType.icon,
        config
      }
    }

    return node
  }

  // Handle drop from palette
  function handleDrop(event: DragEvent, containerBounds: DOMRect) {
    const nodeTypeId = event.dataTransfer?.getData('application/vueflow-nodetype')
    if (!nodeTypeId) return

    const position = project({
      x: event.clientX - containerBounds.left,
      y: event.clientY - containerBounds.top
    })

    const node = createNode(nodeTypeId, position)
    if (node) {
      flowsStore.addNode(node)
      addNodes([node])
    }
  }

  // Get color class for node category
  function getNodeColorClass(category: NodeCategory): string {
    switch (category) {
      case 'trigger':
        return 'bg-amber-500'
      case 'process':
        return 'bg-blue-500'
      case 'action':
        return 'bg-purple-500'
      default:
        return 'bg-gray-500'
    }
  }

  function getNodeBorderClass(category: NodeCategory): string {
    switch (category) {
      case 'trigger':
        return 'border-amber-500'
      case 'process':
        return 'border-blue-500'
      case 'action':
        return 'border-purple-500'
      default:
        return 'border-gray-500'
    }
  }

  // Sync vue-flow changes with store
  function syncNodesFromVueFlow() {
    const vueFlowNodes = getNodes.value
    vueFlowNodes.forEach(vfNode => {
      const storeNode = flowsStore.nodes.find(n => n.id === vfNode.id)
      if (storeNode) {
        flowsStore.updateNode(vfNode.id, {
          position: vfNode.position
        })
      }
    })
  }

  return {
    createNode,
    handleDrop,
    isValidConnection,
    getNodeColorClass,
    getNodeBorderClass,
    syncNodesFromVueFlow,
    fitView,
    project
  }
}
