import { msg } from '@/i18n'
// Importable example flows: a few that need no hardware, and Zigbee home
// automations (docs/design/zigbee.md, phase 3). Each template builds the flow JSON POST /api/flows/import
// accepts (the shape the flow export writes): nodes with their type,
// category, position, settings and ports, and connections between ports.
// test/examples.test.ts checks every template against the Go nodes.

export type ExampleParam = 'connection' | 'sensor' | 'plug' | 'location'

export interface ExampleParams {
  /** The broker connection of every Zigbee node. */
  connection: string
  /** Friendly name of a sensor (humidity, availability, battery). */
  sensor?: string
  /** Friendly name of a plug (or any switchable device). */
  plug?: string
  latitude?: number
  longitude?: number
}

export interface ExamplePort { id: string; name: string; dataType: string; multiple: boolean; required: boolean }

export interface ExampleNode {
  id: string
  type: string
  category: 'input' | 'processing' | 'output'
  position: { x: number; y: number }
  config: Record<string, unknown>
  enabled: true
  inputs: ExamplePort[]
  outputs: ExamplePort[]
}

export interface ExampleConnection { id: string; sourceNode: string; sourcePort: string; targetNode: string; targetPort: string }

export interface ExampleFlow {
  name: string
  description: string
  nodes: ExampleNode[]
  connections: ExampleConnection[]
}

export interface ExampleTemplate {
  id: string
  /** In the interface language, read when shown or built. */
  readonly title: string
  readonly description: string
  /** Needs a broker connection to Zigbee2MQTT. */
  zigbee: boolean
  /** What the dialog asks for besides the connection. */
  needs: Exclude<ExampleParam, 'connection'>[]
  build(p: Required<Pick<ExampleParams, 'connection'>> & ExampleParams): Pick<ExampleFlow, 'nodes' | 'connections'>
}

/** Buenos Aires, as the Sun node's own example. */
export const DEFAULT_LOCATION = { latitude: -34.6, longitude: -58.4 } as const

const port = (id: string, name: string, input: boolean): ExamplePort => ({ id, name, dataType: 'any', multiple: !input, required: input })
const input = [port('input', 'Input', true)]
const output = [port('output', 'Output', false)]

/** The ports of the node types the examples use (as declared in internal/node/builtin). */
export const EXAMPLE_PORTS: Record<string, { inputs: ExamplePort[]; outputs: ExamplePort[] }> = {
  'trigger-zigbee': { inputs: [], outputs: [port('output', 'State', false), port('availability', 'Availability', false)] },
  'trigger-sun': { inputs: [], outputs: output },
  'trigger-cron': { inputs: [], outputs: output },
  'trigger-interval': { inputs: [], outputs: output },
  'trigger-sysmetric': { inputs: [], outputs: output },
  'trigger-webhook': { inputs: [], outputs: output },
  'process-filter': { inputs: input, outputs: output },
  'process-timer': { inputs: input, outputs: [port('output', 'Elapsed', false)] },
  'action-zigbee': { inputs: input, outputs: output },
  'action-log': { inputs: input, outputs: output }
}

const COLUMN = 300
const ROW = 180

function node(id: string, type: string, column: number, row: number, config: Record<string, unknown>): ExampleNode {
  const ports = EXAMPLE_PORTS[type]
  if (!ports) throw new Error(`no ports for ${type}`)
  const category = type.startsWith('trigger-') ? 'input' : type.startsWith('action-') ? 'output' : 'processing'
  return {
    id, type, category,
    position: { x: 80 + column * COLUMN, y: 80 + row * ROW },
    config, enabled: true,
    inputs: ports.inputs.map(p => ({ ...p })),
    outputs: ports.outputs.map(p => ({ ...p }))
  }
}

function link(sourceNode: string, targetNode: string, sourcePort = 'output', targetPort = 'input'): ExampleConnection {
  return { id: `edge_${sourceNode}_${sourcePort}_${targetNode}`, sourceNode, sourcePort, targetNode, targetPort }
}

// A device name inside an expression: a quoted, escaped string literal.
const literal = (name: string) => JSON.stringify(name)
const text = () => msg().examples

export const EXAMPLES: ExampleTemplate[] = [
  // No hardware needed: they work on any PiWorker, right away.
  {
    id: 'heartbeat',
    get title() { return text().items.heartbeat.title },
    get description() { return text().items.heartbeat.description },
    zigbee: false,
    needs: [],
    build: () => ({
      nodes: [
        node('every_minute', 'trigger-interval', 0, 0, { interval: 60000 }),
        node('log', 'action-log', 1, 0, { level: 'info', message: text().items.heartbeat.log })
      ],
      connections: [link('every_minute', 'log')]
    })
  },
  {
    id: 'hot-pi',
    get title() { return text().items.hotPi.title },
    get description() { return text().items.hotPi.description },
    zigbee: false,
    needs: [],
    build: () => ({
      nodes: [
        node('temperature', 'trigger-sysmetric', 0, 0, { metric: 'temperature', comparison: 'above', threshold: 70, interval: 30 }),
        node('log', 'action-log', 1, 0, { level: 'warn', message: text().items.hotPi.log })
      ],
      connections: [link('temperature', 'log')]
    })
  },
  {
    id: 'webhook-log',
    get title() { return text().items.webhook.title },
    get description() { return text().items.webhook.description },
    zigbee: false,
    needs: [],
    build: () => ({
      nodes: [
        node('webhook', 'trigger-webhook', 0, 0, { path: 'hello' }),
        node('log', 'action-log', 1, 0, { level: 'info', message: text().items.webhook.log })
      ],
      connections: [link('webhook', 'log')]
    })
  },
  {
    id: 'humidity-dehumidifier',
    get title() { return text().items.humidity.title },
    get description() { return text().items.humidity.description },
    zigbee: true,
    needs: ['sensor', 'plug'],
    build: ({ connection, sensor = '', plug = '' }) => ({
      nodes: [
        node('humidity', 'trigger-zigbee', 0, 0.5, { connection, device: sensor, property: 'humidity', minChange: 1 }),
        node('too_humid', 'process-filter', 1, 0, { condition: 'payload.value >= 65' }),
        node('dry_enough', 'process-filter', 1, 1, { condition: 'payload.value <= 55' }),
        node('plug_on', 'action-zigbee', 2, 0, { connection, device: plug, command: 'on' }),
        node('plug_off', 'action-zigbee', 2, 1, { connection, device: plug, command: 'off' })
      ],
      connections: [link('humidity', 'too_humid'), link('humidity', 'dry_enough'), link('too_humid', 'plug_on'), link('dry_enough', 'plug_off')]
    })
  },
  {
    id: 'appliance-finished',
    get title() { return text().items.appliance.title },
    get description() { return text().items.appliance.description },
    zigbee: true,
    needs: ['plug'],
    build: ({ connection, plug = '' }) => ({
      nodes: [
        node('power', 'trigger-zigbee', 0, 0, { connection, device: plug, property: 'power', minChange: 5 }),
        node('finished', 'process-filter', 1, 0, { condition: 'payload.previous != nil && payload.previous > 50 && payload.value < 5' }),
        node('log', 'action-log', 2, 0, { level: 'info', message: text().items.appliance.log(plug) })
      ],
      connections: [link('power', 'finished'), link('finished', 'log')]
    })
  },
  {
    id: 'device-offline',
    get title() { return text().items.offline.title },
    get description() { return text().items.offline.description },
    zigbee: true,
    needs: ['sensor'],
    build: ({ connection, sensor = '' }) => ({
      nodes: [
        node('device', 'trigger-zigbee', 0, 0, { connection, device: sensor }),
        node('offline', 'process-filter', 1, 0, { condition: 'payload.online == false' }),
        node('log', 'action-log', 2, 0, { level: 'warn', message: text().items.offline.log(sensor) })
      ],
      connections: [link('device', 'offline', 'availability'), link('offline', 'log')]
    })
  },
  {
    id: 'porch-sunset',
    get title() { return text().items.porch.title },
    get description() { return text().items.porch.description },
    zigbee: true,
    needs: ['plug', 'location'],
    build: ({ connection, plug = '', latitude = DEFAULT_LOCATION.latitude, longitude = DEFAULT_LOCATION.longitude }) => ({
      nodes: [
        node('sunset', 'trigger-sun', 0, 0, { latitude, longitude, event: 'sunset', offset: -15 }),
        node('sunrise', 'trigger-sun', 0, 1, { latitude, longitude, event: 'sunrise' }),
        node('light_on', 'action-zigbee', 1, 0, { connection, device: plug, command: 'on' }),
        node('light_off', 'action-zigbee', 1, 1, { connection, device: plug, command: 'off' })
      ],
      connections: [link('sunset', 'light_on'), link('sunrise', 'light_off')]
    })
  },
  {
    id: 'auto-off',
    get title() { return text().items.autoOff.title },
    get description() { return text().items.autoOff.description },
    zigbee: true,
    needs: ['plug'],
    build: ({ connection, plug = '' }) => ({
      nodes: [
        node('state', 'trigger-zigbee', 0, 0, { connection, device: plug, property: 'state' }),
        node('turned_on', 'process-filter', 1, 0, { condition: 'payload.value == "ON"' }),
        node('wait', 'process-timer', 2, 0, { duration: 30, durationUnit: 'minutes', restart: false }),
        node('turn_off', 'action-zigbee', 3, 0, { connection, device: plug, command: 'off' })
      ],
      connections: [link('state', 'turned_on'), link('turned_on', 'wait'), link('wait', 'turn_off')]
    })
  },
  {
    id: 'low-battery',
    get title() { return text().items.battery.title },
    get description() { return text().items.battery.description },
    zigbee: true,
    needs: ['sensor'],
    build: ({ sensor = '' }) => ({
      nodes: [
        node('daily', 'trigger-cron', 0, 0, { expression: '0 9 * * *' }),
        node('low', 'process-filter', 1, 0, { condition: `(zigbee(${literal(sensor)})?.battery ?? 100) < 20` }),
        node('log', 'action-log', 2, 0, { level: 'warn', message: text().items.battery.log(sensor) })
      ],
      connections: [link('daily', 'low'), link('low', 'log')]
    })
  }
]

const validLatitude = (v: unknown) => typeof v === 'number' && Number.isFinite(v) && v >= -90 && v <= 90
const validLongitude = (v: unknown) => typeof v === 'number' && Number.isFinite(v) && v >= -180 && v <= 180

/** What the example still needs before it can be created (every Zigbee example needs a connection). */
export function missingParams(template: ExampleTemplate, params: ExampleParams): ExampleParam[] {
  const missing: ExampleParam[] = []
  if (template.zigbee && !params.connection?.trim()) missing.push('connection')
  for (const need of template.needs) {
    if (need === 'location') {
      if (!validLatitude(params.latitude) || !validLongitude(params.longitude)) missing.push('location')
    } else if (!params[need]?.trim()) missing.push(need)
  }
  return missing
}

/** The flow to import for a template, with the user's connection, devices and place. */
export function buildExampleFlow(template: ExampleTemplate, params: ExampleParams): ExampleFlow {
  const { nodes, connections } = template.build({
    ...params,
    connection: (params.connection ?? '').trim(),
    sensor: params.sensor?.trim(),
    plug: params.plug?.trim()
  })
  return { name: template.title, description: template.description, nodes, connections }
}
