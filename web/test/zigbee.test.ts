import { expect, test } from 'bun:test'
import { setLocale } from '../src/i18n'
import {
  connectionFormError,
  connectionList,
  connectionRequest,
  connectionRestarts,
  connectionSavedText,
  deviceHint,
  devicesFor,
  featureHint,
  fieldHint,
  formatDuration,
  keyValues,
  networkList,
  permitJoinRemaining,
  relativeTime,
  suggestSource,
  suggestions,
  type ZigbeeNetwork
} from '../src/lib/zigbee'

const plug = {
  friendlyName: 'washer_plug', ieee: '0x01', type: 'Router', powerSource: 'Mains (single phase)', model: 'S60ZBTPF', vendor: 'SONOFF',
  supported: true,
  features: [
    { property: 'state', name: 'state', type: 'binary', access: 7, valueOn: 'ON', valueOff: 'OFF', valueToggle: 'TOGGLE' },
    { property: 'power', name: 'power', type: 'numeric', access: 5, unit: 'W' },
    { property: 'energy', name: 'energy', type: 'numeric', access: 5, unit: 'kWh' },
    { property: 'power_on_behavior', name: 'power_on_behavior', type: 'enum', access: 7, values: ['off', 'on', 'toggle', 'previous'] },
    { property: 'linkquality', name: 'linkquality', type: 'numeric', access: 1, unit: 'lqi', min: 0, max: 255 }
  ],
  state: { state: 'ON', power: 1830.4, energy: 12.25, linkquality: 120 },
  online: true,
  lastSeen: '2026-11-11T11:59:30Z'
}
const sensor = {
  friendlyName: 'bedroom_climate', ieee: '0x02', type: 'EndDevice', powerSource: 'Battery', model: 'SNZB-02P', vendor: 'SONOFF',
  supported: true,
  features: [
    { property: 'temperature', name: 'temperature', type: 'numeric', access: 1, unit: '°C' },
    { property: 'humidity', name: 'humidity', type: 'numeric', access: 1, unit: '%' },
    { property: 'battery', name: 'battery', type: 'numeric', access: 1, unit: '%', min: 0, max: 100 }
  ],
  state: { temperature: 21.5, humidity: 64, battery: 87 }
}
const raw = {
  networks: [
    { connection: 'home', baseTopic: 'zigbee2mqtt', connected: true, bridge: { online: true, info: { version: '2.1.0', permitJoin: false, coordinator: 'zStack3x0' } }, devices: [plug, sensor] },
    { connection: 'garage', baseTopic: 'z2m', connected: false, bridge: { online: false, info: { version: '', permitJoin: false } }, devices: null }
  ]
}

test('networkList normalizes missing lists and features', () => {
  const nets = networkList(raw)
  expect(nets.map(n => n.connection)).toEqual(['home', 'garage'])
  expect(nets[1].devices).toEqual([])
  const bare = networkList({ networks: [{ connection: 'x', devices: [{ friendlyName: 'd', features: null }] }] })
  expect(bare[0].devices[0].features).toEqual([])
  expect(bare[0].bridge.info.permitJoin).toBe(false)
  expect(networkList(undefined)).toEqual([])
})

test('connectionList keeps only names and flags, never a password', () => {
  const list = connectionList({ connections: [{ name: 'home', broker: 'tcp://10.0.0.2:1883', username: 'pi', hasPassword: true, baseTopic: 'zigbee2mqtt', password: 'leak' }] })
  expect(list).toEqual([{ name: 'home', broker: 'tcp://10.0.0.2:1883', username: 'pi', hasPassword: true, baseTopic: 'zigbee2mqtt' }])
  expect(connectionList(null)).toEqual([])
})

test('connectionRequest omits an empty password, sends a new one, or clears it', () => {
  const form = { name: 'home', broker: ' tcp://10.0.0.2:1883 ', username: 'pi', password: '', clearPassword: false, baseTopic: '' }
  expect(connectionRequest(form)).toEqual({ broker: 'tcp://10.0.0.2:1883', username: 'pi', baseTopic: 'zigbee2mqtt' })
  expect(connectionRequest({ ...form, password: 's3cret' })).toEqual({ broker: 'tcp://10.0.0.2:1883', username: 'pi', baseTopic: 'zigbee2mqtt', password: 's3cret' })
  expect(connectionRequest({ ...form, clearPassword: true })).toEqual({ broker: 'tcp://10.0.0.2:1883', username: 'pi', baseTopic: 'zigbee2mqtt', password: '' })
  // A typed password wins over "clear".
  expect(connectionRequest({ ...form, password: 'new', clearPassword: true }).password).toBe('new')
})

test('connectionFormError checks the name and broker URL', () => {
  const form = { name: 'home', broker: 'tcp://10.0.0.2:1883', username: '', password: '', clearPassword: false, baseTopic: '' }
  expect(connectionFormError(form)).toBe('')
  expect(connectionFormError({ ...form, name: '' })).toContain('name')
  expect(connectionFormError({ ...form, name: 'bad name' })).toContain('letters')
  expect(connectionFormError({ ...form, name: 'x'.repeat(65) })).toContain('64')
  expect(connectionFormError({ ...form, broker: '' })).toContain('broker')
  expect(connectionFormError({ ...form, broker: 'http://10.0.0.2' })).toContain('tcp://')
  for (const scheme of ['mqtt', 'ssl', 'mqtts', 'ws', 'wss']) expect(connectionFormError({ ...form, broker: `${scheme}://host:1` })).toBe('')
})

test('suggestSource accepts only the known sources', () => {
  expect(suggestSource('connections')).toBe('connections')
  expect(suggestSource('zigbee-settable')).toBe('zigbee-settable')
  expect(suggestSource('other')).toBeUndefined()
  expect(suggestSource(undefined)).toBeUndefined()
})

test('suggestions come from the connection, device and access bits', () => {
  const networks = networkList(raw)
  const connections = connectionList({ connections: [{ name: 'home' }, { name: 'garage' }] })
  const ctx = (config: Record<string, unknown>) => ({ connections, networks, config })
  expect(suggestions('connections', ctx({}))).toEqual(['home', 'garage'])
  expect(suggestions('zigbee-devices', ctx({ connection: 'home' }))).toEqual(['washer_plug', 'bedroom_climate'])
  expect(suggestions('zigbee-devices', ctx({ connection: 'garage' }))).toEqual([])
  // Unset or unknown connection: every network's devices.
  expect(suggestions('zigbee-devices', ctx({}))).toEqual(['washer_plug', 'bedroom_climate'])
  expect(suggestions('zigbee-properties', ctx({ connection: 'home', device: 'washer_plug' }))).toEqual(['state', 'power', 'energy', 'power_on_behavior', 'linkquality'])
  expect(suggestions('zigbee-settable', ctx({ connection: 'home', device: 'washer_plug' }))).toEqual(['state', 'power_on_behavior'])
  expect(suggestions('zigbee-settable', ctx({ connection: 'home', device: 'bedroom_climate' }))).toEqual([])
  // A template or unknown device: the properties of every device of the connection.
  expect(suggestions('zigbee-properties', ctx({ connection: 'home', device: '{{payload.device}}' }))).toContain('humidity')
})

test('devicesFor finds the devices of one network or of all of them', () => {
  const networks = networkList(raw)
  expect(devicesFor(networks, 'home').length).toBe(2)
  expect(devicesFor(networks, '').length).toBe(2)
  expect(devicesFor(networks, 'nope').length).toBe(2)
  expect(devicesFor(networks, 'garage').length).toBe(0)
})

test('featureHint describes unit, range, values and access', () => {
  const [state, power, , behavior, lqi] = networkList(raw)[0].devices[0].features
  expect(featureHint(power)).toBe('W, read-only')
  expect(featureHint(behavior)).toBe('off, on, toggle, previous')
  expect(featureHint(state)).toBe('ON / OFF')
  expect(featureHint(lqi)).toBe('lqi, 0–255, read-only')
})

test('fieldHint describes the device and the selected property', () => {
  const networks = networkList(raw)
  const ctx = { connections: [], networks, config: { connection: 'home', device: 'washer_plug', property: 'power' } }
  expect(fieldHint('zigbee-devices', ctx)).toBe('SONOFF S60ZBTPF · Mains (single phase)')
  expect(fieldHint('zigbee-properties', ctx)).toBe('W, read-only · now 1830.4 W')
  expect(fieldHint('zigbee-properties', { ...ctx, config: { ...ctx.config, property: 'nope' } })).toBe('')
  expect(fieldHint('connections', ctx)).toBe('')
  expect(deviceHint(networks[0].devices[1])).toBe('SONOFF SNZB-02P · Battery')
})

test('keyValues shows the main readings with their units', () => {
  const [p, s] = networkList(raw)[0].devices
  expect(keyValues(p).map(v => `${v.label}: ${v.text}`)).toEqual(['State: ON', 'Power: 1830.4 W', 'Energy: 12.25 kWh'])
  expect(keyValues(s).map(v => `${v.label}: ${v.text}`)).toEqual(['Temperature: 21.5 °C', 'Humidity: 64 %', 'Battery: 87 %'])
  expect(keyValues({ ...s, state: undefined })).toEqual([])
})

test('permitJoinRemaining counts down to permitJoinEnd', () => {
  const now = Date.UTC(2026, 10, 11, 12, 0, 0)
  expect(permitJoinRemaining({ version: '', permitJoin: false }, now)).toBeNull()
  expect(permitJoinRemaining({ version: '', permitJoin: true }, now)).toBeNull()
  expect(permitJoinRemaining({ version: '', permitJoin: true, permitJoinEnd: now + 90_500 }, now)).toBe(91)
  expect(permitJoinRemaining({ version: '', permitJoin: true, permitJoinEnd: now - 1000 }, now)).toBe(0)
  expect(formatDuration(91)).toBe('1:31')
  expect(formatDuration(240)).toBe('4:00')
  expect(formatDuration(5)).toBe('0:05')
})

test('relativeTime reads like a clock', () => {
  const now = Date.parse('2026-11-11T12:00:00Z')
  expect(relativeTime('2026-11-11T11:59:58Z', now)).toBe('just now')
  expect(relativeTime('2026-11-11T11:59:30Z', now)).toBe('30 s ago')
  expect(relativeTime('2026-11-11T11:55:00Z', now)).toBe('5 min ago')
  expect(relativeTime('2026-11-11T09:00:00Z', now)).toBe('3 h ago')
  expect(relativeTime('2026-11-08T12:00:00Z', now)).toBe('3 d ago')
  expect(relativeTime(undefined, now)).toBe('')
  expect(relativeTime('not a date', now)).toBe('')
})

test('the type of a network is exported for views', () => {
  const n: ZigbeeNetwork = networkList(raw)[0]
  expect(n.bridge.info.coordinator).toBe('zStack3x0')
})

test('storing a connection reads which running flows restarted', () => {
  expect(connectionRestarts(null)).toEqual({ restarted: [], failed: [] })
  const r = connectionRestarts({ restarted: [{ id: 'a', name: 'Lights' }, { name: 'no id' }], failed: [{ id: 'b', name: 'Heating', error: 'boom' }, { id: 'c' }] })
  expect(r).toEqual({ restarted: [{ id: 'a', name: 'Lights' }], failed: [{ id: 'b', name: 'Heating', error: 'boom' }, { id: 'c', name: 'c', error: '' }] })
  setLocale('en')
  expect(connectionSavedText('home', { restarted: [], failed: [] })).toEqual({ kind: 'success', text: 'Connection “home” saved.' })
  const saved = connectionSavedText('home', r)
  expect(saved.kind).toBe('warning')
  expect(saved.text).toContain('Restarted with the new settings: “Lights”.')
  expect(saved.text).toContain('Heating: boom')
})
