<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Button, Card, Badge } from '@/components/ui'
import { Cable, RadioTower, RefreshCw } from 'lucide-vue-next'
import { useShellStore } from '@/stores/shell'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { createVisibilityPoller, type Poller } from '@/lib/poller'
import { apiErrorMessage } from '@/stores/agent'
import { PERMIT_JOIN_SECONDS, ZIGBEE_POLL_MS, useZigbeeStore } from '@/stores/zigbee'
import { messages as m, formatDateTime } from '@/i18n'
import { formatDuration, keyValues, permitJoinRemaining, relativeTime, type ZigbeeNetwork } from '@/lib/zigbee'

const store = useZigbeeStore()
const { feedback, pending, run } = useFeedback()

const loading = ref(true)
const loadError = ref('')
const pollError = ref('')
// The broker connections dialog is the navigation's one copy.
const shell = useShellStore()
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
    loadError.value = apiErrorMessage(e, m.value.zigbee.loadFailed)
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
    pollError.value = apiErrorMessage(e, m.value.zigbee.refreshFailed)
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
  return left === null ? '' : m.value.zigbee.timeLeft(formatDuration(left))
}

async function permitJoin(network: ZigbeeNetwork, seconds: number) {
  await run(
    () => store.permitJoin(network.connection, seconds),
    seconds ? m.value.zigbee.joinOpened(network.connection, seconds / 60) : m.value.zigbee.joinClosed(network.connection),
    e => apiErrorMessage(e, m.value.zigbee.bridgeFailed)
  )
}

watch(() => shell.brokersChanged, () => onConnectionsChanged())
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
          <p class="max-w-2xl text-muted-foreground">{{ m.zigbee.intro }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button variant="outline" :disabled="loading" @click="load"><RefreshCw class="mr-2 h-4 w-4" />{{ m.zigbee.refresh }}</Button>
          <Button variant="outline" @click="shell.showBrokers = true"><Cable class="mr-2 h-4 w-4" />{{ m.zigbee.connections }}</Button>
        </div>
      </header>

      <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />
      <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }}</p>
      <p v-else-if="pollError" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ pollError }}</p>

      <div v-if="loading" class="flex items-center justify-center py-12" role="status" :aria-label="m.zigbee.loading">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>

      <!-- No broker connection yet -->
      <Card v-else-if="!hasConnections && !loadError" class="space-y-3 p-6 text-center">
        <Cable class="mx-auto h-12 w-12 text-primary/70" aria-hidden="true" />
        <h2 class="text-lg font-semibold">{{ m.zigbee.noConnectionsTitle }}</h2>
        <p class="mx-auto max-w-xl text-sm text-muted-foreground">{{ m.zigbee.noConnectionsText[0] }}<code>tcp://192.168.1.10:1883</code>{{ m.zigbee.noConnectionsText[1] }}</p>
        <Button @click="shell.showBrokers = true"><Cable class="mr-2 h-4 w-4" />{{ m.zigbee.addConnection }}</Button>
      </Card>

      <template v-else>
        <Card v-for="network in store.networks" :key="network.connection" class="space-y-4 p-4 sm:p-6">
          <section :aria-labelledby="`zigbee-net-${network.connection}`" class="space-y-4">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="space-y-2">
                <h2 :id="`zigbee-net-${network.connection}`" class="break-all font-mono text-lg font-semibold">{{ network.connection }}</h2>
                <div class="flex flex-wrap gap-2 text-xs">
                  <Badge :variant="network.connected ? 'secondary' : 'destructive'">{{ m.zigbee.broker(network.connected) }}</Badge>
                  <Badge :variant="network.bridge.online ? 'secondary' : 'outline'">{{ m.zigbee.bridge(network.bridge.online) }}</Badge>
                  <Badge v-if="network.bridge.info.version" variant="outline">Zigbee2MQTT {{ network.bridge.info.version }}</Badge>
                  <Badge v-if="network.bridge.info.coordinator" variant="outline">{{ m.zigbee.coordinator(network.bridge.info.coordinator) }}</Badge>
                  <Badge variant="outline">{{ m.zigbee.topic(network.baseTopic) }}</Badge>
                </div>
              </div>
              <!-- Permit join: open the network to new devices. -->
              <div class="flex flex-wrap items-center gap-2" aria-live="polite">
                <template v-if="network.bridge.info.permitJoin">
                  <span class="text-sm font-medium text-primary">
                    {{ m.zigbee.joiningOpen }}<template v-if="joinRemaining(network)"> · {{ joinRemaining(network) }}</template>
                  </span>
                  <Button size="sm" variant="outline" :disabled="pending" :aria-label="m.zigbee.stopJoiningOn(network.connection)" @click="permitJoin(network, 0)">{{ m.zigbee.stop }}</Button>
                </template>
                <Button v-else size="sm" :disabled="pending" :aria-label="m.zigbee.allowJoiningOn(network.connection)" @click="permitJoin(network, PERMIT_JOIN_SECONDS)">
                  {{ m.zigbee.allowJoining }}
                </Button>
              </div>
            </div>

            <!-- Devices -->
            <div v-if="!devices(network).length" class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              <p v-if="!network.connected">{{ m.zigbee.unreachable }}</p>
              <p v-else>{{ m.zigbee.noDevices[0] }}<code>{{ network.baseTopic }}/bridge/devices</code>{{ m.zigbee.noDevices[1] }}</p>
            </div>
            <div v-else class="overflow-x-auto">
              <table class="w-full min-w-[720px] text-left text-sm">
                <caption class="sr-only">{{ m.zigbee.devicesOn(network.connection) }}</caption>
                <thead class="border-b text-xs uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th scope="col" class="py-2 pr-3 font-medium">{{ m.zigbee.columns.device }}</th>
                    <th scope="col" class="py-2 pr-3 font-medium">{{ m.zigbee.columns.vendor }}</th>
                    <th scope="col" class="py-2 pr-3 font-medium">{{ m.zigbee.columns.power }}</th>
                    <th scope="col" class="py-2 pr-3 font-medium">{{ m.zigbee.columns.status }}</th>
                    <th scope="col" class="py-2 pr-3 font-medium">{{ m.zigbee.columns.lastSeen }}</th>
                    <th scope="col" class="py-2 font-medium">{{ m.zigbee.columns.readings }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y">
                  <tr v-for="device in devices(network)" :key="device.ieee || device.friendlyName" class="align-top">
                    <th scope="row" class="py-2 pr-3 font-normal">
                      <span class="break-all font-mono font-medium">{{ device.friendlyName }}</span>
                      <span v-if="device.description" class="block text-xs text-muted-foreground">{{ device.description }}</span>
                      <span v-if="!device.supported" class="block text-xs text-amber-700 dark:text-amber-400">{{ m.zigbee.notSupported }}</span>
                      <span v-if="device.disabled" class="block text-xs text-muted-foreground">{{ m.zigbee.disabled }}</span>
                    </th>
                    <td class="py-2 pr-3">
                      <template v-if="device.vendor || device.model">{{ [device.vendor, device.model].filter(Boolean).join(' ') }}</template>
                      <span v-else class="text-muted-foreground">—</span>
                    </td>
                    <td class="py-2 pr-3">{{ device.powerSource || '—' }}</td>
                    <td class="py-2 pr-3">
                      <span v-if="device.online === true" class="text-primary">{{ m.zigbee.online }}</span>
                      <span v-else-if="device.online === false" class="font-medium text-destructive">{{ m.zigbee.offline }}</span>
                      <span v-else class="text-muted-foreground" :title="m.zigbee.availabilityHint">{{ m.zigbee.unknown }}</span>
                    </td>
                    <td class="py-2 pr-3 whitespace-nowrap">
                      <time v-if="device.lastSeen" :datetime="device.lastSeen" :title="formatDateTime(device.lastSeen)">{{ relativeTime(device.lastSeen, now) }}</time>
                      <span v-else class="text-muted-foreground">—</span>
                    </td>
                    <td class="py-2">
                      <ul v-if="keyValues(device).length" class="flex flex-wrap gap-1.5">
                        <li v-for="value in keyValues(device)" :key="value.property" class="rounded bg-muted px-1.5 py-0.5 text-xs">
                          <span class="text-muted-foreground">{{ value.label }}</span> {{ value.text }}
                        </li>
                      </ul>
                      <span v-else class="text-muted-foreground">{{ m.zigbee.noReport }}</span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </Card>
        <p v-if="!store.networks.length && store.connections.length" class="text-sm text-muted-foreground">{{ m.zigbee.watching }}</p>
      </template>
    </div>

  </main>
</template>
