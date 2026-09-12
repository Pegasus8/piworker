<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Button, Input, Textarea, Label, Select, Switch, ScrollArea, Dialog } from '@/components/ui'
import { X, Save, Trash2 } from 'lucide-vue-next'
import NodeDocumentation from './NodeDocumentation.vue'
import NodePlayground from './NodePlayground.vue'
import type { NodeTestDraft } from '@/types'

const flowsStore = useFlowsStore()
const nodeTypesStore = useNodeTypesStore()
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { onBeforeRouteLeave } from 'vue-router'
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const configDirty = computed(() => JSON.stringify(localConfig.value) !== JSON.stringify(selectedNode.value?.data.config || {}))
const examples: Record<string, string> = {
  expression: 'payload.temperature * 1.8 + 32',
  condition: 'payload.temperature > 25',
  template: 'Temperature: {{payload.temperature}}°C'
}
const playgroundDrafts = ref<Record<string, NodeTestDraft>>({})

const localConfig = ref<Record<string, any>>({})

const selectedNode = computed(() => flowsStore.selectedNode)

// Only processing nodes can be test-run (action/trigger nodes have side effects).
const isProcessing = computed(() => selectedNode.value?.data.category === 'process')
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

async function handleCancel() {
  if (configDirty.value && !await confirmation.value?.ask('Discard node changes?', 'Your configuration changes have not been applied.', 'Discard changes', 'Keep editing')) return
  flowsStore.selectNode(null)
}
onBeforeRouteLeave(async to => to.name === 'login' || !selectedNode.value || !configDirty.value || !!await confirmation.value?.ask('Discard node changes?', 'Your configuration changes have not been applied.', 'Discard changes', 'Keep editing'))

async function handleDelete() {
  if (selectedNode.value && await confirmation.value?.ask('Delete node?', 'Remove this node and its connections? Save the flow to persist this change.', 'Delete node')) {
    flowsStore.removeNode(selectedNode.value.id)
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (selectedNode.value && configDirty.value) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onUnmounted(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <Dialog :open="!!selectedNode && !!nodeTypeConfig" title="Node configuration" :class="isProcessing ? 'max-w-6xl' : 'max-w-lg'" @update:open="handleCancel">
    <div v-if="selectedNode && nodeTypeConfig">
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
          <p class="text-sm text-muted-foreground">
            {{ nodeTypeConfig.description }}
          </p>
        </div>
        <Button variant="ghost" size="icon" class="flex-shrink-0" aria-label="Close configuration" @click="handleCancel">
          <X class="h-4 w-4" />
        </Button>
      </div>

      <!-- Config Fields -->
      <ScrollArea class="max-h-[65dvh] p-4">
        <div class="grid min-w-0 gap-6" :class="isProcessing ? 'lg:grid-cols-[minmax(0,0.8fr)_minmax(0,2fr)]' : ''">
        <section class="min-w-0 space-y-4 lg:sticky lg:top-0 lg:self-start">
          <div><h3 class="font-semibold">Configuration</h3><p class="mt-1 text-sm text-muted-foreground">Apply changes to update the draft. Save the flow to keep them.</p></div>
          <div v-for="field in nodeTypeConfig.configSchema" :key="field.key">
            <Label :for="field.key" class="mb-2 block">
              {{ field.label }}
              <span v-if="field.required" aria-hidden="true" class="text-destructive">*</span>
            </Label>

            <!-- Text Input -->
            <Input
              v-if="field.type === 'text' && !['expression', 'condition', 'template'].includes(field.key)"
              :id="field.key"
              :aria-required="field.required"
              :model-value="localConfig[field.key] ?? ''"
              :placeholder="field.placeholder"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Number Input -->
            <Input
              v-else-if="field.type === 'number'"
              :id="field.key"
              :aria-required="field.required"
              type="number"
              :model-value="localConfig[field.key] ?? 0"
              :placeholder="field.placeholder"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Textarea -->
            <Textarea
              v-else-if="field.type === 'textarea' || field.type === 'cron' || ['expression', 'condition', 'template'].includes(field.key)"
              :id="field.key"
              :aria-required="field.required"
              :model-value="localConfig[field.key] ?? ''"
              :placeholder="field.placeholder"
              :rows="field.type === 'cron' ? 2 : 5"
              spellcheck="false"
              @update:model-value="(v) => updateField(field.key, v)"
            />

            <!-- Select -->
            <Select
              v-else-if="field.type === 'select'"
              :id="field.key"
              :aria-required="field.required"
              :model-value="localConfig[field.key] ?? ''"
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
              :aria-required="field.required"
                :model-value="!!localConfig[field.key]"
                @update:model-value="(v) => updateField(field.key, v)"
              />
            </div>
            <p v-if="field.placeholder" class="mt-2 break-words text-xs text-muted-foreground">{{ field.placeholder }}</p>
            <div v-if="examples[field.key]" class="mt-2 space-y-1">
              <Button variant="ghost" size="sm" :aria-label="`Use example for ${field.label}`" @click="updateField(field.key, examples[field.key])">Use temperature example</Button>
              <p class="text-xs text-muted-foreground">Replaces this field. Use the sample payload in the playground to try it.</p>
            </div>
          </div>

        </section>
        <NodePlayground v-if="isProcessing" :key="selectedNode.id" :node-type="selectedNode.data.nodeType" :config="localConfig" :draft="playgroundDrafts[selectedNode.id]" @draft="playgroundDrafts[selectedNode.id] = $event" />
        </div>
      </ScrollArea>

      <!-- Actions -->
      <div class="grid grid-cols-2 gap-2 border-t bg-card pt-4 sm:flex">
        <Button
          variant="destructive"
          size="sm"
          class="flex-shrink-0"
          @click="handleDelete"
        >
          <Trash2 class="mr-2 h-4 w-4" />
          Delete
        </Button>
        <div class="hidden flex-1 sm:block" />
        <Button variant="outline" size="sm" @click="handleCancel">
          Cancel
        </Button>
        <Button size="sm" class="col-span-2" @click="handleSave">
          <Save class="mr-2 h-4 w-4" />
          Apply changes
        </Button>
      </div>
    </div>
    <ConfirmDialog ref="confirmation" />
  </Dialog>
</template>
