<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Button, Input, Label, Dialog, Select } from '@/components/ui'
import { LayoutTemplate, Cable } from 'lucide-vue-next'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useApi } from '@/composables/useApi'
import { apiErrorMessage } from '@/stores/agent'
import { useZigbeeStore } from '@/stores/zigbee'
import { numberFromInput } from '@/lib/configValues'
import { DEFAULT_LOCATION, EXAMPLES, buildExampleFlow, missingParams, type ExampleParam } from '@/lib/examples'
import { deviceHint, devicesFor, findDevice } from '@/lib/zigbee'

const props = defineProps<{ open: boolean }>()
// manage-connections: the user wants to add a broker connection first.
const emit = defineEmits<{ 'update:open': [boolean]; 'manage-connections': [] }>()

const router = useRouter()
const api = useApi()
const store = useZigbeeStore()
const { feedback, pending, run } = useFeedback()

const exampleId = ref(EXAMPLES[0].id)
const example = computed(() => EXAMPLES.find(e => e.id === exampleId.value) ?? EXAMPLES[0])
const connection = ref('')
const sensor = ref('')
const plug = ref('')
const latitude = ref<number | undefined>(DEFAULT_LOCATION.latitude)
const longitude = ref<number | undefined>(DEFAULT_LOCATION.longitude)
const loading = ref(false)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  // The connection list is required; discovery only suggests device names.
  try {
    await store.fetchConnections()
  } catch (e) {
    loadError.value = apiErrorMessage(e, 'Could not load the broker connections. Try again.')
  } finally {
    loading.value = false
  }
  try { await store.fetchNetworks() } catch { /* names stay free text */ }
}

watch(() => props.open, open => {
  if (!open) return
  feedback.value = null
  void load()
}, { immediate: true })

// Keep the chosen connection valid: the first one by default.
watch(() => store.connections.map(c => c.name), names => {
  if (!names.includes(connection.value)) connection.value = names[0] ?? ''
}, { immediate: true })

const connectionOptions = computed(() => store.connections.map(c => ({ label: `${c.name} (${c.broker})`, value: c.name })))
const deviceNames = computed(() => devicesFor(store.networks, connection.value).filter(d => d.type !== 'Coordinator').map(d => d.friendlyName))
const params = computed(() => ({ connection: connection.value, sensor: sensor.value, plug: plug.value, latitude: latitude.value, longitude: longitude.value }))
const missing = computed(() => missingParams(example.value, params.value))
const noConnections = computed(() => store.connectionsLoaded && !store.connections.length)

const needLabels: Record<ExampleParam, string> = {
  connection: 'a broker connection',
  sensor: 'the sensor',
  plug: 'the plug',
  location: 'a valid latitude and longitude'
}
const missingText = computed(() => missing.value.length ? `Still needed: ${missing.value.map(m => needLabels[m]).join(', ')}.` : '')

function hint(name: string) {
  const device = findDevice(store.networks, connection.value, name.trim())
  return device ? deviceHint(device) : ''
}

async function create(close: () => void) {
  if (missing.value.length) return
  const flow = buildExampleFlow(example.value, params.value)
  let id = ''
  const ok = await run(async () => {
    const created = await api.importFlow(flow)
    id = created.id
  }, `Created “${flow.name}”.`, e => apiErrorMessage(e, 'Could not create the flow. Try again.'))
  if (ok && id) {
    close()
    await router.push(`/editor/${id}`)
  }
}
</script>

<template>
  <Dialog title="Example flows" class="max-w-2xl" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <LayoutTemplate class="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div>
            <h2 class="text-lg font-semibold">Example flows</h2>
            <p class="text-sm text-muted-foreground">
              Start from a ready-made Zigbee automation. The new flow is not deployed: review it in the editor, then deploy it.
            </p>
          </div>
        </div>

        <OperationFeedback :message="feedback" @dismiss="feedback = null" />
        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>

        <fieldset class="space-y-2">
          <legend class="mb-2 text-sm font-medium">Example</legend>
          <label
            v-for="item in EXAMPLES"
            :key="item.id"
            class="flex cursor-pointer items-start gap-3 rounded-lg border p-3 text-sm hover:bg-accent/50"
            :class="exampleId === item.id ? 'border-primary bg-primary/5' : ''"
          >
            <input v-model="exampleId" type="radio" name="example" :value="item.id" class="mt-1" />
            <span>
              <span class="block font-medium">{{ item.title }}</span>
              <span class="block text-muted-foreground">{{ item.description }}</span>
            </span>
          </label>
        </fieldset>

        <!-- No connection: the examples need one. -->
        <div v-if="noConnections" class="space-y-2 rounded-lg border border-dashed p-4 text-sm" role="status">
          <p>Zigbee examples need a broker connection to Zigbee2MQTT. Create one first, then come back here.</p>
          <Button size="sm" variant="outline" @click="emit('manage-connections')"><Cable class="mr-2 h-4 w-4" />Add a broker connection</Button>
        </div>

        <div v-else class="grid gap-3 sm:grid-cols-2">
          <div class="sm:col-span-2">
            <Label for="example-connection" class="text-xs">Broker connection</Label>
            <Select id="example-connection" v-model="connection" class="mt-1" :disabled="pending || loading" :options="connectionOptions" placeholder="Select a connection" />
          </div>
          <div v-if="example.needs.includes('sensor')">
            <Label for="example-sensor" class="text-xs">Sensor (friendly name)</Label>
            <Input id="example-sensor" v-model="sensor" list="example-devices" :disabled="pending" placeholder="bedroom_climate" class="mt-1" autocomplete="off"
              :aria-describedby="hint(sensor) ? 'example-sensor-hint' : undefined" />
            <p v-if="hint(sensor)" id="example-sensor-hint" class="mt-1 text-xs text-primary">{{ hint(sensor) }}</p>
          </div>
          <div v-if="example.needs.includes('plug')">
            <Label for="example-plug" class="text-xs">Plug (friendly name)</Label>
            <Input id="example-plug" v-model="plug" list="example-devices" :disabled="pending" placeholder="washer_plug" class="mt-1" autocomplete="off"
              :aria-describedby="hint(plug) ? 'example-plug-hint' : undefined" />
            <p v-if="hint(plug)" id="example-plug-hint" class="mt-1 text-xs text-primary">{{ hint(plug) }}</p>
          </div>
          <datalist id="example-devices">
            <option v-for="name in deviceNames" :key="name" :value="name" />
          </datalist>
          <template v-if="example.needs.includes('location')">
            <div>
              <Label for="example-latitude" class="text-xs">Latitude (degrees, south negative)</Label>
              <Input id="example-latitude" type="number" step="any" :model-value="latitude ?? ''" :disabled="pending" class="mt-1"
                @update:model-value="v => latitude = numberFromInput(String(v))" />
            </div>
            <div>
              <Label for="example-longitude" class="text-xs">Longitude (degrees, west negative)</Label>
              <Input id="example-longitude" type="number" step="any" :model-value="longitude ?? ''" :disabled="pending" class="mt-1"
                @update:model-value="v => longitude = numberFromInput(String(v))" />
            </div>
          </template>
          <p v-if="(example.needs.includes('sensor') || example.needs.includes('plug')) && !deviceNames.length" class="text-xs text-muted-foreground sm:col-span-2">
            No devices discovered on this connection yet; type the Zigbee2MQTT friendly name.
          </p>
        </div>

        <p v-if="!noConnections && missingText" class="text-xs text-muted-foreground" aria-live="polite">{{ missingText }}</p>

        <div class="flex flex-wrap justify-end gap-2 border-t pt-3">
          <Button variant="outline" @click="close">Cancel</Button>
          <Button :disabled="pending || noConnections || missing.length > 0" @click="create(close)">Create flow</Button>
        </div>
      </div>
    </template>
  </Dialog>
</template>
