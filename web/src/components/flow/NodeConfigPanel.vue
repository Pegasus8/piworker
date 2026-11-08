<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Button, Input, Label, Select, Switch, ScrollArea, Dialog } from '@/components/ui'
import { X, Save, Trash2 } from 'lucide-vue-next'
import ReferenceInput from './ReferenceInput.vue'
import KeyValueInput from './KeyValueInput.vue'
import JsonInput from './JsonInput.vue'
import { integerFromInput, setConfigValue } from '@/lib/configValues'
import { referenceMode, referenceOptions } from '@/lib/references'
import { useApi } from '@/composables/useApi'
import NodeDocumentation from './NodeDocumentation.vue'
import NodePlayground from './NodePlayground.vue'
import TestTool from './TestTool.vue'
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
const api = useApi()
const variableNames = ref<string[]>([])
const flowVariableNames = ref<string[]>([])
const secretNames = ref<string[]>([])
const referencesError = ref('')
let referencesRequest = 0
async function loadReferences() {
 const request = ++referencesRequest
 const result = await Promise.allSettled([api.getVariables(), api.getSecrets(), flowsStore.currentFlow?.id ? api.getVariables(flowsStore.currentFlow.id) : Promise.resolve({})])
 if (request !== referencesRequest) return
 variableNames.value = result[0].status === 'fulfilled' ? Object.keys(result[0].value || {}) : []
 flowVariableNames.value = result[2].status === 'fulfilled' ? Object.keys(result[2].value || {}) : []
 secretNames.value = result[1].status === 'fulfilled' ? result[1].value : []
 referencesError.value = result.some(r => r.status === 'rejected') ? 'Some references could not be loaded. You can still edit the field.' : ''
}
// JSON fields whose text does not parse: their value is not applied, so
// "Apply changes" waits until they are fixed.
const invalidFields = ref<string[]>([])
function setInvalid(key: string, invalid: boolean) {
  invalidFields.value = invalid ? [...new Set([...invalidFields.value, key])] : invalidFields.value.filter(k => k !== key)
}
watch(() => selectedNode.value?.id, id => {
 invalidFields.value = []
 referencesRequest++; variableNames.value = []; flowVariableNames.value = []; secretNames.value = []; referencesError.value = ''
 if (id) void loadReferences()
})
function fieldReferences(key: string) {
 const declared = (scope: string) => flowsStore.nodes.filter(n => n.data.nodeType === 'set-var' && (n.data.config.scope || 'global') === scope).map(n => n.data.config.key).filter(k => typeof k === 'string')
 return referenceOptions(referenceMode(selectedNode.value?.data.nodeType || '',key), [...new Set([...variableNames.value,...declared('global')])].sort(), secretNames.value, [...new Set([...flowVariableNames.value,...declared('flow')])].sort(), localConfig.value.scope || 'global')
}
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

// undefined removes the setting, so the node uses its default.
function updateField(key: string, value: any) {
  setConfigValue(localConfig.value, key, value)
}

function handleSave() {
  if (invalidFields.value.length) return
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
          <p v-if="referencesError" role="alert" class="text-sm text-destructive">{{ referencesError }} <Button size="sm" variant="outline" @click="loadReferences">Retry references</Button></p>
          <!-- Keyed per node: field editors with their own state (JSON text and
               its error) start fresh for each node. -->
          <div v-for="field in nodeTypeConfig.configSchema" :key="`${selectedNode.id}:${field.key}`">
            <Label :for="field.key" class="mb-2 block">
              {{ field.label }}
              <span v-if="field.required" aria-hidden="true" class="text-destructive">*</span>
            </Label>

            <ReferenceInput
              v-if="['text','textarea','cron'].includes(field.type)"
              :id="field.key" :required="field.required"
              :model-value="String(localConfig[field.key] ?? '')"
              :placeholder="field.placeholder"
              :multiline="field.type === 'textarea' || field.type === 'cron' || ['expression','condition','template'].includes(field.key) || (selectedNode.data.nodeType === 'set-var' && field.key === 'value')"
              :mode="referenceMode(selectedNode.data.nodeType, field.key)"
              :options="fieldReferences(field.key)"
              @update:model-value="v => updateField(field.key, v)"
            />

            <!-- Number Input -->
            <Input
              v-else-if="field.type === 'number'"
              :id="field.key"
              :aria-required="field.required"
              type="number"
              :model-value="localConfig[field.key] ?? ''"
              :placeholder="field.default !== undefined ? `Default: ${field.default}` : ''"
              @update:model-value="(v) => updateField(field.key, v === '' ? undefined : v)"
            />

            <!-- Integer Input: stores whole numbers -->
            <Input
              v-else-if="field.type === 'integer'"
              :id="field.key"
              :aria-required="field.required"
              type="number"
              step="1"
              inputmode="numeric"
              :model-value="localConfig[field.key] ?? ''"
              :placeholder="field.default !== undefined ? `Default: ${field.default}` : ''"
              @update:model-value="(v) => updateField(field.key, integerFromInput(String(v)))"
            />

            <!-- JSON object (e.g. a tool's input schema) -->
            <JsonInput
              v-else-if="field.type === 'json'"
              :id="field.key"
              :label="field.label"
              :required="field.required"
              :model-value="localConfig[field.key]"
              @update:model-value="(v) => updateField(field.key, v)"
              @update:invalid="(bad) => setInvalid(field.key, bad)"
            />

            <!-- Name/value pairs (object settings such as HTTP headers) -->
            <KeyValueInput
              v-else-if="field.type === 'keyvalue'"
              :id="field.key"
              :label="field.label"
              :model-value="localConfig[field.key]"
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

          <!-- Agent tools: call the deployed tool (REST twin of an MCP call). -->
          <TestTool
            v-if="selectedNode.data.nodeType === 'trigger-tool'"
            :key="`test-tool:${selectedNode.id}`"
            :flow-id="flowsStore.currentFlow?.id || undefined"
            :node-id="selectedNode.id"
            :schema="selectedNode.data.config?.inputSchema"
            :running="!!flowsStore.currentFlow?.running"
            :dirty="flowsStore.isDirty"
          />
        </section>
        <NodePlayground :flow-id="flowsStore.currentFlow?.id || undefined" v-if="isProcessing" :key="selectedNode.id" :node-type="selectedNode.data.nodeType" :config="localConfig" :draft="playgroundDrafts[selectedNode.id]" @draft="playgroundDrafts[selectedNode.id] = $event" />
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
        <p v-if="invalidFields.length" role="status" class="col-span-2 text-xs text-destructive sm:self-center">
          Fix the JSON in {{ invalidFields.length === 1 ? 'one field' : `${invalidFields.length} fields` }} to apply changes.
        </p>
        <Button size="sm" class="col-span-2" :disabled="invalidFields.length > 0" @click="handleSave">
          <Save class="mr-2 h-4 w-4" />
          Apply changes
        </Button>
      </div>
    </div>
    <ConfirmDialog ref="confirmation" />
  </Dialog>
</template>
