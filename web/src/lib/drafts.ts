// Agent drafts (docs/design/mcp.md §10): the editor's REST payloads of
// /api/agent/drafts* and the pure presentation rules of the drafts pages.
// Everything an agent wrote (names, notes, node IDs, values, messages) is
// returned here as plain strings for text nodes (AgentText), never HTML;
// strings the helpers compose for titles and dialogs escape hidden
// characters with visibleText.
import type { Component } from 'vue'
import {
  AlertTriangle, ArrowRightLeft, DatabaseZap, Eye, Fingerprint, Gauge, Globe, KeyRound, Lock, Route, ScanEye, Send, Shield,
  ShieldAlert, ShieldCheck, ShieldQuestion, Shuffle, ToggleRight, TrendingUp, XCircle
} from 'lucide-vue-next'
import { jsonText, visibleText } from '@/lib/agentAccess'
import { msg } from '@/i18n'

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
  /**
   * Who applied an approved draft: 'editor' (you approved it) or 'agent'
   * (its token deployed it at the low or all agent deploy level, without a
   * review). Absent when not applied.
   */
  appliedBy?: 'editor' | 'agent' | string
}

export interface DraftList {
  drafts: DraftSummary[]
  nextBefore: string
}

/** A text field a draft changes (agent text). */
export interface TextChange {
  before: string
  after: string
}

/** GET /api/agent/drafts/{id}: recomputed on read against the current version. */
export interface DraftDetail extends DraftSummary {
  /** The agent's whole note. */
  note: string
  issues: FlowIssue[] | null
  /** The flow runs: approving redeploys it whatever "deploy" says. */
  running: boolean
  /** The proposed name and description where they differ from the current version's (null: unchanged). */
  nameChange: TextChange | null
  descriptionChange: TextChange | null
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

// --- Icons -----------------------------------------------------------------

const ICONS: Record<string, Component> = {
  AlertTriangle, ArrowRightLeft, DatabaseZap, Eye, Fingerprint, Gauge, Globe, KeyRound, Lock, Route, ScanEye, Send, Shield,
  ShieldAlert, ShieldCheck, ShieldQuestion, Shuffle, ToggleRight, TrendingUp, XCircle
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

const ALERT_ICONS: Readonly<Record<string, string>> = {
  risk_raised: 'TrendingUp',
  privileged_node_added: 'ShieldAlert',
  destination_changed: 'Send',
  dynamic_destination: 'Shuffle',
  new_host: 'Globe',
  secret_added: 'KeyRound',
  secret_moved: 'ArrowRightLeft',
  risk_switch_on: 'ToggleRight',
  sensitive_literal: 'Lock',
  credentialed_request_changed: 'Fingerprint',
  rate_changed: 'Gauge',
  global_variable_write: 'DatabaseZap',
  global_variable_read: 'Eye',
  tool_path_changed: 'Route',
  data_read_changed: 'ScanEye'
}

const alertText = (kind: string) =>
  (msg().drafts.alertKinds as Record<string, { label: string; description: string }>)[kind]

/**
 * Every alert kind flow.Diff raises (stable; the low agent_deploy level
 * matches on them). Labels and descriptions follow the interface language.
 */
export const ALERT_KINDS: Readonly<Record<string, AlertKindInfo>> = Object.fromEntries(
  Object.entries(ALERT_ICONS).map(([kind, icon]) => [kind, {
    icon,
    get label() { return alertText(kind).label },
    get description() { return alertText(kind).description }
  }])
)

/** How an alert kind is shown; an unknown kind keeps its name, as text. */
export function alertKind(kind: string): AlertKindInfo {
  if (has(ALERT_KINDS, kind)) return ALERT_KINDS[kind]
  return {
    icon: 'AlertTriangle',
    label: visibleText(kind) || msg().drafts.unknownAlert,
    description: msg().drafts.unknownAlertDescription
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
   * alerts (not agent-visible, H7), and the node's field diff (of the same
   * comparison) has a row for it: that row shows them.
   */
  inFieldDiff: boolean
  /**
   * The setting has no field row because its value did not change (a node
   * was disabled or enabled): its value in the proposed flow, which the
   * editor may read (credentials there are already "<hidden>").
   */
  unchanged?: string
}

/** What an alert's values are looked up in: the field rows of its comparison and the proposed flow. */
export interface AlertContext {
  /** Node ID → the field paths its NodeDiff lists (fieldPaths of the same DiffReport). */
  fields?: ReadonlyMap<string, ReadonlySet<string>>
  /** Node ID → its settings in the proposed flow (proposedSettings). */
  proposed?: ReadonlyMap<string, Readonly<Record<string, unknown>>>
}

/** The values an alert carries, as text for AgentText, and where else to find them. */
export function alertValues(alert: DraftAlert, ctx: AlertContext = {}): AlertValues {
  const before = alert.before === undefined || alert.before === null ? undefined : valueText(alert.before)
  const after = alert.after === undefined || alert.after === null ? undefined : valueText(alert.after)
  const out: AlertValues = { before, after, inFieldDiff: false }
  if (!alert.nodeId || !alert.path?.startsWith('config.') || before !== undefined || after !== undefined) return out
  if (ctx.fields?.get(alert.nodeId)?.has(alert.path)) {
    out.inFieldDiff = true
    return out
  }
  const settings = ctx.proposed?.get(alert.nodeId)
  const key = alert.path.slice('config.'.length)
  if (settings && has(settings, key) && settings[key] !== undefined && settings[key] !== null) out.unchanged = valueText(settings[key])
  return out
}

/** The field paths each node's diff lists, by node ID. */
export function fieldPaths(diff: DiffReport | null | undefined): Map<string, Set<string>> {
  const out = new Map<string, Set<string>>()
  for (const n of diff?.nodes ?? []) {
    const paths = out.get(n.nodeId) ?? new Set<string>()
    for (const f of n.fields ?? []) paths.add(f.path)
    out.set(n.nodeId, paths)
  }
  return out
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

/** The proposed flow's settings by node ID (the first node of an ID); anything malformed is skipped. */
export function proposedSettings(flow: unknown): Map<string, Record<string, unknown>> {
  const out = new Map<string, Record<string, unknown>>()
  const nodes = isRecord(flow) ? flow.nodes : undefined
  if (!Array.isArray(nodes)) return out
  for (const n of nodes) {
    if (!isRecord(n) || typeof n.id !== 'string' || out.has(n.id)) continue
    out.set(n.id, isRecord(n.config) ? n.config : {})
  }
  return out
}

/** Node label for a node ID: "<type name> (<id>)" when its type is known, else "Node <id>". */
function nodeLabel(nodeId: string, names: ReadonlyMap<string, string>): string {
  const name = names.get(nodeId)
  return name ? `${name} (${nodeId})` : msg().drafts.nodeLabel(nodeId)
}

/** Where an alert is: its node and setting, or the whole flow. */
export function alertLocation(alert: DraftAlert, names: ReadonlyMap<string, string>): string {
  if (!alert.nodeId) return msg().drafts.wholeFlow
  const node = nodeLabel(alert.nodeId, names)
  const { label, setting } = fieldLabel(alert.path ?? '')
  return alert.path && setting ? msg().drafts.onSetting(node, label) : node
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

const RISK_INFO: Record<string, Omit<RiskInfo, 'label'>> = {
  safe: { icon: 'ShieldCheck', variant: 'outline', rank: 0 },
  external: { icon: 'Shield', variant: 'secondary', rank: 1 },
  privileged: { icon: 'ShieldAlert', variant: 'destructive', rank: 2 }
}

/** How a risk level is shown (an unknown value reads as the highest). */
export function riskInfo(risk: string): RiskInfo {
  const t = msg().drafts.risk
  if (has(RISK_INFO, risk)) return { label: t[risk as 'safe' | 'external' | 'privileged'], ...RISK_INFO[risk] }
  return { label: t.unknown, icon: 'ShieldQuestion', variant: 'destructive', rank: 3 }
}

export interface RiskChange {
  direction: 'raised' | 'lowered' | 'same'
  text: string
}

/** The risk before → after, in words. */
export function riskChange(before: string, after: string): RiskChange {
  const b = riskInfo(before)
  const a = riskInfo(after)
  const t = msg().drafts.risk
  if (a.rank > b.rank) return { direction: 'raised', text: t.raised(b.label, a.label) }
  if (a.rank < b.rank) return { direction: 'lowered', text: t.lowered(b.label, a.label) }
  return { direction: 'same', text: t.same(a.label) }
}

// --- Diff ------------------------------------------------------------------

export interface NodeGroup {
  change: Change
  label: string
  nodes: NodeDiff[]
}

const GROUPS: Change[] = ['added', 'removed', 'changed']

/** The node diff by kind of change (non-empty groups only), each in the server's order. */
export function groupNodes(diff: DiffReport): NodeGroup[] {
  const nodes = diff.nodes ?? []
  const labels = msg().drafts.groups
  return GROUPS
    .map(change => ({ change, label: labels[change], nodes: nodes.filter(n => n.change === change) }))
    .filter(g => g.nodes.length > 0)
}

/** "2 nodes added, 1 node changed, 1 connection added", or "No node or connection changes". */
export function diffSummary(diff: DiffReport): string {
  const t = msg().drafts
  const nodes = diff.nodes ?? []
  const parts: string[] = []
  for (const change of ['added', 'removed', 'changed'] as const) {
    const n = nodes.filter(d => d.change === change).length
    if (n) parts.push(t.nodesChanged(n, change))
  }
  const added = diff.connections?.added?.length ?? 0
  const removed = diff.connections?.removed?.length ?? 0
  if (added) parts.push(t.connectionsAdded(added))
  if (removed) parts.push(t.connectionsRemoved(removed))
  return parts.length ? parts.join(', ') : t.noChanges
}

/** A field path as a label: "config.url" → setting "url". */
export function fieldLabel(path: string): { label: string; setting: boolean } {
  if (path.startsWith('config.')) return { label: path.slice('config.'.length), setting: true }
  if (path === 'enabled') return { label: msg().drafts.fieldEnabled, setting: false }
  if (path === 'type') return { label: msg().drafts.fieldType, setting: false }
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
  if (sensitive && v === HIDDEN_VALUE) return { text: msg().drafts.hidden, hidden: true }
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

export interface TextChangeRow extends TextChange {
  field: 'name' | 'description'
  label: string
}

/** The flow's name and description the draft changes, for the changes section. */
export function textChanges(d: Pick<DraftDetail, 'nameChange' | 'descriptionChange'>): TextChangeRow[] {
  const rows: TextChangeRow[] = []
  const t = msg().drafts
  if (d.nameChange) rows.push({ field: 'name', label: t.fieldName, before: d.nameChange.before, after: d.nameChange.after })
  if (d.descriptionChange) rows.push({ field: 'description', label: t.fieldDescription, ...d.descriptionChange })
  return rows
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
  const t = msg().drafts
  if (c.error) parts.push(t.errors(c.error))
  if (c.warning) parts.push(t.warnings(c.warning))
  return parts.length ? parts.join(', ') : t.noIssues
}

export function severityInfo(severity: string): { icon: string; label: string } {
  const t = msg().drafts
  return severity === 'error' ? { icon: 'XCircle', label: t.severityError } : { icon: 'AlertTriangle', label: t.severityWarning }
}

/**
 * Lint's codes that depend on the secrets and broker connections defined
 * rather than on the definition (flow.EnvironmentIssue): the errors block a
 * deploy, not saving a draft; unresolved_secret is only a warning.
 */
export const ENVIRONMENT_ISSUE_CODES: readonly string[] = ['undefined_secret', 'unresolved_secret', 'undefined_connection']

// --- Drafts ----------------------------------------------------------------

/** The draft's flow, for titles: an existing flow's name, or the proposed name of a new one. */
export function draftTitle(d: Pick<DraftSummary, 'flowId' | 'flowName' | 'isNew'>): string {
  const name = visibleText(d.flowName)
  if (d.isNew) return msg().drafts.newFlowTitle(name)
  return name || msg().drafts.flowTitle(visibleText(d.flowId))
}

/** Where the flow is now, for a draft of an existing flow (currentVersion 0: deleted). */
const flowNow = (current: number) => current ? msg().drafts.flowNowAt(current) : msg().drafts.flowDeleted

/**
 * Which version a draft changes and what became of it. The server marks
 * only pending drafts stale, so a decided draft never reads as changing
 * "the current one": it says what it was written for, the version an
 * approval saved, and where the flow is now when that differs.
 */
export function draftVersionText(
  d: Pick<DraftSummary, 'isNew' | 'baseVersion' | 'currentVersion' | 'stale' | 'status' | 'appliedVersion'>
): string {
  const t = msg().drafts
  if (d.status === 'pending') {
    if (d.isNew) return t.createsNewFlow
    if (d.stale) return `${t.writtenFor(d.baseVersion)}; ${flowNow(d.currentVersion)}`
    return t.changesCurrent(d.baseVersion)
  }
  const parts = [d.isNew ? t.writtenAsNew : t.writtenFor(d.baseVersion)]
  const applied = d.status === 'approved' && d.appliedVersion ? d.appliedVersion : 0
  if (applied) parts.push(t.savedAsVersion(applied))
  // The version the text names last; a new flow that was never created has none to compare.
  const named = applied || (d.isNew ? 0 : d.baseVersion)
  const neverCreated = d.isNew && !d.currentVersion
  if (d.currentVersion !== named && !neverCreated) parts.push(flowNow(d.currentVersion))
  return parts.join('; ')
}

export interface DiffBasis {
  /** The heading of the changes section. */
  heading: string
  /** What the risk, the alerts and the changes are compared with, in words. */
  note: string
  /** The alerts panel's text when there are none. */
  emptyAlerts: string
}

/**
 * What a draft's diff is compared with. The server recomputes it on read
 * against the current version (an empty flow for a new one, or a deleted
 * one): for a decided draft that is not the version it was written for,
 * and an approved draft's changes are already part of the version it became.
 */
export function diffBasis(d: DraftDetail): DiffBasis {
  const t = msg().drafts
  const empty = d.isNew || !d.currentVersion
  if (empty) {
    return {
      heading: t.emptyHeading,
      note: d.isNew ? t.emptyNoteNew : t.emptyNoteDeleted,
      emptyAlerts: t.noAlerts
    }
  }
  const current = d.currentVersion
  const notBase = current !== d.baseVersion ? t.notBase(d.baseVersion) : ''
  if (d.status === 'pending') {
    return {
      heading: t.pendingHeading(current),
      note: t.pendingNote(current, notBase),
      emptyAlerts: t.noAlerts
    }
  }
  const heading = t.decidedHeading(current)
  if (d.status === 'approved') {
    const saved = d.appliedVersion ? t.savedAs(d.appliedVersion) : t.wasApproved
    const part = d.appliedVersion === current ? t.partOfIt : t.partOfFlow
    return {
      heading,
      note: t.approvedNote(current, notBase, saved, part),
      emptyAlerts: d.appliedVersion === current ? t.noAlertsApplied(current) : t.noAlertsCurrent(current)
    }
  }
  return {
    heading,
    note: t.otherNote(current, notBase),
    emptyAlerts: t.noAlertsCurrent(current)
  }
}

export interface DraftState {
  label: string
  /** pending: can be decided; blocked: pending but cannot be approved; done: decided. */
  tone: 'pending' | 'blocked' | 'done'
  explanation: string
}

/** A draft's state in words. */
export function draftState(
  d: Pick<DraftSummary, 'status' | 'stale' | 'baseVersion' | 'currentVersion' | 'appliedVersion' | 'appliedBy' | 'tokenName'>
): DraftState {
  const t = msg().drafts.state
  switch (d.status) {
    case 'pending':
      if (d.stale) {
        return { label: t.outOfDate, tone: 'blocked', explanation: t.outOfDateExplanation(d.baseVersion, d.currentVersion) }
      }
      return { label: t.waiting, tone: 'pending', explanation: t.waitingExplanation }
    case 'approved':
      if (d.appliedBy === 'agent') {
        return { label: t.deployedByAgent, tone: 'done', explanation: t.deployedByAgentExplanation(visibleText(d.tokenName), d.appliedVersion) }
      }
      return { label: t.approved, tone: 'done', explanation: t.approvedExplanation(d.appliedVersion) }
    case 'rejected':
      return { label: t.rejected, tone: 'done', explanation: t.rejectedExplanation }
    case 'void':
      return { label: t.void, tone: 'done', explanation: t.voidExplanation }
    default:
      return { label: visibleText(String(d.status)), tone: 'done', explanation: t.unknownExplanation }
  }
}

const STATUS_WORDS = ['approved', 'rejected', 'void']

/**
 * Why approving would be refused, or '' when it would not (the server
 * decides in the end). deploy: whether "Deploy now" is checked; a running
 * flow is always redeployed.
 */
export function approvalBlock(d: DraftDetail, deploy: boolean): string {
  const t = msg().drafts
  if (d.status !== 'pending') {
    if (d.status === 'approved' && d.appliedBy === 'agent') return t.blockDeployedByAgent
    const word = STATUS_WORDS.includes(d.status) ? d.status : 'decided'
    return word === 'void' ? t.blockVoid : t.blockDecided(t.statusWords[word as 'approved' | 'rejected' | 'decided'])
  }
  if (d.stale) return t.blockStale(d.currentVersion)
  const errors = (d.issues ?? []).filter(i => i.severity === 'error')
  const blocking = errors.filter(i => !ENVIRONMENT_ISSUE_CODES.includes(i.code))
  if (blocking.length) return t.blockErrors(blocking.length)
  if (errors.length && (deploy || d.running)) return d.running ? t.blockSecretsRunning : t.blockSecretsDeploy
  return ''
}

/** A diff's risk change and alerts in words: "Risk raised from Safe to External", "2 alerts: New host (2)" or "no alerts". */
function diffDigest(diff: DiffReport): { risk: string; alerts: string } {
  const risk = riskChange(String(diff.riskBefore), String(diff.riskAfter)).text
  const alerts = diff.alerts ?? []
  const counts = new Map<string, number>()
  for (const a of alerts) counts.set(a.kind, (counts.get(a.kind) ?? 0) + 1)
  const kinds = [...counts].map(([kind, n]) => n > 1 ? `${alertKind(kind).label} (${n})` : alertKind(kind).label)
  return { risk, alerts: counts.size ? msg().drafts.alertsList(alerts.length, kinds.join(', ')) : msg().drafts.noAlertsLower }
}

/**
 * The confirmation text of an approval: the risk and the alerts first, then
 * what happens. When the current version is an agent's the admin never
 * approved, it also compares with the version they last approved: approving
 * makes the result the new baseline, so it approves those changes too.
 */
export function approveConfirmText(d: DraftDetail, deploy: boolean): string {
  const t = msg().drafts
  const now = diffDigest(d.diff)
  let compared: string
  if (d.diffApproved) {
    const since = diffDigest(d.diffApproved)
    compared = t.comparedBoth(d.currentVersion, now.risk, now.alerts, d.lastApprovedVersion, since.risk, since.alerts)
  } else {
    const alerts = now.alerts.charAt(0).toUpperCase() + now.alerts.slice(1)
    compared = `${now.risk}. ${alerts}.`
  }
  const name = visibleText(d.flowName) || visibleText(d.flowId)
  const token = visibleText(d.tokenName || d.tokenId)
  const saved = d.isNew ? t.createdFrom(name, token) : t.savedFrom(name, token)
  // A rename can disguise a flow: say it here too (a new flow's name is in "saved").
  const renamed = !d.isNew && d.nameChange
    ? t.renames(visibleText(d.nameChange.before), visibleText(d.nameChange.after))
    : ''
  const described = !d.isNew && d.descriptionChange ? t.describes : ''
  const runs = d.running ? t.runsRedeploy : deploy ? t.runsDeploy : t.runsNot
  return `${compared} ${saved}${renamed}${described} ${runs}`
}

/** What an approval or rollback did, for OperationFeedback. */
export function applyOutcome(r: ApplyResult, action: string): { kind: 'success' | 'error'; text: string } {
  const t = msg().drafts
  if (r.deployError) return { kind: 'error', text: t.outcomeDeployError(action, r.version, visibleText(r.deployError.message)) }
  if (r.deployed) return { kind: 'success', text: t.outcomeDeployed(action, r.version) }
  return { kind: 'success', text: t.outcomeSaved(action, r.version) }
}
