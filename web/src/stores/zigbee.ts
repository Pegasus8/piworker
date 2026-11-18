// Broker connections (ADR-0026) and Zigbee2MQTT discovery: the connections
// dialog, the Zigbee page, the node config form's suggestions and the
// example flows share these lists. Admin session only.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiClient } from '@/composables/useApi'
import { BACKGROUND_HEADERS } from '@/stores/agent'
import { msg } from '@/i18n'
import {
  connectionList,
  connectionRequest,
  connectionRestarts,
  type ConnectionRestarts,
  networkList,
  type BrokerConnection,
  type ConnectionForm,
  type ZigbeeNetwork
} from '@/lib/zigbee'

/** How often the Zigbee page refreshes the networks. */
export const ZIGBEE_POLL_MS = 5_000
/** "Allow joining for 4 minutes". */
export const PERMIT_JOIN_SECONDS = 240

export interface ConnectionTestResult { ok: boolean; error?: string }

const path = (name: string) => encodeURIComponent(name)

export const useZigbeeStore = defineStore('zigbee', () => {
  const connections = ref<BrokerConnection[]>([])
  const networks = ref<ZigbeeNetwork[]>([])
  const connectionsLoaded = ref(false)
  const networksLoaded = ref(false)

  async function fetchConnections() {
    const { data } = await apiClient.get('/connections')
    connections.value = connectionList(data.data)
    connectionsLoaded.value = true
  }

  /** Creates or replaces a connection; an empty password keeps the stored one. */
  /**
   * Stores a connection. The server restarts the running flows that use it
   * (ADR-0029) and answers which ones did and which could not start again.
   */
  async function saveConnection(form: ConnectionForm): Promise<ConnectionRestarts> {
    const { data } = await apiClient.put(`/connections/${path(form.name.trim())}`, connectionRequest(form))
    await fetchConnections()
    return connectionRestarts(data?.data)
  }

  async function deleteConnection(name: string) {
    await apiClient.delete(`/connections/${path(name)}`)
    await fetchConnections()
  }

  /** Connects to the broker with these settings (the stored password when none is typed); stores nothing. */
  async function testConnection(form: ConnectionForm): Promise<ConnectionTestResult> {
    const { data } = await apiClient.post(`/connections/${path(form.name.trim())}/test`, connectionRequest(form))
    const result = data.data ?? {}
    return result.ok === true ? { ok: true } : { ok: false, error: typeof result.error === 'string' && result.error ? result.error : msg().zigbee.noAnswer }
  }

  // A background read (the page's poll) does not extend the session (ADR-0017).
  async function fetchNetworks(opts: { background?: boolean } = {}) {
    const { data } = await apiClient.get('/zigbee', opts.background ? { headers: BACKGROUND_HEADERS } : undefined)
    networks.value = networkList(data.data)
    networksLoaded.value = true
  }

  /** Opens the network to new devices for seconds (0 closes it), then shows the bridge's answer. */
  async function permitJoin(connection: string, seconds: number) {
    await apiClient.post(`/zigbee/${path(connection)}/permit-join`, { seconds })
    try { await fetchNetworks() } catch { /* the page's poll shows it */ }
  }

  /**
   * Loads what the node config form suggests, each list once unless forced.
   * Never throws: a list that fails stays as it was and the fields stay
   * free text. Returns whether everything asked for loaded.
   */
  async function loadSuggestions(need: { connections?: boolean; networks?: boolean; force?: boolean }): Promise<boolean> {
    const reads: Promise<void>[] = []
    if (need.connections && (need.force || !connectionsLoaded.value)) reads.push(fetchConnections())
    if (need.networks && (need.force || !networksLoaded.value)) reads.push(fetchNetworks())
    const results = await Promise.allSettled(reads)
    return results.every(r => r.status === 'fulfilled')
  }

  return {
    connections,
    networks,
    connectionsLoaded,
    networksLoaded,
    fetchConnections,
    saveConnection,
    deleteConnection,
    testConnection,
    fetchNetworks,
    permitJoin,
    loadSuggestions
  }
})
