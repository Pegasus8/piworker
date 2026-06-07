<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import { cn } from '@/lib/utils'
import type { NodeCategory } from '@/types'
import {
  Clock,
  Webhook,
  Radio,
  Cpu,
  Code,
  GitBranch,
  Timer,
  FileText,
  Globe,
  Send,
  Zap,
  Bug,
  Mail,
  HelpCircle
} from 'lucide-vue-next'

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

const iconComponents: Record<string, any> = {
  Clock,
  Webhook,
  Radio,
  Cpu,
  Code,
  GitBranch,
  Timer,
  FileText,
  Globe,
  Send,
  Zap,
  Bug,
  Mail
}

const IconComponent = computed(() => iconComponents[props.data.icon] || HelpCircle)

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

const showInputHandle = computed(() => props.data.category !== 'trigger')
const showOutputHandle = computed(() => props.data.category !== 'action')
</script>

<template>
  <div
    :class="cn(
      'min-w-[160px] rounded-lg border-2 shadow-lg transition-all duration-200',
      colors.bg,
      colors.border,
      selected && 'ring-2 ring-ring ring-offset-2 ring-offset-background'
    )"
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
    </div>

    <!-- Body -->
    <div class="px-3 py-2">
      <div class="text-xs text-muted-foreground">
        {{ data.nodeType }}
      </div>
    </div>

    <!-- Input Handle -->
    <Handle
      v-if="showInputHandle"
      type="target"
      :position="Position.Left"
      :class="cn(
        'w-3 h-3 rounded-full border-2 border-background',
        colors.handle
      )"
    />

    <!-- Output Handle -->
    <Handle
      v-if="showOutputHandle"
      type="source"
      :position="Position.Right"
      :class="cn(
        'w-3 h-3 rounded-full border-2 border-background',
        colors.handle
      )"
    />
  </div>
</template>

<style scoped>
.vue-flow__handle {
  background-color: inherit;
}
</style>
