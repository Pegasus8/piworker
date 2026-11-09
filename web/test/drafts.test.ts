import { expect, test } from 'bun:test'
import {
  ALERT_KINDS,
  alertKind,
  alertLocation,
  alertValues,
  alertsByNode,
  applyOutcome,
  approvalBlock,
  approveConfirmText,
  connectionText,
  diffSummary,
  draftState,
  draftTitle,
  draftVersionText,
  fieldLabel,
  fieldRows,
  groupNodes,
  issueCounts,
  issueCountsLabel,
  riskChange,
  riskInfo,
  severityInfo,
  HIDDEN_VALUE,
  type DiffReport,
  type DraftDetail,
  type DraftSummary,
  type NodeDiff
} from '../src/lib/drafts'

const summary = (over: Partial<DraftSummary> = {}): DraftSummary => ({
  id: 'd1', flowId: 'f1', flowName: 'Garage', isNew: false, baseVersion: 3, currentVersion: 3, stale: false,
  status: 'pending', risk: 'external', alertCount: 0, issueCounts: { error: 0, warning: 0 }, note: 'Adds a notification',
  tokenId: 'tokA', tokenName: 'laptop', createdAt: '2026-11-08T12:00:00Z', decidedAt: null, ...over
})

const emptyDiff = (over: Partial<DiffReport> = {}): DiffReport => ({
  nodes: [], connections: { added: [], removed: [] }, riskBefore: 'safe', riskAfter: 'safe', alerts: [], ...over
})

const detail = (over: Partial<DraftDetail> = {}): DraftDetail => ({
  ...summary(), issues: [], running: false, diff: emptyDiff(), lastApprovedVersion: 3, diffApproved: null, flow: null, ...over
})

test('every alert kind the server raises has its own icon, label and plain explanation', () => {
  const kinds = ['risk_raised', 'privileged_node_added', 'destination_changed', 'dynamic_destination', 'new_host',
    'secret_added', 'secret_moved', 'risk_switch_on', 'sensitive_literal', 'credentialed_request_changed']
  expect(Object.keys(ALERT_KINDS).sort()).toEqual([...kinds].sort())
  const icons = new Set<string>()
  const labels = new Set<string>()
  for (const kind of kinds) {
    const info = alertKind(kind)
    expect(info.icon).not.toBe('AlertTriangle')
    expect(info.label.length).toBeGreaterThan(3)
    expect(info.description.length).toBeGreaterThan(20)
    icons.add(info.icon)
    labels.add(info.label)
  }
  // Color is never the only signal: each kind is told apart by icon and text.
  expect(icons.size).toBe(kinds.length)
  expect(labels.size).toBe(kinds.length)
  expect(alertKind('credentialed_request_changed')).toMatchObject({ icon: 'Fingerprint', label: 'Credential used differently' })
  expect(alertKind('new_host').icon).toBe('Globe')
})

test('an alert kind this editor does not know is still shown, as text', () => {
  const info = alertKind('future_kind‮')
  expect(info.icon).toBe('AlertTriangle')
  expect(info.label).toBe('future_kind\\u202E')
  expect(info.description).toContain('message')
  // Object prototype names are not alert kinds.
  expect(alertKind('constructor').icon).toBe('AlertTriangle')
})

test('alert values are shown only when the server sent them (agent-visible values)', () => {
  expect(alertValues({ kind: 'destination_changed', nodeId: 'n1', path: 'config.topic', message: 'm', before: 'a/b', after: 'a/c' }))
    .toEqual({ before: 'a/b', after: 'a/c', inFieldDiff: false })
  // H7: a hidden setting's alert has no values; the field diff shows them.
  expect(alertValues({ kind: 'destination_changed', nodeId: 'n1', path: 'config.url', message: 'm' }))
    .toEqual({ before: undefined, after: undefined, inFieldDiff: true })
  expect(alertValues({ kind: 'risk_raised', message: 'm', before: 'safe', after: 'privileged' }))
    .toEqual({ before: 'safe', after: 'privileged', inFieldDiff: false })
  expect(alertValues({ kind: 'risk_switch_on', nodeId: 'n1', path: 'config.allowPrivate', message: 'm', after: true }))
    .toEqual({ before: undefined, after: 'true', inFieldDiff: false })
  expect(alertValues({ kind: 'secret_added', nodeId: 'n1', path: 'config.token', message: 'm', after: 'BOT' }).after).toBe('BOT')
  // Flow-level alerts without values have nothing to look up.
  expect(alertValues({ kind: 'risk_raised', message: 'm' }).inFieldDiff).toBe(false)
})

test('alerts name their node and setting, or the whole flow', () => {
  const nodes = new Map([['n1', 'HTTP Request']])
  expect(alertLocation({ kind: 'new_host', nodeId: 'n1', path: 'config.url', message: '' }, nodes)).toBe('HTTP Request (n1) · setting url')
  expect(alertLocation({ kind: 'privileged_node_added', nodeId: 'n2', message: '' }, nodes)).toBe('Node n2')
  expect(alertLocation({ kind: 'risk_raised', message: '' }, nodes)).toBe('Whole flow')
  expect(alertLocation({ kind: 'credentialed_request_changed', nodeId: 'n1', message: '' }, nodes)).toBe('HTTP Request (n1)')
})

test('alerts are grouped by node for the per-node diff', () => {
  const alerts = [
    { kind: 'risk_raised', message: '' },
    { kind: 'new_host', nodeId: 'n1', path: 'config.url', message: '' },
    { kind: 'destination_changed', nodeId: 'n1', path: 'config.url', message: '' },
    { kind: 'secret_added', nodeId: 'n2', path: 'config.token', message: '' }
  ]
  const byNode = alertsByNode(alerts)
  expect(byNode.get('n1')?.map(a => a.kind)).toEqual(['new_host', 'destination_changed'])
  expect(byNode.get('n2')?.map(a => a.kind)).toEqual(['secret_added'])
  expect(byNode.has('')).toBe(false)
})

test('risk is told by icon and text, and its change in words', () => {
  expect(riskInfo('safe')).toMatchObject({ label: 'Safe', icon: 'ShieldCheck' })
  expect(riskInfo('external')).toMatchObject({ label: 'External', icon: 'Shield' })
  expect(riskInfo('privileged')).toMatchObject({ label: 'Privileged', icon: 'ShieldAlert', variant: 'destructive' })
  expect(riskInfo('weird')).toMatchObject({ label: 'Unknown risk', icon: 'ShieldQuestion', variant: 'destructive' })
  expect(riskChange('safe', 'privileged')).toEqual({ direction: 'raised', text: 'Risk raised from Safe to Privileged' })
  expect(riskChange('privileged', 'external')).toEqual({ direction: 'lowered', text: 'Risk lowered from Privileged to External' })
  expect(riskChange('external', 'external')).toEqual({ direction: 'same', text: 'Risk unchanged: External' })
})

test('the node diff is grouped into added, removed and changed nodes in server order', () => {
  const node = (nodeId: string, change: NodeDiff['change'], moved = false): NodeDiff =>
    ({ nodeId, type: 'action-http', typeName: 'HTTP Request', change, fields: moved ? [] : [{ path: 'config.url', change: 'changed', before: 'a', after: 'b', sensitive: false, destination: true }], moved })
  const diff = emptyDiff({ nodes: [node('a', 'added'), node('b', 'changed'), node('c', 'removed'), node('d', 'added'), node('e', 'changed', true)] })
  const groups = groupNodes(diff)
  expect(groups.map(g => [g.change, g.label, g.nodes.map(n => n.nodeId)])).toEqual([
    ['added', 'Added nodes', ['a', 'd']],
    ['removed', 'Removed nodes', ['c']],
    ['changed', 'Changed nodes', ['b', 'e']]
  ])
  // Empty groups are left out; a null node list (an older server) is none.
  expect(groupNodes(emptyDiff({ nodes: [node('x', 'removed')] })).map(g => g.change)).toEqual(['removed'])
  expect(groupNodes({ ...emptyDiff(), nodes: null as any })).toEqual([])
})

test('a diff is summarised in words', () => {
  const nodes: NodeDiff[] = [
    { nodeId: 'a', type: 't', typeName: 'T', change: 'added', fields: [], moved: false },
    { nodeId: 'b', type: 't', typeName: 'T', change: 'added', fields: [], moved: false },
    { nodeId: 'c', type: 't', typeName: 'T', change: 'changed', fields: [], moved: true }
  ]
  const conn = { id: 'c1', sourceNode: 'a', sourcePort: 'output', targetNode: 'b', targetPort: 'input' }
  expect(diffSummary(emptyDiff({ nodes, connections: { added: [conn], removed: null as any } })))
    .toBe('2 nodes added, 1 node changed, 1 connection added')
  expect(diffSummary(emptyDiff())).toBe('No changes')
})

test('field rows label settings, hide Sensitive values and flag destinations', () => {
  const rows = fieldRows({
    nodeId: 'n1', type: 'action-http', typeName: 'HTTP Request', change: 'changed', moved: false,
    fields: [
      { path: 'config.url', change: 'changed', before: 'https://a.example/x', after: 'https://b.example/y', sensitive: false, destination: true },
      { path: 'config.token', change: 'changed', before: HIDDEN_VALUE, after: '{{secret.API}}', sensitive: true, destination: false },
      { path: 'config.headers', change: 'added', after: { 'X-A': '1' }, sensitive: false, destination: false },
      { path: 'enabled', change: 'changed', before: true, after: false, sensitive: false, destination: false },
      { path: 'config.note', change: 'changed', before: HIDDEN_VALUE, after: 'x', sensitive: false, destination: false }
    ]
  })
  expect(rows.map(r => r.label)).toEqual(['url', 'token', 'headers', 'Enabled', 'note'])
  expect(rows[0]).toMatchObject({ before: { text: 'https://a.example/x', hidden: false }, after: { text: 'https://b.example/y', hidden: false }, destination: true })
  // A Sensitive value reads as hidden; a secret reference shows its name.
  expect(rows[1].before).toEqual({ text: 'hidden', hidden: true })
  expect(rows[1].after).toEqual({ text: '{{secret.API}}', hidden: false })
  // Added: no before. Non-strings as JSON text.
  expect(rows[2].before).toBeNull()
  expect(rows[2].after?.text).toBe('{\n  "X-A": "1"\n}')
  expect(rows[3].after).toEqual({ text: 'false', hidden: false })
  // Only a Sensitive field's placeholder means hidden; elsewhere it is the text that runs.
  expect(rows[4].before).toEqual({ text: HIDDEN_VALUE, hidden: false })
  // Null fields (an older server) are no rows.
  expect(fieldRows({ nodeId: 'n', type: 't', typeName: 'T', change: 'changed', moved: true, fields: null as any })).toEqual([])
})

test('field paths read as settings', () => {
  expect(fieldLabel('config.url')).toEqual({ label: 'url', setting: true })
  expect(fieldLabel('config.a.b')).toEqual({ label: 'a.b', setting: true })
  expect(fieldLabel('enabled')).toEqual({ label: 'Enabled', setting: false })
  expect(fieldLabel('type')).toEqual({ label: 'Node type', setting: false })
  expect(fieldLabel('other')).toEqual({ label: 'other', setting: false })
})

test('connections read as source port to target port', () => {
  const names = new Map([['a', 'Webhook'], ['b', 'Telegram']])
  expect(connectionText({ id: 'c', sourceNode: 'a', sourcePort: 'output', targetNode: 'b', targetPort: 'input' }, names))
    .toBe('Webhook (a) · output → Telegram (b) · input')
  expect(connectionText({ id: 'c', sourceNode: 'x', sourcePort: 'true', targetNode: 'y', targetPort: 'input' }, new Map()))
    .toBe('x · true → y · input')
})

test('issues are counted and labelled by severity', () => {
  const issues = [
    { path: '/nodes/a', code: 'unknown_type', severity: 'error' as const, message: 'm' },
    { path: '/nodes/b', code: 'tool_delay_exceeds_timeout', severity: 'warning' as const, message: 'm' },
    { path: '/nodes/c', code: 'x', severity: 'warning' as const, message: 'm' }
  ]
  expect(issueCounts(issues)).toEqual({ error: 1, warning: 2 })
  expect(issueCounts(null)).toEqual({ error: 0, warning: 0 })
  expect(issueCountsLabel({ error: 1, warning: 2 })).toBe('1 error, 2 warnings')
  expect(issueCountsLabel({ error: 0, warning: 1 })).toBe('1 warning')
  expect(issueCountsLabel({ error: 0, warning: 0 })).toBe('No issues')
  expect(severityInfo('error')).toMatchObject({ icon: 'XCircle', label: 'Error' })
  expect(severityInfo('warning')).toMatchObject({ icon: 'AlertTriangle', label: 'Warning' })
})

test('draft titles and versions say whether the flow is new and whether the draft is out of date', () => {
  expect(draftTitle(summary())).toBe('Garage')
  expect(draftTitle(summary({ isNew: true, flowId: '', flowName: 'Lights​' }))).toBe('New flow “Lights\\u200B”')
  expect(draftTitle(summary({ flowName: '' }))).toBe('Flow f1')
  expect(draftVersionText(summary())).toBe('Changes version 3 (the current one)')
  expect(draftVersionText(summary({ stale: true, currentVersion: 5 }))).toBe('Written for version 3; the flow is now at version 5')
  expect(draftVersionText(summary({ isNew: true, baseVersion: 0, currentVersion: 0 }))).toBe('Creates a new flow')
})

test('each draft state is explained in words', () => {
  expect(draftState(summary())).toMatchObject({ label: 'Waiting for review', tone: 'pending' })
  const stale = draftState(summary({ stale: true, currentVersion: 5 }))
  expect(stale).toMatchObject({ label: 'Out of date', tone: 'blocked' })
  expect(stale.explanation).toContain('version 5')
  expect(stale.explanation).toContain('redo')
  expect(draftState(summary({ status: 'approved', appliedVersion: 4, decidedAt: '2026-11-08T13:00:00Z' })))
    .toMatchObject({ label: 'Approved', tone: 'done' })
  expect(draftState(summary({ status: 'approved', appliedVersion: 4 })).explanation).toContain('version 4')
  expect(draftState(summary({ status: 'rejected', reason: 'Too broad' })).explanation).toBe('You rejected this draft. The agent sees the reason.')
  expect(draftState(summary({ status: 'void', reason: 'token revoked' })))
    .toMatchObject({ label: 'Void', tone: 'done' })
  expect(draftState(summary({ status: 'void' })).explanation).toContain('closed without a decision')
  // A stale state only matters while the draft could still be approved.
  expect(draftState(summary({ status: 'rejected', stale: true })).label).toBe('Rejected')
})

test('approving is blocked, with the reason, when the server would refuse it', () => {
  expect(approvalBlock(detail(), false)).toBe('')
  expect(approvalBlock(detail({ status: 'approved' }), false)).toContain('already approved')
  expect(approvalBlock(detail({ status: 'void' }), false)).toContain('void')
  expect(approvalBlock(detail({ stale: true, currentVersion: 4 }), false)).toContain('version 4')
  const error = { path: '/nodes/a', code: 'unknown_type', severity: 'error' as const, message: 'm' }
  const secret = { path: '/nodes/a/config/token', code: 'undefined_secret', severity: 'error' as const, message: 'm' }
  const warning = { path: '/nodes/a', code: 'w', severity: 'warning' as const, message: 'm' }
  expect(approvalBlock(detail({ issues: [warning] }), true)).toBe('')
  expect(approvalBlock(detail({ issues: [error] }), false)).toContain('1 error')
  // An undefined secret blocks only a deploy: saving the draft is fine.
  expect(approvalBlock(detail({ issues: [secret] }), false)).toBe('')
  expect(approvalBlock(detail({ issues: [secret] }), true)).toContain('Secrets')
  // A running flow is always redeployed.
  expect(approvalBlock(detail({ issues: [secret], running: true }), false)).toContain('running')
})

test('the approval confirmation repeats the risk and the alerts first', () => {
  const d = detail({
    flowName: 'Garage‮', running: true,
    diff: emptyDiff({ riskBefore: 'safe', riskAfter: 'external', alerts: [
      { kind: 'new_host', nodeId: 'n', path: 'config.url', message: '' },
      { kind: 'destination_changed', nodeId: 'n', path: 'config.url', message: '' },
      { kind: 'new_host', nodeId: 'm', path: 'config.url', message: '' }
    ] })
  })
  const text = approveConfirmText(d, false)
  expect(text.startsWith('Risk raised from Safe to External. 3 alerts: New host (2), Destination changed.')).toBe(true)
  expect(text).toContain('Garage\\u202E')
  expect(text).toContain('running, so it is redeployed')
  expect(approveConfirmText(detail(), true)).toContain('No alerts.')
  expect(approveConfirmText(detail(), true)).toContain('deployed right away')
  expect(approveConfirmText(detail(), false)).toContain('not deployed')
  expect(approveConfirmText(detail({ isNew: true, flowId: '', flowName: 'Lights' }), false)).toContain('created as a new flow')
})

test('the outcome of an approval or rollback says what was saved and whether it runs', () => {
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: true }, 'Approved')).toEqual({ kind: 'success', text: 'Approved: saved as version 4 and deployed.' })
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: false }, 'Approved')).toEqual({ kind: 'success', text: 'Approved: saved as version 4. It is not running.' })
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: false, deployError: { status: 400, message: 'bad‮' } }, 'Rolled back'))
    .toEqual({ kind: 'error', text: 'Rolled back: saved as version 4, but it could not be deployed: bad\\u202E' })
})
