<script setup lang="ts">
import { validConnection } from '@/lib/ports'
import { ref, onMounted, watch, markRaw, nextTick } from 'vue'
import { VueFlow, useVueFlow, ConnectionMode } from '@vue-flow/core'
import type { NodeTypesObject } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { TriggerNode, ProcessNode, OutputNode } from './nodes'
import type { FlowNode, NodeCategory } from '@/types'

import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'

const flowsStore = useFlowsStore()
const nodeTypesStore = useNodeTypesStore()

const flowContainer = ref<HTMLDivElement | null>(null)

const { onConnect, onNodeClick, onPaneClick, onEdgesChange, onNodeDragStop, addNodes, project, setNodes, setEdges, fitView } = useVueFlow()

// Custom node types
const nodeTypes: NodeTypesObject = {
  trigger: markRaw(TriggerNode) as any,
  process: markRaw(ProcessNode) as any,
  action: markRaw(OutputNode) as any
}

// Handle connections
onConnect((connection) => {
  if (validConnection(connection, flowsStore.nodes, flowsStore.edges, nodeTypesStore.getById)) {
    const edge = {
      id: `edge_${connection.source}_${connection.target}_${Date.now()}`,
      source: connection.source,
      target: connection.target,
      sourceHandle: connection.sourceHandle,
      targetHandle: connection.targetHandle,
      animated: true
    }
    flowsStore.addEdge(edge)
  }
})

// Handle node click for selection
onNodeClick(({ node }) => {
  flowsStore.selectNode(node.id)
})

// Handle pane click to deselect
onPaneClick(() => {
  flowsStore.selectNode(null)
})

// Sync edge deletions (via Delete/Backspace key) back to Pinia store
onEdgesChange((changes) => {
  for (const change of changes) {
    if (change.type === 'remove') {
      flowsStore.removeEdge(change.id)
    }
  }
})

// Sync node drag positions back to Pinia store
onNodeDragStop(({ node }) => {
  flowsStore.updateNode(node.id, { position: node.position })
})

// Sync store nodes/edges to vue-flow on STRUCTURAL changes (the array is
// replaced wholesale on load or remove). We intentionally avoid { deep: true }:
// deep-watching re-pushes the entire graph into Vue Flow on every nested
// mutation (a config edit, a drag), causing full re-renders and feedback churn
// with the drag/connect handlers. Node additions are pushed via addNodes above;
// connections replace the store array and sync through setEdges below.
watch(
  () => flowsStore.nodes,
  (nodes) => {
    setNodes(nodes)
  }
)

watch(
  () => flowsStore.edges,
  (edges) => {
    setEdges(edges)
  }
)

// Drag and drop handling
function onDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
}

let nodeIdCounter = 0

function onDrop(event: DragEvent) {
  event.preventDefault()

  const nodeTypeId = event.dataTransfer?.getData('application/vueflow-nodetype')
  if (!nodeTypeId || !flowContainer.value) return

  const bounds = flowContainer.value.getBoundingClientRect()
  insertNode(nodeTypeId, project({ x: event.clientX - bounds.left, y: event.clientY - bounds.top }))
}

async function addNodeType(nodeTypeId: string) {
  const bounds = flowContainer.value?.getBoundingClientRect()
  if (!bounds) return
  const id = insertNode(nodeTypeId, { x: (flowsStore.nodes.length % 3) * 260, y: Math.floor(flowsStore.nodes.length / 3) * 180 })
  if (id) {
    flowsStore.selectNode(id)
    await nextTick()
    fitView({ padding: 0.3, maxZoom: 1 })
  }
}
function insertNode(nodeTypeId: string, position: { x: number; y: number }) {
  const nodeType = nodeTypesStore.getById(nodeTypeId)
  if (!nodeType) return
  // Build default config from schema
  const config: Record<string, any> = {}
  nodeType.configSchema.forEach(field => {
    if (field.default !== undefined) {
      config[field.key] = field.default
    }
  })

  const id = `node_${Date.now()}_${++nodeIdCounter}`

  const newNode: FlowNode = {
    id,
    type: nodeType.category,
    position,
    data: {
      label: nodeType.name,
      nodeType: nodeType.id,
      category: nodeType.category as NodeCategory,
      icon: nodeType.icon,
      config
    }
  }

  flowsStore.addNode(newNode)
  addNodes([newNode])
  return id
}
defineExpose({ addNodeType })

// Minimap node color
function minimapNodeColor(node: any): string {
  switch (node.data?.category) {
    case 'trigger':
      return '#f59e0b' // amber-500
    case 'process':
      return '#3b82f6' // blue-500
    case 'action':
      return '#a855f7' // purple-500
    default:
      return '#6b7280' // gray-500
  }
}

onMounted(() => {
  // Initial fit
  setTimeout(() => {
    fitView({ padding: 0.3, maxZoom: 1 })
  }, 100)
})
</script>

<template>
  <div
    ref="flowContainer"
    class="h-full w-full"
    @dragover="onDragOver"
    @drop="onDrop"
  >
    <VueFlow
      :nodes="flowsStore.nodes"
      :edges="flowsStore.edges"
      :node-types="nodeTypes"
      :default-viewport="{ x: 0, y: 0, zoom: 1 }"
      :min-zoom="0.2"
      :max-zoom="4"
      :snap-to-grid="true"
      :snap-grid="[15, 15]"
      :connection-mode="ConnectionMode.Strict"
      :is-valid-connection="connection => validConnection(connection, flowsStore.nodes, flowsStore.edges, nodeTypesStore.getById)"
      class="bg-background"
    >
      <Background pattern-color="hsl(var(--border))" :gap="20" />
      <Controls position="bottom-left" />
      <MiniMap
        :node-color="minimapNodeColor"
        :node-stroke-color="minimapNodeColor"
        position="bottom-right"
        class="hidden sm:block bg-card border rounded-lg"
      />
    </VueFlow>
  </div>
</template>

<style>
.vue-flow__minimap {
  background-color: hsl(var(--card));
}

.vue-flow__controls {
  background-color: hsl(var(--card));
  border-color: hsl(var(--border));
}

.vue-flow__controls-button {
  background-color: hsl(var(--card));
  color: hsl(var(--foreground));
  border-color: hsl(var(--border));
}

.vue-flow__controls-button:hover {
  background-color: hsl(var(--accent));
}

.vue-flow__controls-button svg {
  fill: currentColor;
}
</style>
