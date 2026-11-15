<script setup lang="ts">
import { ERROR_PORT, hasErrorPort, nodeMinHeight, outputPorts, ports } from '@/lib/ports'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { computed, nextTick, watch } from 'vue'
import { Handle, Position, useVueFlow } from '@vue-flow/core'
import { cn } from '@/lib/utils'
import { useObservabilityStore } from '@/stores/observability'
import type { NodeCategory } from '@/types'
import { nodeIcon } from '@/lib/nodeIcons'
import { nodeSummary } from '@/lib/nodeSummary'
import { messages as m } from '@/i18n'

interface Props {
  id: string
  data: {
    label: string
    nodeType: string
    category: NodeCategory
    icon: string
    config: Record<string, any>
  }
  selected?: boolean
}

const props = defineProps<Props>()

const IconComponent = computed(() => nodeIcon(props.data.icon))

const categoryColors = {
  trigger: {
    bg: 'bg-amber-500/10 dark:bg-amber-500/20',
    border: 'border-amber-500',
    header: 'bg-amber-500',
    handle: 'bg-amber-500'
  },
  process: {
    bg: 'bg-blue-500/10 dark:bg-blue-500/20',
    border: 'border-blue-500',
    header: 'bg-blue-500',
    handle: 'bg-blue-500'
  },
  action: {
    bg: 'bg-purple-500/10 dark:bg-purple-500/20',
    border: 'border-purple-500',
    header: 'bg-purple-500',
    handle: 'bg-purple-500'
  }
}

const colors = computed(() => categoryColors[props.data.category])

const types = useNodeTypesStore()
const nodeType = computed(() => types.getById(props.data.nodeType))
const inputs = computed(() => ports(nodeType.value?.inputs))
// The declared outputs, then those the settings give (ADR-0040: the
// classifier's categories), so the ports follow the settings as typed.
const outputs = computed(() => outputPorts(nodeType.value, props.data.config))
// Every node that processes messages can hand its failures to the flow.
const errorPort = computed(() => hasErrorPort(nodeType.value))
// Tall enough to keep many handles apart (a classifier's categories).
const minHeight = computed(() => `${nodeMinHeight(inputs.value.length, outputs.value.length)}px`)
// What the node is set to do, instead of its type id; empty required
// settings are flagged so they are found before deploying.
const summary = computed(() => nodeSummary(props.data.config || {}, nodeType.value?.configSchema ?? []))

// Vue Flow measures a node's handles when it renders; ports added or
// removed by a setting are measured again, or edges would attach to where
// the old handles were.
const { updateNodeInternals } = useVueFlow()
watch(() => outputs.value.map(p => p.id).join('\n'), () => nextTick(() => updateNodeInternals([props.id])))

// Live execution state, streamed from the backend via the observability store.
const obs = useObservabilityStore()
const runState = computed(() => obs.nodeState(props.id))

const runRing = computed(() => {
  switch (runState.value?.phase) {
    case 'running':
      return 'ring-2 ring-sky-400 ring-offset-2 ring-offset-background animate-pulse'
    case 'success':
      return 'ring-2 ring-emerald-500 ring-offset-2 ring-offset-background'
    case 'error':
      return 'ring-2 ring-red-500 ring-offset-2 ring-offset-background'
    default:
      return ''
  }
})

// A live run-state ring takes precedence over the selection ring.
const ringClass = computed(() =>
  runRing.value || (props.selected ? 'ring-2 ring-ring ring-offset-2 ring-offset-background' : '')
)

const dotClass = computed(() => {
  switch (runState.value?.phase) {
    case 'running':
      return 'bg-sky-300 animate-pulse'
    case 'success':
      return 'bg-emerald-300'
    case 'error':
      return 'bg-red-300'
    default:
      return ''
  }
})

const durationLabel = computed(() => {
  const s = runState.value
  if (!s || s.phase === 'running' || s.durMs == null) return ''
  return `${s.durMs.toFixed(s.durMs < 1 ? 2 : 0)}ms`
})
</script>

<template>
  <div
    :class="cn(
      'min-w-[160px] rounded-lg border-2 shadow-lg transition-all duration-200',
      colors.bg,
      colors.border,
      ringClass
    )"
    :style="{ minHeight }"
  >
    <!-- Header -->
    <div
      :class="cn(
        'flex items-center gap-2 rounded-t-md px-3 py-2 text-white',
        colors.header
      )"
    >
      <component :is="IconComponent" class="h-4 w-4" />
      <span class="text-sm font-medium">{{ data.label }}</span>
      <!-- Live execution status -->
      <span
        v-if="runState"
        class="ml-auto flex items-center gap-1 text-[10px] font-normal text-white/90"
      >
        <span v-if="durationLabel">{{ durationLabel }}</span>
        <span :class="cn('h-2 w-2 rounded-full', dotClass)" />
      </span>
    </div>

    <!-- Body -->
    <div v-if="summary.text || summary.missing.length" class="max-w-[220px] px-3 py-2">
      <div v-if="summary.missing.length" class="text-xs text-amber-600 dark:text-amber-400">{{ m.editor.missingSettings(summary.missing) }}</div>
      <div v-else class="truncate text-xs text-muted-foreground" :title="summary.text">{{ summary.text }}</div>
    </div>

    <div v-if="outputs.length > 1" class="px-3 pb-2 text-right text-xs text-muted-foreground">{{ m.editor.outputs(outputs.map(p => p.name)) }}</div>
    <!-- Input Handle -->
    <Handle
      v-for="(port, index) in inputs"
      :key="`in-${port.id}`" :id="port.id" :title="m.editor.inputPort(port.name)"
      :style="{ top: `${(index + 1) * 100 / (inputs.length + 1)}%` }"
      type="target"
      :position="Position.Left"
      :class="cn(
        'w-3 h-3 rounded-full border-2 border-background',
        colors.handle
      )"
    />

    <!-- Output Handle -->
    <Handle
      v-for="(port, index) in outputs"
      :key="`out-${port.id}`" :id="port.id" :title="m.editor.outputPort(port.name)"
      :style="{ top: `${(index + 1) * 100 / (outputs.length + 1)}%` }"
      type="source"
      :position="Position.Right"
      :class="cn(
        'w-3 h-3 rounded-full border-2 border-background',
        colors.handle
      )"
    />

    <!-- Error port: at the bottom, apart from the outputs -->
    <Handle
      v-if="errorPort"
      :id="ERROR_PORT" :title="m.editor.errorPort"
      type="source"
      :position="Position.Bottom"
      class="error-port w-3 h-3 rounded-full border-2 border-background"
    />
  </div>
</template>

<style scoped>
.vue-flow__handle {
  background-color: inherit;
}
/* The error port stands apart from the outputs whatever the node's colour. */
.vue-flow__handle.error-port {
  background-color: rgb(239 68 68);
}
</style>
