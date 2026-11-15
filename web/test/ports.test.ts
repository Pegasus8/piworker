import { expect, test } from 'bun:test'
import { ERROR_PORT, HANDLE_SPACING, edgesAfterConfig, followPorts, nodeMinHeight, outputPorts, sourcePorts, validConnection } from '../src/lib/ports'
import { followRows, listFromRows, rowItems, type ItemRenames, type RowItem } from '../src/lib/configValues'
import type { FlowNode, NodeTypeConfig } from '../src/types'

const node = (id: string, nodeType: string) => ({ id, type: 'process', position: { x: 0, y: 0 }, data: { nodeType, config: {} } }) as unknown as FlowNode
const types: Record<string, Partial<NodeTypeConfig>> = {
  timer: { outputs: ['output'] as any, inputs: [] as any },
  log: { inputs: [{ id: 'input', name: 'Input', multiple: false }] as any, outputs: [] as any }
}
const getType = (id: string) => types[id] as NodeTypeConfig | undefined
const nodes = [node('a', 'timer'), node('b', 'log')]
const edge = { id: 'e1', source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }

test('a new connection that duplicates an existing one is refused', () => {
  expect(validConnection({ source: 'a', target: 'b', sourceHandle: 'output', targetHandle: 'input' }, nodes, [edge], getType)).toBe(false)
})

// Vue Flow re-validates the edges it is given with isValidConnection: an
// existing edge must not count as its own duplicate, or the canvas drops
// every connection (they were saved and ran, but were never drawn).
test('an existing edge is valid against the list that contains it', () => {
  expect(validConnection(edge, nodes, [edge], getType)).toBe(true)
})

// The error port (ADR-0036): every node that processes messages has it
// besides its declared outputs; a trigger has none.
test('the error port is a valid source on processing and action nodes only', () => {
  const kinds: Record<string, Partial<NodeTypeConfig>> = {
    timer: { category: 'trigger', outputs: ['output'] as any, inputs: [] as any },
    http: { category: 'action', inputs: ['input'] as any, outputs: ['output'] as any },
    log: { category: 'action', inputs: [{ id: 'input', name: 'Input' }] as any, outputs: [] as any }
  }
  const get = (id: string) => kinds[id] as NodeTypeConfig | undefined
  const all = [node('t', 'timer'), node('h', 'http'), node('l', 'log')]
  expect(validConnection({ source: 'h', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 't', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(false)
  expect(sourcePorts(get('http')).map(p => p.id)).toEqual(['output', ERROR_PORT])
  expect(sourcePorts(get('timer')).map(p => p.id)).toEqual(['output'])
  expect(sourcePorts(undefined)).toEqual([])
})

// Outputs from a setting (ADR-0040): the classifier's categories become
// ports after its static outputs, and the error port stays last.
const classify = { id: 'process-llm-classify', category: 'process', inputs: [{ id: 'input', name: 'Input' }], outputs: [{ id: 'other', name: 'Other' }], outputsFrom: 'categories' } as unknown as NodeTypeConfig

test('a setting adds one output port per item after the static outputs', () => {
  expect(outputPorts(classify, { categories: ['bug', 'question'] }).map(p => p.id)).toEqual(['other', 'bug', 'question'])
  expect(outputPorts(classify, { categories: ['bug'] }).map(p => p.name)).toEqual(['Other', 'bug'])
  expect(sourcePorts(classify, { categories: ['bug'] }).map(p => p.id)).toEqual(['other', 'bug', ERROR_PORT])
  // No config, an unset or unreadable setting: the static outputs only.
  expect(outputPorts(classify).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(classify, {}).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(classify, { categories: 'bug, question' }).map(p => p.id)).toEqual(['other'])
  expect(outputPorts(undefined, { categories: ['bug'] })).toEqual([])
})

// The same ports as the server's node.ConfigOutputs: empty, repeated and
// non-string items give none, and no item shadows a static port or the
// error port.
test('setting ports skip empty, repeated and reserved items', () => {
  const ids = outputPorts(classify, { categories: ['bug', '', 'bug', 3, 'other', ERROR_PORT, 'praise'] }).map(p => p.id)
  expect(ids).toEqual(['other', 'bug', 'praise'])
  expect(sourcePorts(classify, { categories: [ERROR_PORT] }).map(p => p.id)).toEqual(['other', ERROR_PORT])
})

test('a type without outputsFrom ignores the settings', () => {
  const http = { category: 'action', inputs: ['input'], outputs: ['output'] } as unknown as NodeTypeConfig
  expect(outputPorts(http, { categories: ['bug'] }).map(p => p.id)).toEqual(['output'])
})

test('a connection may leave a port the node settings give it', () => {
  const all = [{ ...node('c', 'classify'), data: { nodeType: 'classify', config: { categories: ['bug'] } } } as unknown as FlowNode, node('l', 'log')]
  const get = (id: string) => (id === 'classify' ? classify : types[id]) as NodeTypeConfig | undefined
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'bug', targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'other', targetHandle: 'input' }, all, [], get)).toBe(true)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: 'praise', targetHandle: 'input' }, all, [], get)).toBe(false)
  expect(validConnection({ source: 'c', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' }, all, [], get)).toBe(true)
})

// Editing the setting: the edges leaving a port that is gone are removed;
// an item renamed in place (typing in its row) keeps its edges.
test('edges follow the ports when the setting changes', () => {
  const edges = [
    { id: 'e1', source: 'c', target: 'l', sourceHandle: 'bug', targetHandle: 'input' },
    { id: 'e2', source: 'c', target: 'l', sourceHandle: 'question', targetHandle: 'input' },
    { id: 'e3', source: 'c', target: 'l', sourceHandle: 'other', targetHandle: 'input' },
    { id: 'e4', source: 'c', target: 'l', sourceHandle: ERROR_PORT, targetHandle: 'input' },
    { id: 'e5', source: 'x', target: 'c', sourceHandle: 'bug', targetHandle: 'input' }
  ]
  const before = { categories: ['bug', 'question'] }
  // Removed: its edge goes, the others stay (also edges of other nodes).
  expect(edgesAfterConfig('c', classify, before, { categories: ['question'] }, edges).map(e => e.id)).toEqual(['e2', 'e3', 'e4', 'e5'])
  // Renamed in place: the edge follows the new name.
  const renamed = edgesAfterConfig('c', classify, before, { categories: ['bugs', 'question'] }, edges)
  expect(renamed.find(e => e.id === 'e1')?.sourceHandle).toBe('bugs')
  expect(renamed).toHaveLength(5)
  // Reordered: every port is still there.
  expect(edgesAfterConfig('c', classify, before, { categories: ['question', 'bug'] }, edges)).toBe(edges)
  // Unchanged ports: the same array (nothing to record or sync).
  expect(edgesAfterConfig('c', classify, before, { ...before, instructions: 'x' }, edges)).toBe(edges)
  // A renamed item that now repeats another one is a removal.
  expect(edgesAfterConfig('c', classify, before, { categories: ['question', 'question'] }, edges).map(e => e.id)).toEqual(['e2', 'e3', 'e4', 'e5'])
  // Types without outputsFrom keep their edges.
  expect(edgesAfterConfig('c', types.log as NodeTypeConfig, before, {}, edges)).toBe(edges)
})

// Typing in a category's row as the editor does: ListInput says which item
// each row stands for (followRows) and the store follows the ports with the
// renames it reports (followPorts), keeping aside the connections of a row
// that is blank or repeats another for a moment.
type Edge = { id: string; source: string; target: string; sourceHandle: string; targetHandle: string }
function editor(categories: string[], edges: Edge[]) {
  let rows = [...categories]
  let items = rowItems(rows)
  let config: Record<string, unknown> = { categories: listFromRows(rows) }
  let parked: Edge[] = []
  const apply = (next: string[], kept: RowItem[], removed: RowItem[] = []) => {
    const followed = followRows(next, kept, removed)
    const renames: ItemRenames[] = followed.renamed.size ? [followed.renamed] : []
    const after = { categories: listFromRows(next) }
    const result = followPorts('c', classify, config, after, edges, parked, renames)
    rows = next
    items = followed.items
    config = after
    edges = result.edges
    parked = result.parked
  }
  return {
    type: (index: number, ...texts: string[]) => {
      for (const text of texts) apply(rows.map((r, i) => i === index ? text : r), items)
    },
    remove: (index: number) => apply(rows.filter((_, i) => i !== index), items.filter((_, i) => i !== index), [items[index]]),
    add: () => { rows = [...rows, '']; items = [...items, { name: null, own: false }] },
    port: (id: string) => edges.find(e => e.id === id)?.sourceHandle,
    ids: () => edges.map(e => e.id),
    parked: () => parked
  }
}
const edgeFrom = (id: string, port: string, target = 'l'): Edge => ({ id, source: 'c', target, sourceHandle: port, targetHandle: 'input' })

test('a category retyped through a blank row keeps its connections', () => {
  const ed = editor(['bug', 'question'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'question'), edgeFrom('e3', 'other')])
  ed.type(0, 'bu', 'b', '')
  // Blank: its port is gone, so is the connection, for now.
  expect(ed.ids()).toEqual(['e2', 'e3'])
  ed.type(0, 'd', 'de', 'defect')
  expect(ed.port('e1')).toBe('defect')
  expect(ed.port('e2')).toBe('question')
  expect(ed.parked()).toEqual([])
})

test('a category retyped through another category\'s name keeps its connections', () => {
  const ed = editor(['bug', 'bugs'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'bugs', 'm')])
  ed.type(1, 'bug')
  expect(ed.ids()).toEqual(['e1'])
  ed.type(1, 'bugf', 'bugfix')
  expect(ed.port('e2')).toBe('bugfix')
  expect(ed.port('e1')).toBe('bug')
  // Back to its own name after repeating another: the connection is back too.
  const back = editor(['bug', 'bugs'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'bugs', 'm')])
  back.type(1, 'bug', 'bugs')
  expect(back.port('e2')).toBe('bugs')
  // The first row renamed while the second repeats it: each keeps its own.
  const swap = editor(['bug', 'bugs'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'bugs', 'm')])
  swap.type(1, 'bug')
  swap.type(0, 'bu')
  expect(swap.port('e1')).toBe('bu')
  expect(swap.port('e2')).toBe('bug')
})

test('a row removed while blank or repeated takes its connections along for good', () => {
  const blank = editor(['bug', 'question'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'question')])
  blank.type(0, '')
  blank.remove(0)
  blank.add()
  blank.type(1, 'b', 'bug', 'bugs')
  expect(blank.ids()).toEqual(['e2'])
  expect(blank.parked()).toEqual([])
  // Removed while repeating another: only its own connections go.
  const repeated = editor(['bug', 'bugs'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'bugs', 'm')])
  repeated.type(1, 'bug')
  repeated.remove(0)
  expect(repeated.ids()).toEqual(['e2'])
  expect(repeated.port('e2')).toBe('bug')
})

test('a new category with the name of a removed one does not take its connections', () => {
  const ed = editor(['bug', 'question'], [edgeFrom('e1', 'bug'), edgeFrom('e2', 'question')])
  ed.remove(0)
  ed.add()
  ed.type(1, 'b', 'bu', 'bug')
  expect(ed.ids()).toEqual(['e2'])
})

test('a category never takes over the static outputs or the error port', () => {
  const ed = editor(['oth'], [edgeFrom('e1', 'oth'), edgeFrom('e2', 'other', 'm'), edgeFrom('e3', ERROR_PORT, 'n')])
  ed.type(0, 'othe', 'other')
  // "other" is the static output: the category's connection waits aside.
  expect(ed.ids()).toEqual(['e2', 'e3'])
  ed.type(0, 'others')
  expect(ed.port('e1')).toBe('others')
  expect(ed.port('e2')).toBe('other')
  expect(ed.port('e3')).toBe(ERROR_PORT)
  // Without the rows' reports, a rename to a static output is a removal too.
  const edges = [edgeFrom('e1', 'othe'), edgeFrom('e2', 'other', 'm')]
  expect(edgesAfterConfig('c', classify, { categories: ['othe'] }, { categories: ['other'] }, edges).map(e => [e.id, e.sourceHandle])).toEqual([['e2', 'other']])
})

test('a connection that comes back never repeats one drawn meanwhile', () => {
  const ed = editor(['bug'], [edgeFrom('e1', 'bug')])
  ed.type(0, '')
  ed.type(0, 'bug')
  expect(ed.ids()).toEqual(['e1'])
  const result = followPorts('c', classify, { categories: [] }, { categories: ['bug'] }, [edgeFrom('e9', 'bug')], [edgeFrom('e1', 'bug')], [new Map([['bug', 'bug']])])
  expect(result.edges.map(e => e.id)).toEqual(['e9'])
  expect(result.parked).toEqual([])
})

// Handles are spread evenly over a node's side: a classifier with many
// categories (up to 20 plus "other") grows tall enough that its 12px
// handles stay apart and each can be picked.
test('a node with many ports is tall enough to keep its handles apart', () => {
  for (const outputs of [1, 2, 8, 13, 21]) {
    const height = nodeMinHeight(1, outputs)
    expect(height / (outputs + 1)).toBeGreaterThanOrEqual(HANDLE_SPACING)
  }
  expect(HANDLE_SPACING).toBeGreaterThanOrEqual(18)
  expect(nodeMinHeight(4, 2)).toBe(nodeMinHeight(1, 4))
  // A node with a few ports keeps its natural height (header and summary).
  expect(nodeMinHeight(1, 2)).toBeLessThanOrEqual(80)
})
