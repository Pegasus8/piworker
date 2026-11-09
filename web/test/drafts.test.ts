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
  diffBasis,
  diffSummary,
  draftIcon,
  draftState,
  draftTitle,
  draftVersionText,
  fieldLabel,
  fieldPaths,
  fieldRows,
  groupNodes,
  issueCounts,
  issueCountsLabel,
  proposedSettings,
  riskChange,
  riskInfo,
  severityInfo,
  textChanges,
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
    'secret_added', 'secret_moved', 'risk_switch_on', 'sensitive_literal', 'credentialed_request_changed',
    'rate_changed', 'global_variable_write', 'global_variable_read', 'tool_path_changed', 'data_read_changed']
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
  expect(alertKind('rate_changed')).toMatchObject({ icon: 'Gauge', label: 'Rate changed' })
  expect(alertKind('global_variable_write')).toMatchObject({ icon: 'DatabaseZap', label: 'Global variable written' })
  expect(alertKind('global_variable_read')).toMatchObject({ icon: 'Eye', label: 'Global variable read by a tool' })
  expect(alertKind('tool_path_changed')).toMatchObject({ icon: 'Route', label: 'Tool changed' })
  expect(alertKind('data_read_changed')).toMatchObject({ icon: 'ScanEye', label: 'Protected data read' })
  // Every icon name resolves to its own component, not the fallback.
  for (const kind of kinds) expect(draftIcon(alertKind(kind).icon)).not.toBe(draftIcon('no-such-icon'))
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
  const fields = new Map([['n1', new Set(['config.url'])]])
  expect(alertValues({ kind: 'destination_changed', nodeId: 'n1', path: 'config.url', message: 'm' }, { fields }))
    .toEqual({ before: undefined, after: undefined, inFieldDiff: true })
  expect(alertValues({ kind: 'risk_raised', message: 'm', before: 'safe', after: 'privileged' }))
    .toEqual({ before: 'safe', after: 'privileged', inFieldDiff: false })
  expect(alertValues({ kind: 'risk_switch_on', nodeId: 'n1', path: 'config.allowPrivate', message: 'm', after: true }))
    .toEqual({ before: undefined, after: 'true', inFieldDiff: false })
  expect(alertValues({ kind: 'secret_added', nodeId: 'n1', path: 'config.token', message: 'm', after: 'BOT' }).after).toBe('BOT')
  // Flow-level alerts without values have nothing to look up.
  expect(alertValues({ kind: 'risk_raised', message: 'm' }).inFieldDiff).toBe(false)
})

test('an alert points at the field changes only when the node diff has that field', () => {
  // Disabling an HTTP node raises destination_changed for its unchanged url
  // (not agent-visible: no values), while the node diff lists only enabled.
  const diff = emptyDiff({
    nodes: [{ nodeId: 'h1', type: 'action-http', typeName: 'HTTP Request', change: 'changed', moved: false,
      fields: [{ path: 'enabled', change: 'changed', before: true, after: false, sensitive: false, destination: false }] }]
  })
  const fields = fieldPaths(diff)
  expect([...fields.get('h1') ?? []]).toEqual(['enabled'])
  const gone = { kind: 'destination_changed', nodeId: 'h1', path: 'config.url', message: 'disables node h1' }
  expect(alertValues(gone, { fields }).inFieldDiff).toBe(false)
  // The editor may read the proposed flow (credentials already hidden):
  // the unchanged value is shown from it.
  const proposed = proposedSettings({ nodes: [{ id: 'h1', type: 'action-http', enabled: false, config: { url: 'https://a.example/hook' } }] })
  expect(alertValues(gone, { fields, proposed })).toEqual({
    before: undefined, after: undefined, inFieldDiff: false, unchanged: 'https://a.example/hook'
  })
  // Without the setting anywhere, nothing is claimed.
  expect(alertValues({ ...gone, path: 'config.host' }, { fields, proposed })).toEqual({
    before: undefined, after: undefined, inFieldDiff: false
  })
  // Without a context nothing is claimed either.
  expect(alertValues(gone).inFieldDiff).toBe(false)
  // A field row wins over the proposed flow's value.
  const both = fieldPaths(emptyDiff({ nodes: [{ nodeId: 'h1', type: 't', typeName: 'T', change: 'removed', moved: false,
    fields: [{ path: 'config.url', change: 'removed', before: 'https://a.example/hook', sensitive: false, destination: true }] }] }))
  expect(alertValues(gone, { fields: both, proposed })).toEqual({ before: undefined, after: undefined, inFieldDiff: true })
})

test('the proposed flow is read defensively, node by node', () => {
  const settings = proposedSettings({ nodes: [
    { id: 'a', config: { url: 'x', n: 2 } },
    { id: 'a', config: { url: 'second' } },
    { id: 'b' },
    { config: { url: 'no id' } },
    'junk'
  ] })
  expect(settings.get('a')).toEqual({ url: 'x', n: 2 })
  expect(settings.get('b')).toEqual({})
  expect(settings.size).toBe(2)
  expect(proposedSettings(null).size).toBe(0)
  expect(proposedSettings({ nodes: 'x' }).size).toBe(0)
  // Prototype names are settings like any other, not inherited members.
  expect(alertValues({ kind: 'destination_changed', nodeId: 'b', path: 'config.constructor', message: '' },
    { fields: new Map(), proposed: settings }).unchanged).toBeUndefined()
  expect(fieldPaths(null).size).toBe(0)
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
  // A rename alone is no node change: the summary says what it counts.
  expect(diffSummary(emptyDiff())).toBe('No node or connection changes')
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
  expect(draftVersionText(summary({ stale: true, currentVersion: 0 }))).toBe('Written for version 3; the flow was deleted')
})

test('a decided draft says which version it was written for and what became of it', () => {
  // The server marks only pending drafts stale: a decided one is never "the current one".
  const approved = summary({ status: 'approved', appliedVersion: 4, currentVersion: 4, decidedAt: '2026-11-08T13:00:00Z' })
  expect(draftVersionText(approved)).toBe('Written for version 3; saved as version 4')
  expect(draftVersionText({ ...approved, currentVersion: 6 })).toBe('Written for version 3; saved as version 4; the flow is now at version 6')
  expect(draftVersionText(summary({ status: 'rejected', currentVersion: 5 }))).toBe('Written for version 3; the flow is now at version 5')
  expect(draftVersionText(summary({ status: 'rejected' }))).toBe('Written for version 3')
  expect(draftVersionText(summary({ status: 'void', currentVersion: 0 }))).toBe('Written for version 3; the flow was deleted')
  expect(draftVersionText(summary({ status: 'void', currentVersion: 4 }))).toBe('Written for version 3; the flow is now at version 4')
  const created = summary({ status: 'approved', isNew: true, baseVersion: 0, appliedVersion: 1, currentVersion: 1 })
  expect(draftVersionText(created)).toBe('Written as a new flow; saved as version 1')
  expect(draftVersionText(summary({ status: 'rejected', isNew: true, baseVersion: 0, currentVersion: 0 }))).toBe('Written as a new flow')
})

test('what the risk, alerts and changes are compared with, for pending and decided drafts', () => {
  const pending = diffBasis(detail())
  expect(pending.heading).toBe('Changes compared with version 3 (current)')
  expect(pending.note).toBe('Compared with the current version, 3.')
  expect(pending.emptyAlerts).toContain('No alerts')
  expect(diffBasis(detail({ stale: true, currentVersion: 5 })).note)
    .toBe('Compared with the current version, 5, not version 3 the draft was written for.')
  const fresh = diffBasis(detail({ isNew: true, flowId: '', baseVersion: 0, currentVersion: 0 }))
  expect(fresh.heading).toBe('Changes compared with an empty flow')
  expect(fresh.note).toContain('creates a new flow')

  // An approved draft is recomputed against the version it became: its
  // changes are already part of it, so "no changes" must not read as "the
  // draft changed nothing".
  const approved = diffBasis(detail({ status: 'approved', appliedVersion: 4, currentVersion: 4 }))
  expect(approved.heading).toBe('Changes compared with version 4 (current, recomputed now)')
  expect(approved.note).toContain('Recomputed now against the current version, 4')
  expect(approved.note).toContain('saved as version 4')
  expect(approved.note).toContain('already part of it')
  expect(approved.emptyAlerts).toBe('No alerts against version 4: this draft\'s changes are already part of it.')
  const later = diffBasis(detail({ status: 'approved', appliedVersion: 4, currentVersion: 6 }))
  expect(later.emptyAlerts).toBe('No alerts against the current version, 6.')
  expect(later.note).toContain('saved as version 4')

  const rejected = diffBasis(detail({ status: 'rejected', currentVersion: 5 }))
  expect(rejected.heading).toBe('Changes compared with version 5 (current, recomputed now)')
  expect(rejected.note).toBe('Recomputed now against the current version, 5, not version 3 the draft was written for. Nothing from this draft was saved.')
  expect(rejected.emptyAlerts).toBe('No alerts against the current version, 5.')
  expect(diffBasis(detail({ status: 'void' })).note).toBe('Recomputed now against the current version, 3. Nothing from this draft was saved.')
  const deleted = diffBasis(detail({ status: 'void', currentVersion: 0 }))
  expect(deleted.heading).toBe('Changes compared with an empty flow')
  expect(deleted.note).toContain('no longer exists')
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

test('a draft its agent deployed itself never reads as one the admin approved', () => {
  const byAgent = draftState(summary({ status: 'approved', appliedVersion: 4, appliedBy: 'agent', decidedAt: '2026-11-08T13:00:00Z' }))
  expect(byAgent.label).toBe('Deployed by agent')
  expect(byAgent.tone).toBe('done')
  expect(byAgent.explanation).toBe(
    'Agent laptop deployed this draft itself as version 4 (the agent deploy level allows it); you did not review it. ' +
    'The version history can roll it back to the version you last approved.')
  expect(byAgent.explanation).not.toContain('You approved')
  expect(draftState(summary({ status: 'approved', appliedVersion: 4, appliedBy: 'editor' })).explanation)
    .toBe('You approved this draft; it was saved as version 4.')
  expect(approvalBlock(detail({ status: 'approved', appliedBy: 'agent' }), false)).toBe(
    'This draft was already deployed by its agent; there is nothing to approve.')
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

test('the approval confirmation also compares with the version you last approved', () => {
  // The current version 5 is an agent's, not approved: it added a host. The
  // draft changes little on top of it, so only diffApproved has alerts.
  const d = detail({
    baseVersion: 5, currentVersion: 5, lastApprovedVersion: 3,
    diff: emptyDiff({ riskBefore: 'external', riskAfter: 'external' }),
    diffApproved: emptyDiff({ riskBefore: 'safe', riskAfter: 'external', alerts: [
      { kind: 'new_host', nodeId: 'h', path: 'config.url', message: '', after: 'evil.example' },
      { kind: 'risk_raised', message: '', before: 'safe', after: 'external' }
    ] })
  })
  const text = approveConfirmText(d, false)
  expect(text).not.toContain('No alerts.')
  expect(text.startsWith('Compared with the current version, 5: Risk unchanged: External; no alerts. ' +
    'Compared with version 3, the one you last approved: Risk raised from Safe to External; 2 alerts: New host, Risk raised. ' +
    'Approving also approves these earlier agent changes.')).toBe(true)
  expect(text).toContain('saved as a new version')
  // Without alerts since the approved version, it says that too.
  const quiet = approveConfirmText({ ...d, diffApproved: emptyDiff({ riskBefore: 'external', riskAfter: 'external' }) }, false)
  expect(quiet).toContain('Compared with version 3, the one you last approved: Risk unchanged: External; no alerts.')
})

test('a rename and a new description are listed, and the confirmation names the new name', () => {
  expect(textChanges(detail())).toEqual([])
  expect(textChanges(detail({ nameChange: null, descriptionChange: null }))).toEqual([])
  const d = detail({ nameChange: { before: 'Garage', after: 'Gate‮' }, descriptionChange: { before: '', after: 'Opens it' } })
  expect(textChanges(d)).toEqual([
    { field: 'name', label: 'Name', before: 'Garage', after: 'Gate‮' },
    { field: 'description', label: 'Description', before: '', after: 'Opens it' }
  ])
  const text = approveConfirmText(d, false)
  expect(text).toContain('It renames the flow from “Garage” to “Gate\\u202E”.')
  expect(text).toContain('It changes the flow\'s description.')
  // A new flow's name is already in the sentence that creates it.
  const created = approveConfirmText(detail({ isNew: true, flowId: '', flowName: 'Lights', nameChange: { before: '', after: 'Lights' } }), false)
  expect(created).not.toContain('renames')
  expect(approveConfirmText(detail({ isNew: true, flowId: '', flowName: 'Lights', descriptionChange: { before: '', after: 'x' } }), false))
    .not.toContain('description')
})

test('the outcome of an approval or rollback says what was saved and whether it runs', () => {
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: true }, 'Approved')).toEqual({ kind: 'success', text: 'Approved: saved as version 4 and deployed.' })
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: false }, 'Approved')).toEqual({ kind: 'success', text: 'Approved: saved as version 4. It is not running.' })
  expect(applyOutcome({ flowId: 'f1', version: 4, deployed: false, deployError: { status: 400, message: 'bad‮' } }, 'Rolled back'))
    .toEqual({ kind: 'error', text: 'Rolled back: saved as version 4, but it could not be deployed: bad\\u202E' })
})
