// Agent drafts (docs/design/mcp.md §10): the editor's REST payloads of
// /api/agent/drafts* and the pure presentation rules of the drafts pages.
// Everything an agent wrote (names, notes, node IDs, values, messages) is
// returned here as plain strings for text nodes (AgentText), never HTML;
// strings the helpers compose for titles and dialogs escape hidden
// characters with visibleText.
import type { Component } from 'vue'
import {
  AlertTriangle, ArrowRightLeft, Fingerprint, Globe, KeyRound, Lock, Send, Shield, ShieldAlert, ShieldCheck,
  ShieldQuestion, Shuffle, ToggleRight, TrendingUp, XCircle
} from 'lucide-vue-next'
import { jsonText, visibleText } from '@/lib/agentAccess'

export type Risk = 'safe' | 'external' | 'privileged'
export type DraftStatus = 'pending' | 'approved' | 'rejected' | 'void'
export type DraftFilter = 'pending' | 'decided' | 'all'
export type IssueSeverity = 'error' | 'warning'
export type Change = 'added' | 'removed' | 'changed'

/** A Lint or authoring issue (flow.Issue). */
export interface FlowIssue {
  path: string
  code: string
  severity: IssueSeverity
  message: string
}

export interface IssueCounts {
  error: number
  warning: number
}

/** A connection of the flow model (flow.Connection). */
export interface FlowConnection {
  id: string
  sourceNode: string
  sourcePort: string
  targetNode: string
  targetPort: string
}

/** One changed field of a node: "type", "enabled" or "config.<prop>". Before is absent when added, after when removed. */
export interface FieldDiff {
  path: string
  change: Change
  before?: unknown
  after?: unknown
  sensitive: boolean
  destination: boolean
}

/** An added, removed or changed node; a position-only change is "changed" with moved and no fields. */
export interface NodeDiff {
  nodeId: string
  type: string
  typeName: string
  change: Change
  fields: FieldDiff[] | null
  moved: boolean
}

/** A change the approval page flags (flow.Alert). Values only for settings agents may read (H7). */
export interface DraftAlert {
  kind: string
  nodeId?: string
  path?: string
  message: string
  before?: unknown
  after?: unknown
}

/** flow.DiffReport as GET /api/agent/drafts/{id} returns it. */
export interface DiffReport {
  nodes: NodeDiff[] | null
  connections: { added: FlowConnection[] | null; removed: FlowConnection[] | null }
  riskBefore: Risk | string
  riskAfter: Risk | string
  alerts: DraftAlert[] | null
}

/** One draft in GET /api/agent/drafts. */
export interface DraftSummary {
  id: string
  /** '' for a new flow. */
  flowId: string
  flowName: string
  isNew: boolean
  baseVersion: number
  currentVersion: number
  stale: boolean
  status: DraftStatus | string
  risk: Risk | string
  alertCount: number
  issueCounts: IssueCounts
  /** The first 200 characters of the agent's note. */
  note: string
  tokenId: string
  tokenName: string
  createdAt: string
  decidedAt: string | null
  reason?: string
  appliedVersion?: number
}

export interface DraftList {
  drafts: DraftSummary[]
  nextBefore: string
}

/** GET /api/agent/drafts/{id}: recomputed on read against the current version. */
export interface DraftDetail extends DraftSummary {
  /** The agent's whole note. */
  note: string
  issues: FlowIssue[] | null
  /** The flow runs: approving redeploys it whatever "deploy" says. */
  running: boolean
  diff: DiffReport
  /** The newest version you approved (0: none). */
  lastApprovedVersion: number
  /** The proposal against lastApprovedVersion, when that is not the current version. */
  diffApproved: DiffReport | null
  /** The proposed flow, Sensitive literals as "<hidden>". */
  flow: unknown
}

/** POST …/approve and POST /api/flows/{id}/rollback. */
export interface ApplyResult {
  flowId: string
  version: number
  deployed: boolean
  deployError?: { status: number; message: string }
}

/** What the server shows instead of a Sensitive value (flow.HiddenValue). */
export const HIDDEN_VALUE = '<hidden>'

const has = (o: object, key: string) => Object.prototype.hasOwnProperty.call(o, key)
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// --- Icons -----------------------------------------------------------------

const ICONS: Record<string, Component> = {
  AlertTriangle, ArrowRightLeft, Fingerprint, Globe, KeyRound, Lock, Send, Shield, ShieldAlert, ShieldCheck,
  ShieldQuestion, Shuffle, ToggleRight, TrendingUp, XCircle
}

/** The component of an icon name the helpers return (AlertTriangle when unknown). */
export function draftIcon(name: string): Component {
  return has(ICONS, name) ? ICONS[name] : AlertTriangle
}

// --- Alerts ----------------------------------------------------------------

export interface AlertKindInfo {
  /** A lucide icon name (draftIcon). */
  icon: string
  label: string
  /** What the alert means, in plain words. */
  description: string
}

/** Every alert kind flow.Diff raises (stable; the low agent_deploy level matches on them). */
export const ALERT_KINDS: Readonly<Record<string, AlertKindInfo>> = {
  risk_raised: {
    icon: 'TrendingUp', label: 'Risk raised',
    description: 'The flow as a whole can do more than before: its risk level is higher.'
  },
  privileged_node_added: {
    icon: 'ShieldAlert', label: 'Privileged node',
    description: 'A node that can run commands, write files, query databases or reach the hardware now runs.'
  },
  destination_changed: {
    icon: 'Send', label: 'Destination changed',
    description: 'Where a node sends data or connects changed: a URL, host, broker, topic, recipient, chat or file path.'
  },
  dynamic_destination: {
    icon: 'Shuffle', label: 'Dynamic destination',
    description: 'A destination is built from message data or variables, so where data goes can change while the flow runs.'
  },
  new_host: {
    icon: 'Globe', label: 'New host',
    description: 'The flow contacts a host that the compared version never contacts.'
  },
  secret_added: {
    icon: 'KeyRound', label: 'New secret used',
    description: 'The flow uses a secret that the compared version never uses.'
  },
  secret_moved: {
    icon: 'ArrowRightLeft', label: 'Secret used elsewhere',
    description: 'A secret is used by a node or setting that did not use it before, so it may be sent somewhere new.'
  },
  risk_switch_on: {
    icon: 'ToggleRight', label: 'Risk switch turned on',
    description: 'An opt-in that widens what a node may do (such as private network access or dash arguments) is on.'
  },
  sensitive_literal: {
    icon: 'Lock', label: 'Credential typed in',
    description: 'A credential setting holds a typed-in value instead of a {{secret.NAME}} reference.'
  },
  credentialed_request_changed: {
    icon: 'Fingerprint', label: 'Credential used differently',
    description: 'A node that uses a credential now sends a different request or gets new input, so the credential you approved is used for something else.'
  }
}

/** How an alert kind is shown; an unknown kind keeps its name, as text. */
export function alertKind(kind: string): AlertKindInfo {
  if (has(ALERT_KINDS, kind)) return ALERT_KINDS[kind]
  return {
    icon: 'AlertTriangle',
    label: visibleText(kind) || 'Alert',
    description: 'An alert this editor does not know yet: read its message.'
  }
}

/** Agent-provided data as text: strings as they are, anything else as JSON. */
function valueText(v: unknown): string {
  return typeof v === 'string' ? v : jsonText(v)
}

export interface AlertValues {
  before?: string
  after?: string
  /**
   * The alert is about a setting whose values the server keeps out of
   * alerts (not agent-visible, H7): the field diff below shows them.
   */
  inFieldDiff: boolean
}

/** The values an alert carries, as text for AgentText. */
export function alertValues(alert: DraftAlert): AlertValues {
  const before = alert.before === undefined || alert.before === null ? undefined : valueText(alert.before)
  const after = alert.after === undefined || alert.after === null ? undefined : valueText(alert.after)
  const onSetting = !!alert.nodeId && !!alert.path?.startsWith('config.')
  return { before, after, inFieldDiff: onSetting && before === undefined && after === undefined }
}

/** Node label for a node ID: "<type name> (<id>)" when its type is known, else "Node <id>". */
function nodeLabel(nodeId: string, names: ReadonlyMap<string, string>): string {
  const name = names.get(nodeId)
  return name ? `${name} (${nodeId})` : `Node ${nodeId}`
}

/** Where an alert is: its node and setting, or the whole flow. */
export function alertLocation(alert: DraftAlert, names: ReadonlyMap<string, string>): string {
  if (!alert.nodeId) return 'Whole flow'
  const node = nodeLabel(alert.nodeId, names)
  const { label, setting } = fieldLabel(alert.path ?? '')
  return alert.path && setting ? `${node} · setting ${label}` : node
}

/** Alerts of each node, in the server's order (flow-level ones are left out). */
export function alertsByNode(alerts: readonly DraftAlert[] | null | undefined): Map<string, DraftAlert[]> {
  const out = new Map<string, DraftAlert[]>()
  for (const a of alerts ?? []) {
    if (!a.nodeId) continue
    const list = out.get(a.nodeId)
    if (list) list.push(a)
    else out.set(a.nodeId, [a])
  }
  return out
}

// --- Risk ------------------------------------------------------------------

export interface RiskInfo {
  label: string
  icon: string
  variant: 'outline' | 'secondary' | 'destructive'
  rank: number
}

const RISK_INFO: Record<string, RiskInfo> = {
  safe: { label: 'Safe', icon: 'ShieldCheck', variant: 'outline', rank: 0 },
  external: { label: 'External', icon: 'Shield', variant: 'secondary', rank: 1 },
  privileged: { label: 'Privileged', icon: 'ShieldAlert', variant: 'destructive', rank: 2 }
}

/** How a risk level is shown (an unknown value reads as the highest). */
export function riskInfo(risk: string): RiskInfo {
  return has(RISK_INFO, risk) ? RISK_INFO[risk] : { label: 'Unknown risk', icon: 'ShieldQuestion', variant: 'destructive', rank: 3 }
}

export interface RiskChange {
  direction: 'raised' | 'lowered' | 'same'
  text: string
}

/** The risk before → after, in words. */
export function riskChange(before: string, after: string): RiskChange {
  const b = riskInfo(before)
  const a = riskInfo(after)
  if (a.rank > b.rank) return { direction: 'raised', text: `Risk raised from ${b.label} to ${a.label}` }
  if (a.rank < b.rank) return { direction: 'lowered', text: `Risk lowered from ${b.label} to ${a.label}` }
  return { direction: 'same', text: `Risk unchanged: ${a.label}` }
}

// --- Diff ------------------------------------------------------------------

export interface NodeGroup {
  change: Change
  label: string
  nodes: NodeDiff[]
}

const GROUPS: { change: Change; label: string }[] = [
  { change: 'added', label: 'Added nodes' },
  { change: 'removed', label: 'Removed nodes' },
  { change: 'changed', label: 'Changed nodes' }
]

/** The node diff by kind of change (non-empty groups only), each in the server's order. */
export function groupNodes(diff: DiffReport): NodeGroup[] {
  const nodes = diff.nodes ?? []
  return GROUPS
    .map(g => ({ ...g, nodes: nodes.filter(n => n.change === g.change) }))
    .filter(g => g.nodes.length > 0)
}

/** "2 nodes added, 1 node changed, 1 connection added", or "No changes". */
export function diffSummary(diff: DiffReport): string {
  const nodes = diff.nodes ?? []
  const parts: string[] = []
  for (const change of ['added', 'removed', 'changed'] as const) {
    const n = nodes.filter(d => d.change === change).length
    if (n) parts.push(`${plural(n, 'node')} ${change}`)
  }
  const added = diff.connections?.added?.length ?? 0
  const removed = diff.connections?.removed?.length ?? 0
  if (added) parts.push(`${plural(added, 'connection')} added`)
  if (removed) parts.push(`${plural(removed, 'connection')} removed`)
  return parts.length ? parts.join(', ') : 'No changes'
}

/** A field path as a label: "config.url" → setting "url". */
export function fieldLabel(path: string): { label: string; setting: boolean } {
  if (path.startsWith('config.')) return { label: path.slice('config.'.length), setting: true }
  if (path === 'enabled') return { label: 'Enabled', setting: false }
  if (path === 'type') return { label: 'Node type', setting: false }
  return { label: path, setting: false }
}

export interface FieldValue {
  /** The value as text, or "hidden" for a Sensitive value. */
  text: string
  hidden: boolean
}

export interface FieldRow {
  path: string
  label: string
  setting: boolean
  change: Change
  /** null when the field was added (before) or removed (after). */
  before: FieldValue | null
  after: FieldValue | null
  sensitive: boolean
  destination: boolean
}

function fieldValue(v: unknown, present: boolean, sensitive: boolean): FieldValue | null {
  if (!present) return null
  if (sensitive && v === HIDDEN_VALUE) return { text: 'hidden', hidden: true }
  return { text: valueText(v), hidden: false }
}

/** The field-level before/after of a node, ready for text nodes. */
export function fieldRows(node: NodeDiff): FieldRow[] {
  return (node.fields ?? []).map(f => {
    const { label, setting } = fieldLabel(f.path)
    return {
      path: f.path,
      label,
      setting,
      change: f.change,
      before: fieldValue(f.before, f.change !== 'added' && f.before !== undefined, f.sensitive),
      after: fieldValue(f.after, f.change !== 'removed' && f.after !== undefined, f.sensitive),
      sensitive: f.sensitive,
      destination: f.destination
    }
  })
}

/** "Webhook (a) · output → Telegram (b) · input". */
export function connectionText(c: FlowConnection, names: ReadonlyMap<string, string>): string {
  const end = (id: string, port: string) => {
    const name = names.get(id)
    return `${name ? `${name} (${id})` : id} · ${port}`
  }
  return `${end(c.sourceNode, c.sourcePort)} → ${end(c.targetNode, c.targetPort)}`
}

/** Node type names by node ID, from the diffs (for alert locations and connections). */
export function nodeNames(...diffs: (DiffReport | null | undefined)[]): Map<string, string> {
  const names = new Map<string, string>()
  for (const d of diffs) for (const n of d?.nodes ?? []) if (!names.has(n.nodeId)) names.set(n.nodeId, n.typeName || n.type)
  return names
}

// --- Issues ----------------------------------------------------------------

export function issueCounts(issues: readonly FlowIssue[] | null | undefined): IssueCounts {
  const counts = { error: 0, warning: 0 }
  for (const i of issues ?? []) {
    if (i.severity === 'error') counts.error++
    else counts.warning++
  }
  return counts
}

/** "1 error, 2 warnings", or "No issues". */
export function issueCountsLabel(c: IssueCounts): string {
  const parts: string[] = []
  if (c.error) parts.push(plural(c.error, 'error'))
  if (c.warning) parts.push(plural(c.warning, 'warning'))
  return parts.length ? parts.join(', ') : 'No issues'
}

export function severityInfo(severity: string): { icon: string; label: string } {
  return severity === 'error' ? { icon: 'XCircle', label: 'Error' } : { icon: 'AlertTriangle', label: 'Warning' }
}

/**
 * Lint's code that depends on the secret store rather than the definition
 * (flow.EnvironmentIssue): it blocks a deploy, not saving a draft.
 */
export const ENVIRONMENT_ISSUE_CODES: readonly string[] = ['undefined_secret']

// --- Drafts ----------------------------------------------------------------

/** The draft's flow, for titles: an existing flow's name, or the proposed name of a new one. */
export function draftTitle(d: Pick<DraftSummary, 'flowId' | 'flowName' | 'isNew'>): string {
  const name = visibleText(d.flowName)
  if (d.isNew) return `New flow “${name}”`
  return name || `Flow ${visibleText(d.flowId)}`
}

/** Which version a draft changes, and whether the flow moved on since. */
export function draftVersionText(d: Pick<DraftSummary, 'isNew' | 'baseVersion' | 'currentVersion' | 'stale'>): string {
  if (d.isNew) return 'Creates a new flow'
  if (d.stale) return `Written for version ${d.baseVersion}; the flow is now at version ${d.currentVersion}`
  return `Changes version ${d.baseVersion} (the current one)`
}

export interface DraftState {
  label: string
  /** pending: can be decided; blocked: pending but cannot be approved; done: decided. */
  tone: 'pending' | 'blocked' | 'done'
  explanation: string
}

/** A draft's state in words. */
export function draftState(d: Pick<DraftSummary, 'status' | 'stale' | 'baseVersion' | 'currentVersion' | 'appliedVersion'>): DraftState {
  switch (d.status) {
    case 'pending':
      if (d.stale) {
        return {
          label: 'Out of date', tone: 'blocked',
          explanation: `The agent wrote this draft for version ${d.baseVersion}, but the flow is now at version ${d.currentVersion}. ` +
            'It cannot be approved: ask the agent to redo it on the current version, or reject it.'
        }
      }
      return { label: 'Waiting for review', tone: 'pending', explanation: 'Nothing changes until you approve it.' }
    case 'approved':
      return {
        label: 'Approved', tone: 'done',
        explanation: d.appliedVersion ? `You approved this draft; it was saved as version ${d.appliedVersion}.` : 'You approved this draft.'
      }
    case 'rejected':
      return { label: 'Rejected', tone: 'done', explanation: 'You rejected this draft. The agent sees the reason.' }
    case 'void':
      return {
        label: 'Void', tone: 'done',
        explanation: 'This draft was closed without a decision (its token was revoked or its flow deleted). Nothing was saved.'
      }
    default:
      return { label: visibleText(String(d.status)), tone: 'done', explanation: 'This draft can no longer be approved.' }
  }
}

const STATUS_WORDS: Record<string, string> = { approved: 'approved', rejected: 'rejected', void: 'void' }

/**
 * Why approving would be refused, or '' when it would not (the server
 * decides in the end). deploy: whether "Deploy now" is checked; a running
 * flow is always redeployed.
 */
export function approvalBlock(d: DraftDetail, deploy: boolean): string {
  if (d.status !== 'pending') {
    const word = STATUS_WORDS[d.status] ?? 'decided'
    return word === 'void'
      ? 'This draft is void: it was closed without a decision, so there is nothing to approve.'
      : `This draft was already ${word}; there is nothing to approve.`
  }
  if (d.stale) {
    return `This draft is out of date: the flow is now at version ${d.currentVersion}. Ask the agent to redo it, or reject it.`
  }
  const errors = (d.issues ?? []).filter(i => i.severity === 'error')
  const blocking = errors.filter(i => !ENVIRONMENT_ISSUE_CODES.includes(i.code))
  if (blocking.length) {
    return `The draft has ${plural(blocking.length, 'error')} (listed under Issues), so it cannot be saved. Ask the agent to fix them, or reject it.`
  }
  if (errors.length && (deploy || d.running)) {
    const why = d.running ? 'The flow is running, so approving redeploys it, but' : 'Deploying needs'
    const tail = d.running
      ? 'it uses secrets that are not defined (listed under Issues). Define them in Secrets first.'
      : 'secrets that are not defined (listed under Issues). Define them in Secrets first, or approve without deploying.'
    return `${why} ${tail}`
  }
  return ''
}

/** The confirmation text of an approval: the risk and the alerts first, then what happens. */
export function approveConfirmText(d: DraftDetail, deploy: boolean): string {
  const risk = riskChange(String(d.diff.riskBefore), String(d.diff.riskAfter)).text
  const counts = new Map<string, number>()
  for (const a of d.diff.alerts ?? []) counts.set(a.kind, (counts.get(a.kind) ?? 0) + 1)
  const alerts = counts.size
    ? `${plural(d.diff.alerts!.length, 'alert')}: ${[...counts].map(([kind, n]) => n > 1 ? `${alertKind(kind).label} (${n})` : alertKind(kind).label).join(', ')}.`
    : 'No alerts.'
  const name = visibleText(d.flowName) || visibleText(d.flowId)
  const saved = d.isNew
    ? `“${name}” is created as a new flow from this draft by “${visibleText(d.tokenName || d.tokenId)}”.`
    : `“${name}” is saved as a new version from this draft by “${visibleText(d.tokenName || d.tokenId)}”.`
  const runs = d.running
    ? 'The flow is running, so it is redeployed with this version.'
    : deploy ? 'It is deployed right away.' : 'It is not deployed.'
  return `${risk}. ${alerts} ${saved} ${runs}`
}

/** What an approval or rollback did, for OperationFeedback. */
export function applyOutcome(r: ApplyResult, action: string): { kind: 'success' | 'error'; text: string } {
  if (r.deployError) {
    return { kind: 'error', text: `${action}: saved as version ${r.version}, but it could not be deployed: ${visibleText(r.deployError.message)}` }
  }
  if (r.deployed) return { kind: 'success', text: `${action}: saved as version ${r.version} and deployed.` }
  return { kind: 'success', text: `${action}: saved as version ${r.version}. It is not running.` }
}
