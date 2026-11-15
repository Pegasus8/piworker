import { expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { parse } from 'vue/compiler-sfc'

// A live region reads out every change of its text: a counter that ticks
// every second inside one is announced every second for minutes. The
// template is read as written (no DOM needed).
type Node = { type: number; tag?: string; props?: { type: number; name: string; value?: { content: string } }[]; children?: Node[]; content?: { content?: string } }
const attr = (node: Node, name: string) => node.props?.find(p => p.type === 6 && p.name === name)?.value?.content

// The interpolations inside live regions that assistive technology reads (not under aria-hidden="true").
function announced(node: Node, live = false, hidden = false, found: string[] = []): string[] {
  if (node.type === 1) {
    hidden ||= attr(node, 'aria-hidden') === 'true'
    live ||= !!attr(node, 'aria-live') || attr(node, 'role') === 'status'
  }
  if (node.type === 5 && live && !hidden) found.push(node.content?.content?.trim() ?? '')
  for (const child of node.children ?? []) announced(child, live, hidden, found)
  return found
}
function template(file: string): Node {
  const { descriptor } = parse(readFileSync(join(import.meta.dir, '../src', file), 'utf8'))
  return descriptor.template!.ast as unknown as Node
}

test('the assistant does not announce its elapsed-seconds counter', () => {
  const read = announced(template('components/flow/AssistantDialog.vue'))
  // The region still says that the model is being asked.
  expect(read).toContain('t.generating')
  expect(read.filter(text => text.includes('elapsed'))).toEqual([])
})
