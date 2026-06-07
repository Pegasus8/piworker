<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Button, Input, Textarea, Label, Select, Switch, ScrollArea } from '@/components/ui'
import { X, Save, Trash2 } from 'lucide-vue-next'
import NodeDocumentation from './NodeDocumentation.vue'

const flowsStore = useFlowsStore()
const nodeTypesStore = useNodeTypesStore()

const localConfig = ref<Record<string, any>>({})

const selectedNode = computed(() => flowsStore.selectedNode)

const nodeTypeConfig = computed(() => {
  if (!selectedNode.value) return null
  return nodeTypesStore.getById(selectedNode.value.data.nodeType)
})

// Sync local config with selected node
watch(
  () => selectedNode.value?.data.config,
  (config) => {
    if (config) {
      localConfig.value = { ...config }
    }
  },
  { immediate: true, deep: true }
)

function updateField(key: string, value: any) {
  localConfig.value[key] = value
}

function handleSave() {
  if (selectedNode.value) {
    flowsStore.updateNodeConfig(selectedNode.value.id, { ...localConfig.value })
    flowsStore.selectNode(null)
  }
}

function handleCancel() {
  flowsStore.selectNode(null)
}

function handleDelete() {
  if (selectedNode.value) {
    flowsStore.removeNode(selectedNode.value.id)
  }
}
</script>

<template>
  <Transition
    enter-active-class="transition duration-200 ease-out"
    enter-from-class="translate-x-full"
    enter-to-class="translate-x-0"
    leave-active-class="transition duration-150 ease-in"
    leave-from-class="translate-x-0"
    leave-to-class="translate-x-full"
  >
    <div
      v-if="selectedNode && nodeTypeConfig"
      class="fixed right-0 top-0 z-40 h-full w-80 border-l bg-card shadow-lg"
    >
      <!-- Header -->
      <div class="flex items-center justify-between border-b p-4">
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-1">
            <h3 class="font-semibold truncate">{{ selectedNode.data.label }}</h3>
            <NodeDocumentation
              v-if="nodeTypeConfig.documentation"
              :title="nodeTypeConfig.name"
              :documentation="nodeTypeConfig.documentation"
            />
          </div>
          <p class="text-sm text-muted-foreground truncate">
            {{ nodeTypeConfig.description }}
          </p>
        </div>
        <Button variant="ghost" size="icon" class="flex-shrink-0" @click="handleCancel">
          <X class="h-4 w-4" />
        </Button>
      </div>

      <!-- Config Fields -->
      <ScrollArea class="h-[calc(100%-140px)] p-4">
        <div class="space-y-4">
          <div v-for="field in nodeTypeConfig.configSchema" :key="field.key">
            <Label :for="field.key" class="mb-2 block">
              {{ field.label }}
              <span v-if="field.required" class="text-destructive">*</span>
            </Label>

            <!-- Text Input -->
            <Input
              v-if="field.type === 'text'"
              :id="field.key"
              :model-value="localConfig[field.key] || ''"
              :placeholder="field.placeholder"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Number Input -->
            <Input
              v-else-if="field.type === 'number'"
              :id="field.key"
              type="number"
              :model-value="localConfig[field.key] || 0"
              :placeholder="field.placeholder"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Textarea -->
            <Textarea
              v-else-if="field.type === 'textarea' || field.type === 'cron'"
              :id="field.key"
              :model-value="localConfig[field.key] || ''"
              :placeholder="field.placeholder"
              :rows="field.type === 'textarea' ? 5 : 2"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Select -->
            <Select
              v-else-if="field.type === 'select'"
              :id="field.key"
              :model-value="localConfig[field.key] || ''"
              :options="field.options || []"
              :placeholder="field.placeholder || 'Select...'"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Boolean Switch -->
            <div
              v-else-if="field.type === 'boolean'"
              class="flex items-center justify-between"
            >
              <Switch
                :id="field.key"
                :model-value="!!localConfig[field.key]"
                @update:model-value="(v) => updateField(field.key, v)"
              />
            </div>
          </div>
        </div>
      </ScrollArea>

      <!-- Actions -->
      <div class="absolute bottom-0 left-0 right-0 flex gap-2 border-t bg-card p-4">
        <Button
          variant="destructive"
          size="sm"
          class="flex-shrink-0"
          @click="handleDelete"
        >
          <Trash2 class="mr-2 h-4 w-4" />
          Delete
        </Button>
        <div class="flex-1" />
        <Button variant="outline" size="sm" @click="handleCancel">
          Cancel
        </Button>
        <Button size="sm" @click="handleSave">
          <Save class="mr-2 h-4 w-4" />
          Save
        </Button>
      </div>
    </div>
  </Transition>
</template>
