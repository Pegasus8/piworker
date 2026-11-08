import { expect, test } from 'bun:test'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { HelpCircle, Reply, Wrench } from 'lucide-vue-next'
import { lucideIconName, nodeIcon } from '../src/lib/nodeIcons'

// The icon each builtin node declares in Go (NodeTypeInfo.Icon).
function goIcons(): Map<string, string> {
  const dir = join(import.meta.dir, '../../internal/node/builtin')
  const icons = new Map<string, string>()
  for (const file of readdirSync(dir)) {
    if (!file.endsWith('.go') || file.endsWith('_test.go')) continue
    for (const m of readFileSync(join(dir, file), 'utf8').matchAll(/Icon:\s*"([^"]+)"/g)) icons.set(m[1], file)
  }
  return icons
}

test('agent tool nodes show their own icons', () => {
  expect(nodeIcon(lucideIconName('wrench'))).toBe(Wrench)
  expect(nodeIcon(lucideIconName('reply'))).toBe(Reply)
})

test('every builtin node icon is shown on the canvas and in the palette', () => {
  const icons = goIcons()
  expect(icons.get('wrench')).toBe('trigger_tool.go')
  expect(icons.get('reply')).toBe('action_reply.go')
  const missing = [...icons].filter(([icon]) => nodeIcon(lucideIconName(icon)) === HelpCircle).map(([icon, file]) => `${icon} (${file})`)
  expect(missing).toEqual([])
})

test('unknown icons fall back to a help icon', () => {
  expect(lucideIconName('no-such-icon')).toBe('HelpCircle')
  expect(nodeIcon('NoSuchIcon')).toBe(HelpCircle)
  expect(nodeIcon('')).toBe(HelpCircle)
  // Names of Object.prototype members are not icons.
  expect(nodeIcon('constructor')).toBe(HelpCircle)
  expect(lucideIconName('constructor')).toBe('HelpCircle')
  expect(lucideIconName('toString')).toBe('HelpCircle')
})
