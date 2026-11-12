import { expect, test } from 'bun:test'
import { nodeSummary } from '../src/lib/nodeSummary'
import type { ConfigField } from '../src/types'

const f = (key: string, extra: Partial<ConfigField> = {}): ConfigField => ({ key, label: key, type: 'text', ...extra })

test('required values come first, durations read naturally', () => {
  const schema = [f('interval', { type: 'number', required: true, unit: 'milliseconds', default: 1000 })]
  expect(nodeSummary({ interval: 60000 }, schema)).toEqual({ text: '1 min', missing: [] })
})
test('secrets, switches and structured settings are never shown', () => {
  const schema = [f('botToken', { required: true, sensitive: true }), f('chatId', { required: true }), f('silent', { type: 'boolean' }), f('headers', { type: 'keyvalue' })]
  expect(nodeSummary({ botToken: 'abc', chatId: '42', silent: true, headers: { a: 'b' } }, schema).text).toBe('42')
})
test('optional values equal to their default add nothing', () => {
  const schema = [f('name', { default: 'Debug' }), f('level', { type: 'select', default: 'info' })]
  expect(nodeSummary({ name: 'Debug', level: 'warn' }, schema).text).toBe('warn')
})
test('at most two values, long ones shortened', () => {
  const schema = [f('a', { required: true }), f('b', { required: true }), f('c', { required: true })]
  const long = 'x'.repeat(60)
  expect(nodeSummary({ a: long, b: 'two', c: 'three' }, schema).text).toBe(`${'x'.repeat(31)}… · two`)
})
test('empty required settings are reported by label', () => {
  const schema = [f('url', { required: true, label: 'URL' }), f('method', { default: 'GET' })]
  expect(nodeSummary({ url: '  ' }, schema)).toEqual({ text: '', missing: ['URL'] })
})
