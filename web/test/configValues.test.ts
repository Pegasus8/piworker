import { expect, test } from 'bun:test'
import { configFieldsFromSchema, integerFromInput, jsonText, listDuplicates, listFromRows, listRows, moveItem, numberFromInput, objectFromRows, objectRows, parseJsonObject, setConfigValue, unreadableList } from '../src/lib/configValues'

test('a cleared number input is unset, not 0', () => {
  expect(numberFromInput('')).toBeUndefined()
  expect(numberFromInput('   ')).toBeUndefined()
  expect(numberFromInput('abc')).toBeUndefined()
  expect(numberFromInput('50')).toBe(50)
  expect(numberFromInput('-3.5')).toBe(-3.5)
  expect(numberFromInput('0')).toBe(0)
})

test('an unset value removes the key so the node uses its default', () => {
  const config: Record<string, unknown> = { timeout: 5000, url: 'https://example.com' }
  setConfigValue(config, 'timeout', undefined)
  expect('timeout' in config).toBe(false)
  setConfigValue(config, 'timeout', 0)
  expect(config.timeout).toBe(0)
  expect(JSON.stringify(config)).toBe('{"url":"https://example.com","timeout":0}')
})

test('object values are edited as name/value rows and stored as an object', () => {
  const rows = objectRows({ Authorization: 'Bearer {{secret.api_token}}', 'X-Count': 3 })
  expect(rows).toEqual([
    { key: 'Authorization', value: 'Bearer {{secret.api_token}}' },
    { key: 'X-Count', value: '3' }
  ])
  // Rows without a name are left out; the last duplicate name wins.
  expect(objectFromRows([...rows, { key: '  ', value: 'x' }, { key: ' X-Count ', value: '4' }]))
    .toEqual({ Authorization: 'Bearer {{secret.api_token}}', 'X-Count': '4' })
  expect(objectFromRows([{ key: '', value: '' }])).toBeUndefined()
})

test('a value that is not an object (an old free-text header) yields no rows', () => {
  expect(objectRows('Authorization: Bearer x')).toEqual([])
  expect(objectRows(['a'])).toEqual([])
  expect(objectRows(null)).toEqual([])
  expect(objectRows(undefined)).toEqual([])
})

test('schema types map to editor fields', () => {
  const fields = configFieldsFromSchema({
    required: ['url'],
    properties: {
      url: { type: 'string', title: 'URL' },
      headers: { type: 'object', title: 'Headers', description: 'HTTP headers' },
      timeout: { type: 'number', title: 'Timeout', default: 30000 },
      count: { type: 'integer', title: 'Count' },
      schema: { type: 'json', title: 'Schema', default: { type: 'object' } },
      enabled: { type: 'boolean' },
      method: { type: 'string', enum: ['GET', 'POST'] }
    }
  })
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]))
  expect(byKey.url.type).toBe('text')
  expect(byKey.url.required).toBe(true)
  expect(byKey.headers.type).toBe('keyvalue')
  expect(byKey.headers.placeholder).toBe('HTTP headers')
  expect(byKey.timeout.type).toBe('number')
  expect(byKey.timeout.default).toBe(30000)
  expect(byKey.count.type).toBe('integer')
  expect(byKey.schema.type).toBe('json')
  expect(byKey.schema.default).toEqual({ type: 'object' })
  expect(byKey.enabled.type).toBe('boolean')
  expect(byKey.enabled.label).toBe('enabled')
  expect(byKey.method.type).toBe('select')
  expect(byKey.method.options).toEqual([{ label: 'GET', value: 'GET' }, { label: 'POST', value: 'POST' }])
})

test('an integer input stores whole numbers', () => {
  expect(integerFromInput('')).toBeUndefined()
  expect(integerFromInput('  ')).toBeUndefined()
  expect(integerFromInput('abc')).toBeUndefined()
  expect(integerFromInput('3')).toBe(3)
  expect(integerFromInput('0')).toBe(0)
  // Fractions are cut toward zero, as the node itself would read them.
  expect(integerFromInput('3.7')).toBe(3)
  expect(integerFromInput('-2.5')).toBe(-2)
  expect(Number.isInteger(integerFromInput('10000.0'))).toBe(true)
})

test('a JSON setting is shown as indented JSON text', () => {
  expect(jsonText({ type: 'object', properties: {} })).toBe('{\n  "type": "object",\n  "properties": {}\n}')
  expect(jsonText(undefined)).toBe('')
  expect(jsonText(null)).toBe('')
  // Text saved by hand or by older editors is shown as it is.
  expect(jsonText('{"type":"object"}')).toBe('{"type":"object"}')
})

test('JSON text is stored as a parsed object', () => {
  expect(parseJsonObject('{"type": "object", "n": 1}')).toEqual({ value: { type: 'object', n: 1 } })
  expect(parseJsonObject('  ')).toEqual({ value: undefined })
  expect(parseJsonObject('')).toEqual({ value: undefined })
  for (const text of ['[1]', 'null', '"x"', '3', 'true']) {
    expect(parseJsonObject(text).error).toBe('Enter a JSON object, e.g. {"type": "object"}')
    expect(parseJsonObject(text).value).toBeUndefined()
  }
  const bad = parseJsonObject('{"type": ')
  expect(bad.error).toStartWith('Invalid JSON')
  expect(bad.value).toBeUndefined()
})

test('a string setting with a suggestion source keeps it, unknown sources are ignored', () => {
  const fields = configFieldsFromSchema({
    required: ['connection', 'device'],
    properties: {
      connection: { type: 'string', title: 'Connection', options: 'connections' },
      device: { type: 'string', title: 'Device', options: 'zigbee-devices' },
      property: { type: 'string', title: 'Property', options: 'zigbee-settable' },
      command: { type: 'string', enum: ['on', 'off'] },
      other: { type: 'string', options: 'not-a-source' }
    }
  })
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]))
  expect(byKey.connection.type).toBe('text')
  expect(byKey.connection.suggest).toBe('connections')
  expect(byKey.device.suggest).toBe('zigbee-devices')
  expect(byKey.property.suggest).toBe('zigbee-settable')
  // Enum choices stay in options; suggestions never turn a field into a select.
  expect(byKey.command.suggest).toBeUndefined()
  expect(byKey.command.options?.length).toBe(2)
  expect(byKey.other.suggest).toBeUndefined()
})

// List settings (ConfigProperty type "array", docs/design/llm.md §5): a list
// of strings edited as rows, such as the classifier's categories or the
// tools an agent node may call.
test('an array setting is a list field, with its suggestions per item', () => {
  const fields = configFieldsFromSchema({
    required: ['categories'],
    properties: {
      categories: { type: 'array', title: 'Categories', description: 'One port per category' },
      tools: { type: 'array', title: 'Tools', options: 'tools' },
      connection: { type: 'string', title: 'Connection', options: 'llm-connections' },
      model: { type: 'string', title: 'Model', options: 'llm-models' },
      collection: { type: 'string', title: 'Collection', options: 'knowledge-collections' }
    }
  })
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]))
  expect(byKey.categories.type).toBe('list')
  expect(byKey.categories.required).toBe(true)
  expect(byKey.categories.suggest).toBeUndefined()
  expect(byKey.tools.type).toBe('list')
  expect(byKey.tools.suggest).toBe('tools')
  expect(byKey.connection.suggest).toBe('llm-connections')
  expect(byKey.model.suggest).toBe('llm-models')
  expect(byKey.collection.suggest).toBe('knowledge-collections')
})

test('a list is edited as rows and stored trimmed, without blank items', () => {
  expect(listRows(['bug', 'question'])).toEqual(['bug', 'question'])
  // Items that are not text are shown as text.
  expect(listRows(['a', 3, true, null])).toEqual(['a', '3', 'true', ''])
  // Anything but a list (text saved by hand) yields no rows.
  expect(listRows('bug, question')).toEqual([])
  expect(listRows(undefined)).toEqual([])
  expect(listFromRows([' bug ', '', '   ', 'question'])).toEqual(['bug', 'question'])
  // No item left: unset, so the node uses its default (or is flagged as missing).
  expect(listFromRows(['', ' '])).toBeUndefined()
  expect(listFromRows([])).toBeUndefined()
})

test('a list value that is not a list is reported, an unset one is not', () => {
  expect(unreadableList('bug, question')).toBe(true)
  expect(unreadableList({ a: 1 })).toBe(true)
  expect(unreadableList(['bug'])).toBe(false)
  expect(unreadableList(undefined)).toBe(false)
  expect(unreadableList(null)).toBe(false)
  expect(unreadableList('')).toBe(false)
})

test('repeated items are named once each', () => {
  expect(listDuplicates(['bug', 'question', ' bug', 'question', 'praise', 'bug'])).toEqual(['bug', 'question'])
  expect(listDuplicates(['bug', '', ''])).toEqual([])
})

test('moving an item returns the reordered list', () => {
  const list = ['a', 'b', 'c']
  expect(moveItem(list, 0, 1)).toEqual(['b', 'a', 'c'])
  expect(moveItem(list, 2, 0)).toEqual(['c', 'a', 'b'])
  expect(list).toEqual(['a', 'b', 'c'])
  // Out of range: unchanged.
  expect(moveItem(list, 0, -1)).toEqual(['a', 'b', 'c'])
  expect(moveItem(list, 2, 3)).toEqual(['a', 'b', 'c'])
})
