<script setup lang="ts">
import { ports, validConnection } from '@/lib/ports'
import { ref, computed, watch } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useNodeTypesStore } from '@/stores/nodeTypes'
import { Button, Dialog, Select, Label } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
const store = useFlowsStore()
const types = useNodeTypesStore()
const source = ref('')
const target = ref('')
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const options = (direction: 'inputs' | 'outputs') => store.nodes.flatMap((node, index) => {
  const type = types.getById(node.data.nodeType)
  return type?.[direction]?.length ? [{ value: node.id, label: `${node.data.label} (${index + 1})` }] : []
})
const sources = computed(() => options('outputs'))
const targets = computed(() => options('inputs').filter(node => node.value !== source.value))
const sourcePort = ref('')
const targetPort = ref('')
function portOptions(id: string, direction: 'inputs' | 'outputs') {
  const node = store.nodes.find(n => n.id === id)
  return ports(node ? types.getById(node.data.nodeType)?.[direction] : []).map(p => ({ value: p.id, label: p.name }))
}
const sourcePorts = computed(() => portOptions(source.value, 'outputs'))
const targetPorts = computed(() => portOptions(target.value, 'inputs'))
watch(sourcePorts, options => { sourcePort.value = options.length === 1 ? options[0].value : '' })
watch(targetPorts, options => { targetPort.value = options.length === 1 ? options[0].value : '' })
const connection = computed(() => ({ source: source.value, target: target.value, sourceHandle: sourcePort.value, targetHandle: targetPort.value }))
const canConnect = computed(() => validConnection(connection.value, store.nodes, store.edges, types.getById))
function connect() {
  if (!canConnect.value) return
  store.addEdge({ id: `edge_${crypto.randomUUID()}`, ...connection.value, animated: true })
}
function name(id: string) { return store.nodes.find(n => n.id === id)?.data.label || id }
async function remove(id: string) {
  if (await confirmation.value?.ask('Remove connection?', 'The nodes will remain. Save the flow to persist this change.', 'Remove connection')) store.removeEdge(id)
}
</script>
<template>
  <Dialog :open="props.open" title="Connections" @update:open="emit('update:open', $event)">
    <h2 class="text-lg font-semibold">Connections</h2>
    <p class="mt-2 text-sm text-muted-foreground">Choose where messages start and where they go. Changes are saved with the flow.</p>
    <div class="my-4 space-y-3">
      <div><Label for="connection-source">From node</Label><Select id="connection-source" v-model="source" :options="sources" placeholder="Choose a source" /></div>
      <div><Label for="source-port">From port</Label><Select id="source-port" v-model="sourcePort" :options="sourcePorts" placeholder="Choose an output" /></div>
      <div><Label for="connection-target">To node</Label><Select id="connection-target" v-model="target" :options="targets" placeholder="Choose a destination" /></div>
      <div><Label for="target-port">To port</Label><Select id="target-port" v-model="targetPort" :options="targetPorts" placeholder="Choose an input" /></div>
      <Button :disabled="!canConnect" @click="connect">Connect nodes</Button>
    </div>
    <p v-if="!store.edges.length" class="text-sm text-muted-foreground">No connections yet.</p>
    <ul class="max-h-60 overflow-auto divide-y">
      <li v-for="edge in store.edges" :key="edge.id" class="flex items-center justify-between gap-2 py-2 text-sm">
        <span>{{ name(edge.source) }} → {{ name(edge.target) }}<small class="block text-muted-foreground">{{ edge.sourceHandle }} → {{ edge.targetHandle }}</small><small v-if="store.invalidConnections.some(e => e.id === edge.id)" class="block text-destructive">Choose valid ports: remove this connection and reconnect it.</small></span>
        <Button variant="ghost" size="sm" :aria-label="`Remove ${name(edge.source)} to ${name(edge.target)}`" @click="remove(edge.id)">Remove</Button>
      </li>
    </ul>
    <div class="mt-4 flex justify-end"><Button variant="outline" @click="emit('update:open', false)">Done</Button></div>
    <ConfirmDialog ref="confirmation" />
  </Dialog>
</template>
