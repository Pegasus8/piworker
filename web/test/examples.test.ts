import { expect, test } from 'bun:test'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { DEFAULT_LOCATION, EXAMPLES, EXAMPLE_PORTS, buildExampleFlow, missingParams, type ExampleParams } from '../src/lib/examples'

// The Go source of each builtin node type, to check the templates against
// the real settings and ports (read-only).
function goSources(): Map<string, string> {
  const dir = join(import.meta.dir, '../../internal/node/builtin')
  const files = readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go')).map(f => readFileSync(join(dir, f), 'utf8'))
  const common = files.filter(src => src.includes('func zigbeeSchemaConnection')).join('\n')
  const byType = new Map<string, string>()
  for (const src of files) {
    for (const m of src.matchAll(/Type:\s*"([a-z][a-z0-9-]*)"/g)) byType.set(m[1], src.includes('zigbeeSchemaConnection()') ? `${src}\n${common}` : src)
  }
  return byType
}

const params: ExampleParams = { connection: 'home', sensor: 'bedroom "climate"', plug: 'washer_plug', ...DEFAULT_LOCATION }

test('there are six examples with distinct ids and titles', () => {
  expect(EXAMPLES.length).toBe(6)
  expect(new Set(EXAMPLES.map(e => e.id)).size).toBe(6)
  expect(new Set(EXAMPLES.map(e => e.title)).size).toBe(6)
})

for (const example of EXAMPLES) {
  test(`${example.title}: unique ids, connections between declared ports`, () => {
    const flow = buildExampleFlow(example, params)
    expect(flow.name).toBe(example.title)
    const ids = flow.nodes.map(n => n.id)
    expect(new Set(ids).size).toBe(ids.length)
    const connIds = flow.connections.map(c => c.id)
    expect(new Set(connIds).size).toBe(connIds.length)
    expect(flow.connections.length).toBeGreaterThan(0)
    for (const c of flow.connections) {
      const source = flow.nodes.find(n => n.id === c.sourceNode)
      const target = flow.nodes.find(n => n.id === c.targetNode)
      expect(source).toBeDefined()
      expect(target).toBeDefined()
      expect(source!.outputs.map(p => p.id)).toContain(c.sourcePort)
      expect(target!.inputs.map(p => p.id)).toContain(c.targetPort)
    }
    // Every node is connected, and laid out left to right.
    for (const n of flow.nodes) expect(flow.connections.some(c => c.sourceNode === n.id || c.targetNode === n.id)).toBe(true)
    for (const c of flow.connections) {
      const source = flow.nodes.find(n => n.id === c.sourceNode)!
      const target = flow.nodes.find(n => n.id === c.targetNode)!
      expect(target.position.x).toBeGreaterThan(source.position.x)
    }
  })

  test(`${example.title}: types, settings and ports match the Go nodes`, () => {
    const sources = goSources()
    const flow = buildExampleFlow(example, params)
    for (const n of flow.nodes) {
      const src = sources.get(n.type)
      expect(src, n.type).toBeDefined()
      for (const port of [...n.inputs, ...n.outputs]) expect(src!).toMatch(new RegExp(`ID:\\s*"${port.id}"`))
      for (const key of Object.keys(n.config)) expect(src!, `${n.type}.${key}`).toMatch(new RegExp(`"${key}":\\s*(\\{|zigbeeSchema)`))
      const required = [...src!.matchAll(/Required:\s*\[\]string\{([^}]*)\}/g)].flatMap(m => [...m[1].matchAll(/"([^"]+)"/g)].map(r => r[1]))
      for (const key of required) expect(n.config[key], `${n.type} requires ${key}`).not.toBeUndefined()
      expect(n.category).toBe(n.type.startsWith('trigger-') ? 'input' : n.type.startsWith('action-') ? 'output' : 'processing')
      expect(EXAMPLE_PORTS[n.type]).toBeDefined()
      expect(n.enabled).toBe(true)
    }
  })
}

test('the chosen connection and devices are used, names escaped in expressions', () => {
  const humidity = buildExampleFlow(EXAMPLES.find(e => e.id === 'humidity-dehumidifier')!, params)
  const zigbeeNodes = humidity.nodes.filter(n => n.type.endsWith('-zigbee'))
  expect(zigbeeNodes.length).toBe(3)
  for (const n of zigbeeNodes) expect(n.config.connection).toBe('home')
  expect(humidity.nodes.find(n => n.type === 'trigger-zigbee')!.config).toEqual({ connection: 'home', device: 'bedroom "climate"', property: 'humidity', minChange: 1 })
  expect(humidity.nodes.filter(n => n.type === 'action-zigbee').map(n => n.config.command).sort()).toEqual(['off', 'on'])

  const battery = buildExampleFlow(EXAMPLES.find(e => e.id === 'low-battery')!, params)
  const filter = battery.nodes.find(n => n.type === 'process-filter')!
  expect(filter.config.condition).toBe('(zigbee("bedroom \\"climate\\"")?.battery ?? 100) < 20')
  expect(battery.nodes.find(n => n.type === 'trigger-cron')!.config.expression).toBe('0 9 * * *')

  const offline = buildExampleFlow(EXAMPLES.find(e => e.id === 'device-offline')!, params)
  expect(offline.connections.find(c => c.sourceNode === offline.nodes.find(n => n.type === 'trigger-zigbee')!.id)!.sourcePort).toBe('availability')

  const porch = buildExampleFlow(EXAMPLES.find(e => e.id === 'porch-sunset')!, { ...params, latitude: 51.5, longitude: -0.1 })
  const suns = porch.nodes.filter(n => n.type === 'trigger-sun').map(n => n.config)
  expect(suns).toEqual([
    { latitude: 51.5, longitude: -0.1, event: 'sunset', offset: -15 },
    { latitude: 51.5, longitude: -0.1, event: 'sunrise' }
  ])

  const timer = buildExampleFlow(EXAMPLES.find(e => e.id === 'auto-off')!, params)
  expect(timer.nodes.find(n => n.type === 'process-timer')!.config).toEqual({ duration: 30, durationUnit: 'minutes', restart: false })
  expect(timer.nodes.find(n => n.type === 'process-filter')!.config.condition).toBe('payload.value == "ON"')
})

test('missingParams names what an example still needs', () => {
  const porch = EXAMPLES.find(e => e.id === 'porch-sunset')!
  expect(missingParams(porch, { connection: '' })).toEqual(['connection', 'plug', 'location'])
  expect(missingParams(porch, { connection: 'home', plug: ' ', latitude: 0, longitude: 0 })).toEqual(['plug'])
  expect(missingParams(porch, { connection: 'home', plug: 'p', latitude: 91, longitude: 0 })).toEqual(['location'])
  expect(missingParams(porch, { connection: 'home', plug: 'p', latitude: -34.6, longitude: -58.4 })).toEqual([])
  const humidity = EXAMPLES.find(e => e.id === 'humidity-dehumidifier')!
  expect(missingParams(humidity, { connection: 'home', sensor: 's' })).toEqual(['plug'])
})

test('device names are trimmed', () => {
  const flow = buildExampleFlow(EXAMPLES.find(e => e.id === 'auto-off')!, { connection: ' home ', plug: '  washer_plug ' })
  for (const n of flow.nodes.filter(n => n.type.endsWith('-zigbee'))) {
    expect(n.config.connection).toBe('home')
    expect(n.config.device).toBe('washer_plug')
  }
})
