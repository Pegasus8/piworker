<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Button, Input, Label, Select, Switch } from '@/components/ui'
import { X, Trash2, CheckCircle2 } from 'lucide-vue-next'
import DurationInput from './DurationInput.vue'
import { isDurationUnit } from '@/lib/duration'
import { messages as m } from '@/i18n'
import { nodeIcon } from '@/lib/nodeIcons'
import ReferenceInput from './ReferenceInput.vue'
import KeyValueInput from './KeyValueInput.vue'
import JsonInput from './JsonInput.vue'
import { integerFromInput, setConfigValue } from '@/lib/configValues'
import { referenceMode, referenceOptions } from '@/lib/references'
import { useApi } from '@/composables/useApi'
import NodeDocumentation from './NodeDocumentation.vue'
import NodePlayground from './NodePlayground.vue'
import TestTool from './TestTool.vue'
import type { ConfigField, NodeTestDraft } from '@/types'
import { useZigbeeStore } from '@/stores/zigbee'
import { fieldHint, findNetwork, suggestions, type SuggestionContext } from '@/lib/zigbee'

const flowsStore = useFlowsStore()
const nodeTypesStore = useNodeTypesStore()
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
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
 secretNames.value = result[1].status === 'fulfilled' ? result[1].value.secrets.map(s => s.name) : []
 referencesError.value = result.some(r => r.status === 'rejected') ? m.value.editor.referencesError : ''
}
// JSON fields whose text does not parse: their value is not applied until
// they are fixed.
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

// Suggestions of settings with a source (broker connections, Zigbee devices
// and their properties): read when the panel opens, again when it names a
// connection the networks do not know yet. A failure leaves the fields free text.
const zigbeeStore = useZigbeeStore()
const suggestionsNotice = ref('')
const suggestFields = computed(() => (nodeTypeConfig.value?.configSchema ?? []).filter(f => f.suggest))
const triedConnections = new Set<string>()
async function loadSuggestions(force = false) {
 const fields = suggestFields.value
 if (!fields.length) return
 const ok = await zigbeeStore.loadSuggestions({
  connections: fields.some(f => f.suggest === 'connections'),
  networks: fields.some(f => f.suggest !== 'connections'),
  force
 })
 suggestionsNotice.value = ok ? '' : m.value.editor.suggestionsError
}
// Also when the node types arrive after the panel opened.
watch(() => [selectedNode.value?.id, suggestFields.value.length] as const, ([id]) => {
 suggestionsNotice.value = ''
 triedConnections.clear()
 if (id) void loadSuggestions(true)
}, { immediate: true })
watch(() => localConfig.value.connection, connection => {
 if (typeof connection !== 'string' || !zigbeeStore.networksLoaded || triedConnections.has(connection)) return
 if (findNetwork(zigbeeStore.networks, connection) || !zigbeeStore.connections.some(c => c.name === connection)) return
 triedConnections.add(connection)
 void loadSuggestions(true)
})
const suggestionContext = computed((): SuggestionContext => ({ connections: zigbeeStore.connections, networks: zigbeeStore.networks, config: localConfig.value }))
function fieldSuggestions(field: ConfigField): string[] {
 return field.suggest ? suggestions(field.suggest, suggestionContext.value) : []
}
function suggestHint(field: ConfigField): string {
 return field.suggest ? fieldHint(field.suggest, suggestionContext.value) : ''
}

// undefined removes the setting, so the node uses its default.
function updateField(key: string, value: any) {
  setConfigValue(localConfig.value, key, value)
}

// Settings apply to the flow as they are typed (one undo step per node and
// burst of edits); a JSON field that does not parse waits until it does.
watch(localConfig, config => {
  const node = selectedNode.value
  if (!node || invalidFields.value.length) return
  if (JSON.stringify(config) === JSON.stringify(node.data.config || {})) return
  flowsStore.updateNodeConfig(node.id, { ...config })
}, { deep: true })
watch(invalidFields, fields => {
  const node = selectedNode.value
  if (!node || fields.length || JSON.stringify(localConfig.value) === JSON.stringify(node.data.config || {})) return
  flowsStore.updateNodeConfig(node.id, { ...localConfig.value })
})

function close() {
  flowsStore.selectNode(null)
}

async function handleDelete() {
  if (selectedNode.value && await confirmation.value?.ask(m.value.editor.deleteNodeTitle, m.value.editor.deleteNodeBody, m.value.editor.deleteNode)) {
    flowsStore.removeNode(selectedNode.value.id)
  }
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && !event.defaultPrevented && !(event.target as HTMLElement | null)?.closest('dialog')) close()
}
const icon = computed(() => nodeIcon(selectedNode.value?.data.icon || ''))
const duration = (field: ConfigField) => (field.type === 'number' || field.type === 'integer') && isDurationUnit(field.unit)
</script>

<template>
  <aside v-if="selectedNode && nodeTypeConfig" :aria-label="m.editor.nodeSettings"
    class="fixed inset-0 z-40 flex flex-col bg-card md:static md:z-auto md:w-[400px] md:shrink-0 md:border-l"
    :class="isProcessing ? 'xl:w-[520px]' : ''" @keydown="onKeydown">
    <div class="flex items-start gap-3 border-b p-4">
      <component :is="icon" class="mt-0.5 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-1">
          <h2 class="truncate font-semibold">{{ selectedNode.data.label }}</h2>
          <NodeDocumentation v-if="nodeTypeConfig.documentation" :title="nodeTypeConfig.name" :documentation="nodeTypeConfig.documentation" />
        </div>
        <p class="text-sm text-muted-foreground">{{ nodeTypeConfig.description }}</p>
      </div>
      <Button variant="ghost" size="icon" class="shrink-0" :aria-label="m.editor.closeSettings" :title="m.editor.closeSettings" @click="close">
        <X class="h-4 w-4" />
      </Button>
    </div>

    <div class="min-h-0 flex-1 space-y-6 overflow-y-auto p-4">
      <section class="min-w-0 space-y-4">
        <p class="flex items-center gap-2 text-xs text-muted-foreground"><CheckCircle2 class="h-3.5 w-3.5 text-primary" aria-hidden="true" />{{ m.editor.autoApply }}</p>
        <p v-if="referencesError" role="alert" class="text-sm text-destructive">{{ referencesError }} <Button size="sm" variant="outline" @click="loadReferences">{{ m.editor.retryReferences }}</Button></p>
        <p v-if="suggestionsNotice" role="status" class="text-sm text-muted-foreground">{{ suggestionsNotice }} <Button size="sm" variant="outline" @click="loadSuggestions(true)">{{ m.editor.retrySuggestions }}</Button></p>
        <p v-if="!nodeTypeConfig.configSchema.length" class="text-sm text-muted-foreground">{{ m.editor.noSettings }}</p>
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
            :list="field.suggest ? `${field.key}-suggestions` : undefined"
            :describedby="suggestHint(field) ? `${field.key}-hint` : undefined"
            @update:model-value="v => updateField(field.key, v)"
          />
          <!-- Suggestions (free text stays allowed) and what is known of the choice. -->
          <datalist v-if="field.suggest" :id="`${field.key}-suggestions`">
            <option v-for="value in fieldSuggestions(field)" :key="value" :value="value" />
          </datalist>
          <p v-if="suggestHint(field)" :id="`${field.key}-hint`" class="mt-1 break-words text-xs text-primary">{{ suggestHint(field) }}</p>

          <!-- Time settings: typed in a friendly unit, stored in the node's. -->
          <DurationInput
            v-else-if="duration(field)"
            :id="field.key"
            :required="field.required"
            :model-value="localConfig[field.key]"
            :base-unit="field.unit!"
            :integer="field.type === 'integer'"
            :placeholder="field.default !== undefined ? m.editor.defaultValue(String(field.default)) : ''"
            @update:model-value="v => updateField(field.key, v)"
          />

          <!-- Number Input -->
          <Input
            v-else-if="field.type === 'number'"
            :id="field.key"
            :aria-required="field.required"
            type="number"
            :model-value="localConfig[field.key] ?? ''"
            :placeholder="field.default !== undefined ? m.editor.defaultValue(String(field.default)) : ''"
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
            :placeholder="field.default !== undefined ? m.editor.defaultValue(String(field.default)) : ''"
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
            :placeholder="field.placeholder || m.editor.selectPlaceholder"
            @update:model-value="(v) => updateField(field.key, v)"
          />

          <!-- Boolean Switch -->
          <div v-else-if="field.type === 'boolean'" class="flex items-center justify-between">
            <Switch :id="field.key" :aria-required="field.required" :model-value="!!localConfig[field.key]" @update:model-value="(v) => updateField(field.key, v)" />
          </div>
          <p v-if="field.placeholder" class="mt-2 break-words text-xs text-muted-foreground">{{ field.placeholder }}</p>
          <div v-if="examples[field.key]" class="mt-2 space-y-1">
            <Button variant="ghost" size="sm" :aria-label="m.editor.useExampleFor(field.label)" @click="updateField(field.key, examples[field.key])">{{ m.editor.useExample }}</Button>
            <p class="text-xs text-muted-foreground">{{ m.editor.useExampleHint }}</p>
          </div>
        </div>
        <p v-if="invalidFields.length" role="status" class="text-xs text-destructive">{{ m.editor.fixJson(invalidFields.length) }}</p>

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
      <NodePlayground v-if="isProcessing" :key="selectedNode.id" :flow-id="flowsStore.currentFlow?.id || undefined" :node-type="selectedNode.data.nodeType" :config="localConfig" :draft="playgroundDrafts[selectedNode.id]" @draft="playgroundDrafts[selectedNode.id] = $event" />
    </div>

    <div class="flex items-center gap-2 border-t p-3">
      <Button variant="ghost" size="sm" class="text-destructive hover:bg-destructive/10 hover:text-destructive" @click="handleDelete">
        <Trash2 class="mr-2 h-4 w-4" />
        {{ m.editor.deleteNode }}
      </Button>
      <div class="flex-1" />
      <Button size="sm" variant="outline" @click="close">{{ m.common.done }}</Button>
    </div>
    <ConfirmDialog ref="confirmation" />
  </aside>
</template>
