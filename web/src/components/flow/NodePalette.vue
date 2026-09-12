<script setup lang="ts">
import { ref, computed } from 'vue'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Input, ScrollArea, Badge } from '@/components/ui'
import { cn } from '@/lib/utils'
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
  HelpCircle,
  Search,
  ChevronDown,
  ChevronRight
} from 'lucide-vue-next'
import type { NodeCategory } from '@/types'

const emit = defineEmits<{ add: [nodeType: string] }>()
const nodeTypesStore = useNodeTypesStore()

const searchQuery = ref('')

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

const getIcon = (iconName: string) => iconComponents[iconName] || HelpCircle

const expandedCategories = ref<Set<NodeCategory>>(new Set(['trigger', 'process', 'action']))

const categories: { key: NodeCategory; label: string; color: string }[] = [
  { key: 'trigger', label: 'Triggers', color: 'bg-amber-500' },
  { key: 'process', label: 'Processing', color: 'bg-blue-500' },
  { key: 'action', label: 'Actions', color: 'bg-purple-500' }
]

const filteredNodeTypes = computed(() => {
  const query = searchQuery.value.toLowerCase()
  if (!query) return nodeTypesStore.nodeTypes

  return nodeTypesStore.nodeTypes.filter(
    node =>
      node.name.toLowerCase().includes(query) ||
      node.description.toLowerCase().includes(query)
  )
})

const getNodesByCategory = (category: NodeCategory) => {
  return filteredNodeTypes.value.filter(n => n.category === category)
}

function toggleCategory(category: NodeCategory) {
  if (expandedCategories.value.has(category)) {
    expandedCategories.value.delete(category)
  } else {
    expandedCategories.value.add(category)
  }
}

function onDragStart(event: DragEvent, nodeTypeId: string) {
  if (event.dataTransfer) {
    event.dataTransfer.setData('application/vueflow-nodetype', nodeTypeId)
    event.dataTransfer.effectAllowed = 'move'
  }
}
</script>

<template>
  <div class="flex h-full flex-col border-r bg-card">
    <!-- Header -->
    <div class="border-b p-4">
      <h2 class="mb-3 text-lg font-semibold">Nodes</h2>
      <div class="relative">
        <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          v-model="searchQuery"
          aria-label="Search nodes" placeholder="Search nodes..."
          class="pl-9"
        />
      </div>
    </div>

    <p class="px-4 py-2 text-xs text-muted-foreground">Tap to add, or drag onto the canvas.</p>
    <p v-if="nodeTypesStore.loading" role="status" class="p-4 text-sm">Loading nodes…</p>
    <div v-else-if="nodeTypesStore.error" role="alert" class="p-4 text-sm text-destructive">Could not load nodes. <button class="underline" @click="nodeTypesStore.fetchNodeTypes()">Retry</button></div>
    <p v-else-if="!filteredNodeTypes.length" class="p-4 text-sm text-muted-foreground">No matching nodes. Try another search.</p>
    <!-- Node List -->
    <ScrollArea class="flex-1 p-2">
      <div v-for="category in categories" :key="category.key" class="mb-2">
        <!-- Category Header -->
        <button
          class="flex w-full items-center gap-2 rounded-md px-2 py-2 text-sm font-medium hover:bg-accent"
          @click="toggleCategory(category.key)"
        >
          <component
            :is="expandedCategories.has(category.key) ? ChevronDown : ChevronRight"
            class="h-4 w-4"
          />
          <div :class="cn('h-2 w-2 rounded-full', category.color)" />
          <span>{{ category.label }}</span>
          <Badge variant="secondary" class="ml-auto">
            {{ getNodesByCategory(category.key).length }}
          </Badge>
        </button>

        <!-- Category Nodes -->
        <Transition
          enter-active-class="transition duration-200 ease-out"
          enter-from-class="opacity-0 -translate-y-1"
          enter-to-class="opacity-100 translate-y-0"
          leave-active-class="transition duration-150 ease-in"
          leave-from-class="opacity-100 translate-y-0"
          leave-to-class="opacity-0 -translate-y-1"
        >
          <div
            v-if="expandedCategories.has(category.key)"
            class="ml-4 mt-1 space-y-1"
          >
            <button
              type="button"
              :aria-label="`Add ${nodeType.name}`"
              @click="emit('add', nodeType.id)"
              v-for="nodeType in getNodesByCategory(category.key)"
              :key="nodeType.id"
              draggable="true"
              :class="cn(
                'group flex w-full text-left cursor-grab items-center gap-3 rounded-md border border-transparent p-2',
                'hover:border-border hover:bg-accent',
                'active:cursor-grabbing'
              )"
              @dragstart="(e) => onDragStart(e, nodeType.id)"
            >
              <div
                :class="cn(
                  'flex h-8 w-8 items-center justify-center rounded-md',
                  category.color
                )"
              >
                <component :is="getIcon(nodeType.icon)" class="h-4 w-4 text-white" />
              </div>
              <div class="flex-1 min-w-0">
                <div class="text-sm font-medium truncate">{{ nodeType.name }}</div>
                <div class="text-xs text-muted-foreground truncate">
                  {{ nodeType.description }}
                </div>
              </div>
            </button>
          </div>
        </Transition>
      </div>
    </ScrollArea>
  </div>
</template>
