// Broker connections (ADR-0026) and Zigbee2MQTT discovery as the editor
// shows them: the connections dialog, the Zigbee page and the suggestions of
// the node config form. Pure helpers; the requests live in stores/zigbee.ts.
import { msg } from '@/i18n'

/** Where a node setting finds its suggestions (ConfigProperty.Options). The value stays free text. */
export type SuggestSource = 'connections' | 'zigbee-devices' | 'zigbee-properties' | 'zigbee-settable'
const SOURCES: readonly SuggestSource[] = ['connections', 'zigbee-devices', 'zigbee-properties', 'zigbee-settable']

export function suggestSource(value: unknown): SuggestSource | undefined {
  return SOURCES.includes(value as SuggestSource) ? value as SuggestSource : undefined
}

/** A named broker connection as listed: the password is write-only, only whether one is stored. */
export interface BrokerConnection {
  name: string
  broker: string
  username: string
  hasPassword: boolean
  baseTopic: string
}

/** Access bits of a Zigbee2MQTT feature. */
export const ACCESS_PUBLISHED = 1
export const ACCESS_SETTABLE = 2
export const ACCESS_GETTABLE = 4

export interface ZigbeeFeature {
  property: string
  name: string
  type: string
  access: number
  unit?: string
  min?: number
  max?: number
  values?: string[]
  valueOn?: unknown
  valueOff?: unknown
  valueToggle?: unknown
  description?: string
}

export interface ZigbeeDevice {
  friendlyName: string
  ieee: string
  type: string
  powerSource?: string
  model?: string
  vendor?: string
  description?: string
  supported: boolean
  disabled?: boolean
  features: ZigbeeFeature[]
  state?: Record<string, unknown>
  online?: boolean
  lastSeen?: string
}

export interface ZigbeeBridgeInfo {
  version: string
  permitJoin: boolean
  /** When joining closes, Unix milliseconds (Zigbee2MQTT 2.x), if it says. */
  permitJoinEnd?: number
  coordinator?: string
}

export interface ZigbeeNetwork {
  connection: string
  baseTopic: string
  /** Connected to the broker. */
  connected: boolean
  bridge: { online: boolean; info: ZigbeeBridgeInfo }
  devices: ZigbeeDevice[]
}

const str = (v: unknown) => typeof v === 'string' ? v : ''
const obj = (v: unknown): Record<string, any> => v && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, any> : {}
const arr = (v: unknown): any[] => Array.isArray(v) ? v : []

/** The connections of GET /api/connections (data), never with a password. */
export function connectionList(data: unknown): BrokerConnection[] {
  return arr(obj(data).connections).map(c => ({
    name: str(c?.name),
    broker: str(c?.broker),
    username: str(c?.username),
    hasPassword: c?.hasPassword === true,
    baseTopic: str(c?.baseTopic)
  }))
}

export function normalizeNetwork(raw: unknown): ZigbeeNetwork {
  const n = obj(raw)
  const bridge = obj(n.bridge)
  const info = obj(bridge.info)
  return {
    connection: str(n.connection),
    baseTopic: str(n.baseTopic),
    connected: n.connected === true,
    bridge: {
      online: bridge.online === true,
      info: {
        version: str(info.version),
        permitJoin: info.permitJoin === true,
        ...(typeof info.permitJoinEnd === 'number' ? { permitJoinEnd: info.permitJoinEnd } : {}),
        ...(info.coordinator ? { coordinator: str(info.coordinator) } : {})
      }
    },
    devices: arr(n.devices).map(d => ({ ...obj(d), friendlyName: str(d?.friendlyName), features: arr(d?.features) }) as ZigbeeDevice)
  }
}

/** The networks of GET /api/zigbee (data). */
export function networkList(data: unknown): ZigbeeNetwork[] {
  return arr(obj(data).networks).map(normalizeNetwork)
}

// --- The connection form -------------------------------------------------

export const DEFAULT_BASE_TOPIC = 'zigbee2mqtt'
const NAME = /^[A-Za-z0-9_-]+$/
const BROKER = /^(tcp|mqtt|ssl|mqtts|ws|wss):\/\/\S+$/i

export interface ConnectionForm {
  name: string
  broker: string
  username: string
  /** Empty keeps the stored password. */
  password: string
  /** Remove the stored password (when no new one is typed). */
  clearPassword: boolean
  baseTopic: string
}

export interface ConnectionRequest {
  broker: string
  username: string
  baseTopic: string
  /** Omitted: keep the stored one; '': clear it. */
  password?: string
}

/** The body of PUT /api/connections/{name} and of its test. */
export function connectionRequest(form: ConnectionForm): ConnectionRequest {
  const body: ConnectionRequest = {
    broker: form.broker.trim(),
    username: form.username.trim(),
    baseTopic: form.baseTopic.trim() || DEFAULT_BASE_TOPIC
  }
  if (form.password) body.password = form.password
  else if (form.clearPassword) body.password = ''
  return body
}

/** What is wrong with the form before it is sent ('' when nothing); the server checks again. */
export function connectionFormError(form: ConnectionForm): string {
  const t = msg().zigbee.form
  const name = form.name.trim()
  if (!name) return t.nameRequired
  if (name.length > 64) return t.nameTooLong
  if (!NAME.test(name)) return t.nameChars
  const broker = form.broker.trim()
  if (!broker) return t.brokerRequired
  if (!BROKER.test(broker)) return t.brokerFormat
  return ''
}

// --- Suggestions for node settings ------------------------------------------

export interface SuggestionContext {
  connections: BrokerConnection[]
  networks: ZigbeeNetwork[]
  /** The node's settings being edited (connection, device). */
  config: Record<string, unknown>
}

export function findNetwork(networks: ZigbeeNetwork[], connection: unknown): ZigbeeNetwork | undefined {
  return typeof connection === 'string' && connection ? networks.find(n => n.connection === connection) : undefined
}

/** The devices of the connection's network, or of every network when it is unset or unknown. */
export function devicesFor(networks: ZigbeeNetwork[], connection: unknown): ZigbeeDevice[] {
  const network = findNetwork(networks, connection)
  return network ? network.devices : networks.flatMap(n => n.devices)
}

export function findDevice(networks: ZigbeeNetwork[], connection: unknown, device: unknown): ZigbeeDevice | undefined {
  return typeof device === 'string' && device ? devicesFor(networks, connection).find(d => d.friendlyName === device) : undefined
}

const unique = (values: string[]) => [...new Set(values.filter(Boolean))]

/** The suggestions of a setting (they never restrict what may be typed). */
export function suggestions(source: SuggestSource, ctx: SuggestionContext): string[] {
  if (source === 'connections') return unique(ctx.connections.map(c => c.name))
  if (source === 'zigbee-devices') return unique(devicesFor(ctx.networks, ctx.config.connection).map(d => d.friendlyName))
  const bit = source === 'zigbee-settable' ? ACCESS_SETTABLE : ACCESS_PUBLISHED
  const device = findDevice(ctx.networks, ctx.config.connection, ctx.config.device)
  const devices = device ? [device] : devicesFor(ctx.networks, ctx.config.connection)
  return unique(devices.flatMap(d => d.features.filter(f => (f.access & bit) !== 0).map(f => f.property)))
}

/** Unit, range, values and access of a feature: "W, read-only", "off, on, toggle, previous". */
export function featureHint(feature: ZigbeeFeature): string {
  const parts: string[] = []
  if (feature.unit) parts.push(feature.unit)
  if (typeof feature.min === 'number' && typeof feature.max === 'number') parts.push(`${feature.min}–${feature.max}`)
  if (feature.values?.length) parts.push(feature.values.join(', '))
  else if (feature.type === 'binary' && feature.valueOn !== undefined && feature.valueOff !== undefined) parts.push(`${String(feature.valueOn)} / ${String(feature.valueOff)}`)
  if ((feature.access & ACCESS_SETTABLE) === 0) parts.push(msg().zigbee.readOnly)
  return parts.join(', ')
}

/** Vendor, model and power source of a device. */
export function deviceHint(device: ZigbeeDevice): string {
  return [[device.vendor, device.model].filter(Boolean).join(' '), device.powerSource].filter(Boolean).join(' · ')
}

/** The hint under a device or property setting ('' when nothing is known). */
export function fieldHint(source: SuggestSource, ctx: SuggestionContext): string {
  if (source === 'connections') return ''
  const device = findDevice(ctx.networks, ctx.config.connection, ctx.config.device)
  if (!device) return ''
  if (source === 'zigbee-devices') return deviceHint(device)
  const property = ctx.config.property
  const feature = typeof property === 'string' ? device.features.find(f => f.property === property) : undefined
  if (!feature) return ''
  const now = device.state?.[feature.property]
  return [featureHint(feature), now === undefined ? '' : msg().zigbee.now(formatValue(now, feature.unit))].filter(Boolean).join(' · ')
}

// --- The Zigbee page ----------------------------------------------------------

/** The readings the device table shows, in this order, when a device reports them. */
export const KEY_PROPERTIES = ['state', 'power', 'energy', 'temperature', 'humidity', 'battery'] as const

export function formatValue(value: unknown, unit?: string): string {
  const text = typeof value === 'string' ? value : typeof value === 'number' || typeof value === 'boolean' ? String(value) : JSON.stringify(value)
  return unit ? `${text} ${unit}` : text
}

export interface KeyValue { property: string; label: string; text: string }

export function keyValues(device: ZigbeeDevice): KeyValue[] {
  const state = device.state ?? {}
  return KEY_PROPERTIES.filter(p => state[p] !== undefined && state[p] !== null).map(p => ({
    property: p,
    label: msg().zigbee.keyLabels[p] ?? p.charAt(0).toUpperCase() + p.slice(1),
    text: formatValue(state[p], device.features.find(f => f.property === p)?.unit)
  }))
}

/** How long permit join stays open (seconds, never negative), or null when it is closed or the bridge does not say. */
export function permitJoinRemaining(info: ZigbeeBridgeInfo, nowMs: number): number | null {
  if (!info.permitJoin || typeof info.permitJoinEnd !== 'number') return null
  return Math.max(0, Math.ceil((info.permitJoinEnd - nowMs) / 1000))
}

/** 91 → "1:31". */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

/** "30 s ago", "5 min ago"…; '' for a missing or invalid time. */
export function relativeTime(iso: string | undefined, nowMs: number): string {
  if (!iso) return ''
  const at = Date.parse(iso)
  if (Number.isNaN(at)) return ''
  const s = Math.floor((nowMs - at) / 1000)
  const t = msg().zigbee
  if (s < 5) return t.justNow
  if (s < 60) return t.secondsAgo(s)
  if (s < 3600) return t.minutesAgo(Math.floor(s / 60))
  if (s < 86400) return t.hoursAgo(Math.floor(s / 3600))
  return t.daysAgo(Math.floor(s / 86400))
}

/** A flow a response names. */
export interface FlowRef { id: string; name: string }

/**
 * What storing a connection did to the running flows that use it
 * (PUT /api/connections/{name}, ADR-0029): restarted with the new
 * settings, or stopped and failed to start again (with the reason).
 */
export interface ConnectionRestarts {
  restarted: FlowRef[]
  failed: (FlowRef & { error: string })[]
}

function flowRef(raw: unknown): FlowRef | null {
  const r = (raw ?? {}) as { id?: unknown; name?: unknown }
  return typeof r.id === 'string' ? { id: r.id, name: typeof r.name === 'string' ? r.name : r.id } : null
}

/** Reads PUT's data member; anything missing reads as nothing restarted. */
export function connectionRestarts(data: unknown): ConnectionRestarts {
  const d = (data ?? {}) as { restarted?: unknown; failed?: unknown }
  const restarted = (Array.isArray(d.restarted) ? d.restarted : []).map(flowRef).filter((f): f is FlowRef => f !== null)
  const failed: ConnectionRestarts['failed'] = []
  for (const raw of Array.isArray(d.failed) ? d.failed : []) {
    const ref = flowRef(raw)
    const error = (raw as { error?: unknown } | null)?.error
    if (ref) failed.push({ ...ref, error: typeof error === 'string' ? error : '' })
  }
  return { restarted, failed }
}

/** The feedback after saving a connection: what happened to the flows using it. */
export function connectionSavedText(name: string, r: ConnectionRestarts): { kind: 'success' | 'warning'; text: string } {
  const t = msg().zigbee.dialog
  const parts = [t.saved(name)]
  if (r.restarted.length) parts.push(t.restarted(r.restarted.map(f => f.name)))
  if (r.failed.length) parts.push(t.restartFailed(r.failed.map(f => `${f.name}: ${f.error}`)))
  return { kind: r.failed.length ? 'warning' : 'success', text: parts.join(' ') }
}
