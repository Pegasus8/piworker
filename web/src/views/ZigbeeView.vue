<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Button, Card, Badge } from '@/components/ui'
import { Cable, RadioTower, RefreshCw } from 'lucide-vue-next'
import BrokerConnectionsDialog from '@/components/BrokerConnectionsDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { createVisibilityPoller, type Poller } from '@/lib/poller'
import { apiErrorMessage } from '@/stores/agent'
import { PERMIT_JOIN_SECONDS, ZIGBEE_POLL_MS, useZigbeeStore } from '@/stores/zigbee'
import { formatDuration, keyValues, permitJoinRemaining, relativeTime, type ZigbeeNetwork } from '@/lib/zigbee'

const store = useZigbeeStore()
const { feedback, pending, run } = useFeedback()

const loading = ref(true)
const loadError = ref('')
const pollError = ref('')
const showConnections = ref(false)
// The clock of the countdowns and "last seen" (no requests).
const now = ref(Date.now())

let lastLoad = 0
async function load() {
  loadError.value = ''
  try {
    await Promise.all([store.fetchConnections(), store.fetchNetworks()])
    lastLoad = Date.now()
    pollError.value = ''
  } catch (e) {
    loadError.value = apiErrorMessage(e, 'Could not load the Zigbee networks. Check your connection and try again.')
  } finally {
    loading.value = false
  }
}

// The poll is a background read: an open page alone never keeps the session
// alive (ADR-0017). It pauses while the tab is hidden.
async function poll() {
  if (Date.now() - lastLoad < 1000) return
  try {
    await store.fetchNetworks({ background: true })
    pollError.value = ''
  } catch (e) {
    pollError.value = apiErrorMessage(e, 'Could not refresh the networks; retrying.')
  }
}

let poller: Poller | null = null
let clock: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await load()
  poller = createVisibilityPoller(poll, { intervalMs: ZIGBEE_POLL_MS })
  poller.start()
  clock = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => {
  poller?.stop()
  if (clock) clearInterval(clock)
})

const hasConnections = computed(() => store.connections.length > 0 || store.networks.length > 0)

function devices(network: ZigbeeNetwork) {
  return network.devices.filter(d => d.type !== 'Coordinator')
}

function joinRemaining(network: ZigbeeNetwork): string {
  const left = permitJoinRemaining(network.bridge.info, now.value)
  return left === null ? '' : `${formatDuration(left)} left`
}

async function permitJoin(network: ZigbeeNetwork, seconds: number) {
  await run(
    () => store.permitJoin(network.connection, seconds),
    seconds ? `Joining is open on “${network.connection}” for ${seconds / 60} minutes. Pair your device now.` : `Joining closed on “${network.connection}”.`,
    e => apiErrorMessage(e, 'Could not reach the bridge. Try again.')
  )
}

function onConnectionsChanged() {
  void load()
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-6xl space-y-6">

      <header class="flex flex-wrap items-end justify-between gap-4">
        <div class="space-y-2">
          <h1 class="flex items-center gap-2 text-2xl font-bold"><RadioTower class="h-6 w-6 text-primary" aria-hidden="true" />Zigbee</h1>
          <p class="max-w-2xl text-muted-foreground">
            The devices Zigbee2MQTT reports on each broker connection, with their latest readings. Use their names in
            Zigbee nodes.
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button variant="outline" :disabled="loading" @click="load"><RefreshCw class="mr-2 h-4 w-4" />Refresh</Button>
          <Button variant="outline" @click="showConnections = true"><Cable class="mr-2 h-4 w-4" />Connections</Button>
        </div>
      </header>

      <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />
      <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
      <p v-else-if="pollError" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ pollError }}</p>

      <div v-if="loading" class="flex items-center justify-center py-12" role="status" aria-label="Loading">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>

      <!-- No broker connection yet -->
      <Card v-else-if="!hasConnections && !loadError" class="space-y-3 p-6 text-center">
        <Cable class="mx-auto h-12 w-12 text-primary/70" aria-hidden="true" />
        <h2 class="text-lg font-semibold">No broker connections yet</h2>
        <p class="mx-auto max-w-xl text-sm text-muted-foreground">
          PiWorker talks to your Zigbee devices through Zigbee2MQTT and its MQTT broker. Add a connection with the
          broker's address (for example <code>tcp://192.168.1.10:1883</code>) and Zigbee2MQTT's base topic, and the
          network shows up here.
        </p>
        <Button @click="showConnections = true"><Cable class="mr-2 h-4 w-4" />Add a broker connection</Button>
      </Card>

      <template v-else>
        <Card v-for="network in store.networks" :key="network.connection" class="space-y-4 p-4 sm:p-6">
          <section :aria-labelledby="`zigbee-net-${network.connection}`" class="space-y-4">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="space-y-2">
                <h2 :id="`zigbee-net-${network.connection}`" class="break-all font-mono text-lg font-semibold">{{ network.connection }}</h2>
                <div class="flex flex-wrap gap-2 text-xs">
                  <Badge :variant="network.connected ? 'secondary' : 'destructive'">Broker {{ network.connected ? 'connected' : 'disconnected' }}</Badge>
                  <Badge :variant="network.bridge.online ? 'secondary' : 'outline'">Bridge {{ network.bridge.online ? 'online' : 'offline' }}</Badge>
                  <Badge v-if="network.bridge.info.version" variant="outline">Zigbee2MQTT {{ network.bridge.info.version }}</Badge>
                  <Badge v-if="network.bridge.info.coordinator" variant="outline">Coordinator {{ network.bridge.info.coordinator }}</Badge>
                  <Badge variant="outline">Topic {{ network.baseTopic }}</Badge>
                </div>
              </div>
              <!-- Permit join: open the network to new devices. -->
              <div class="flex flex-wrap items-center gap-2" aria-live="polite">
                <template v-if="network.bridge.info.permitJoin">
                  <span class="text-sm font-medium text-primary">
                    Joining is open<template v-if="joinRemaining(network)"> · {{ joinRemaining(network) }}</template>
                  </span>
                  <Button size="sm" variant="outline" :disabled="pending" :aria-label="`Stop joining on ${network.connection}`" @click="permitJoin(network, 0)">Stop</Button>
                </template>
                <Button v-else size="sm" :disabled="pending" :aria-label="`Allow joining for 4 minutes on ${network.connection}`" @click="permitJoin(network, PERMIT_JOIN_SECONDS)">
                  Allow joining for 4 minutes
                </Button>
              </div>
            </div>

            <!-- Devices -->
            <div v-if="!devices(network).length" class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              <p v-if="!network.connected">PiWorker cannot reach this broker yet. Check the connection's address and credentials (Connections → Test connection).</p>
              <p v-else>
                No devices yet. Zigbee2MQTT has not published its device list on <code>{{ network.baseTopic }}/bridge/devices</code>:
                check that it runs, uses this broker and base topic, then pair a device with “Allow joining”.
              </p>
            </div>
            <div v-else class="overflow-x-auto">
              <table class="w-full min-w-[720px] text-left text-sm">
                <caption class="sr-only">Devices on {{ network.connection }}</caption>
                <thead class="border-b text-xs uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th scope="col" class="py-2 pr-3 font-medium">Device</th>
                    <th scope="col" class="py-2 pr-3 font-medium">Vendor / model</th>
                    <th scope="col" class="py-2 pr-3 font-medium">Power</th>
                    <th scope="col" class="py-2 pr-3 font-medium">Status</th>
                    <th scope="col" class="py-2 pr-3 font-medium">Last seen</th>
                    <th scope="col" class="py-2 font-medium">Readings</th>
                  </tr>
                </thead>
                <tbody class="divide-y">
                  <tr v-for="device in devices(network)" :key="device.ieee || device.friendlyName" class="align-top">
                    <th scope="row" class="py-2 pr-3 font-normal">
                      <span class="break-all font-mono font-medium">{{ device.friendlyName }}</span>
                      <span v-if="device.description" class="block text-xs text-muted-foreground">{{ device.description }}</span>
                      <span v-if="!device.supported" class="block text-xs text-amber-700 dark:text-amber-400">Not supported by Zigbee2MQTT</span>
                      <span v-if="device.disabled" class="block text-xs text-muted-foreground">Disabled</span>
                    </th>
                    <td class="py-2 pr-3">
                      <template v-if="device.vendor || device.model">{{ [device.vendor, device.model].filter(Boolean).join(' ') }}</template>
                      <span v-else class="text-muted-foreground">—</span>
                    </td>
                    <td class="py-2 pr-3">{{ device.powerSource || '—' }}</td>
                    <td class="py-2 pr-3">
                      <span v-if="device.online === true" class="text-primary">Online</span>
                      <span v-else-if="device.online === false" class="font-medium text-destructive">Offline</span>
                      <span v-else class="text-muted-foreground" title="Turn on availability in Zigbee2MQTT to see it">Unknown</span>
                    </td>
                    <td class="py-2 pr-3 whitespace-nowrap">
                      <time v-if="device.lastSeen" :datetime="device.lastSeen" :title="new Date(device.lastSeen).toLocaleString()">{{ relativeTime(device.lastSeen, now) }}</time>
                      <span v-else class="text-muted-foreground">—</span>
                    </td>
                    <td class="py-2">
                      <ul v-if="keyValues(device).length" class="flex flex-wrap gap-1.5">
                        <li v-for="value in keyValues(device)" :key="value.property" class="rounded bg-muted px-1.5 py-0.5 text-xs">
                          <span class="text-muted-foreground">{{ value.label }}</span> {{ value.text }}
                        </li>
                      </ul>
                      <span v-else class="text-muted-foreground">No report yet</span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </Card>
        <p v-if="!store.networks.length && store.connections.length" class="text-sm text-muted-foreground">
          The connections are being watched; their networks appear here in a moment.
        </p>
      </template>
    </div>

    <BrokerConnectionsDialog v-model:open="showConnections" @changed="onConnectionsChanged" />
  </main>
</template>
