// Agent access (docs/design/mcp.md §7, §9, §10, §12): types of the
// /api/agent/* payloads and the pure rules the editor applies before calling
// them. Everything an agent or a token supplies is shown as plain text; these
// helpers only ever produce strings for text nodes, never HTML.
import { caFileName } from '@/lib/trust'

export type AgentScope = 'call' | 'read' | 'author' | 'deploy'
export type AgentDeployLevel = 'off' | 'low' | 'all'
export type AgentRisk = 'safe' | 'external' | 'privileged'
export type AgentTokenStatus = 'active' | 'expired' | 'revoked'

export interface TokenGrant {
  flowId: string
  triggerNodeId: string
  fingerprint: string
  suspended: boolean
}

export interface AgentToken {
  id: string
  name: string
  prefix: string
  scopes: AgentScope[]
  privileged: boolean
  grants: TokenGrant[]
  allTools: boolean
  expiresAt: string
  lastUsedAt?: string
  revokedAt?: string
  createdAt: string
  status: AgentTokenStatus
}

export interface CreatedAgentToken {
  token: string
  info: Omit<AgentToken, 'status'>
}

export interface AgentToolAnnotations {
  readOnlyHint: boolean
  destructiveHint: boolean
  idempotentHint: boolean
  openWorldHint: boolean
}

/** A published tool, as GET /api/agent/tools lists it (docs/design/mcp.md §7). */
export interface AgentTool {
  flowId: string
  flowName: string
  triggerNodeId: string
  toolName: string
  title: string
  description: string
  risk: AgentRisk
  annotations: AgentToolAnnotations
  fingerprint: string
  confirm: 'none' | 'client' | 'admin'
  inputSchema: unknown
}

export interface AgentSettings {
  agentDeploy: AgentDeployLevel
  authorEnabled: boolean
  /** mcp.enabled: whether the /mcp endpoint is mounted. */
  mcpEnabled: boolean
  /** mcp.lan_access: whether /mcp accepts LAN clients when TLS is off. */
  lanAccess: boolean
}

export type ApprovalStatus = 'pending' | 'approved' | 'rejected' | 'expired'

export interface AgentApproval {
  id: string
  kind: 'tool_call' | 'draft_deploy' | string
  tokenId: string
  tokenName?: string
  flowId?: string
  ref: string
  /** The tool as published when the agent asked (tool calls; absent on older approvals). */
  toolName?: string
  toolFingerprint?: string
  toolRisk?: AgentRisk
  args?: unknown
  summary: string
  status: ApprovalStatus
  createdAt: string
  decidedAt?: string
  decidedBy?: string
  expiresAt: string
  tokenStatus: AgentTokenStatus | 'unknown'
  /** The stored call result, when the server reports it with the approval. */
  result?: unknown
}

export interface ApprovalDecision {
  approval: AgentApproval
  executed: boolean
  executionError?: string
}

export interface AuditEntry {
  id: number
  ts: string
  tokenId: string
  tokenName: string
  tool: string
  flowId?: string
  runId?: string
  argsSha256?: string
  argsPreview?: string
  outcome: string
  durMs: number
  privileged: boolean
}

export interface AuditRejection {
  tokenId: string
  at: string
  reason: string
  count: number
}

export interface AuditFilters {
  tokenId?: string
  tool?: string
  flowId?: string
  outcome?: string
  /** datetime-local values (the browser's time zone). */
  since?: string
  until?: string
}

export interface ScopeOption {
  scope: AgentScope
  label: string
  description: string
  available: boolean
  /** Why the scope cannot be granted now ('' when it can). */
  reason: string
}

/**
 * The scopes a new token may get. The server refuses author unless
 * mcp.author_enabled is set and deploy while agent deploy is off; unknown
 * settings fail closed.
 */
export function scopeOptions(settings: AgentSettings | null): ScopeOption[] {
  const authorReason = settings?.authorEnabled
    ? ''
    : 'Turned off on this server: set author_enabled = true in the [mcp] section of piworker.toml and restart.'
  const deployReason = settings && settings.agentDeploy !== 'off'
    ? ''
    : 'Agent deploy is off. Choose a level under Agent deploy first.'
  return [
    { scope: 'call', label: 'Call tools', description: 'Run the published tools this token is granted.', available: true, reason: '' },
    { scope: 'read', label: 'Read', description: 'See flows, runs and system status (secrets and variables stay hidden).', available: true, reason: '' },
    { scope: 'author', label: 'Author drafts', description: 'Draft flows; you review and approve each one here.', available: !authorReason, reason: authorReason },
    { scope: 'deploy', label: 'Deploy', description: 'Deploy, start and stop flows within the Agent deploy level.', available: !deployReason, reason: deployReason }
  ]
}

export interface ExpiryOption { days: number; label: string; default?: boolean }

/** Token lifetimes offered by the editor (the server accepts 1–365 days). */
export const EXPIRY_OPTIONS: ExpiryOption[] = [
  { days: 7, label: '7 days' },
  { days: 30, label: '30 days' },
  { days: 90, label: '90 days', default: true },
  { days: 180, label: '180 days' },
  { days: 365, label: '1 year' }
]

export interface DeployLevelOption { value: AgentDeployLevel; label: string; risk: string }

/** Agent deploy levels with the risk the admin accepts (docs/design/mcp.md §10). */
export const DEPLOY_LEVELS: DeployLevelOption[] = [
  {
    value: 'off',
    label: 'Off (recommended)',
    risk: 'Agents can only draft flows. You review and deploy every change yourself.'
  },
  {
    value: 'low',
    label: 'Low-risk flows',
    risk: 'Agents with the deploy scope may deploy, start and stop flows, and deploy their own drafts, when the resulting flow is at most external risk (HTTP, MQTT, e-mail, Telegram, notifications), changes no recipient or destination, and adds no secret reference compared with the version you last approved.'
  },
  {
    value: 'all',
    label: 'All flows',
    risk: 'Agents with the deploy scope may deploy, start and stop any flow, including privileged ones that run commands, write files, query databases or switch GPIO and I2C devices. A leaked token or a misled agent could take over this device.'
  }
]

export interface CreateTokenForm {
  name: string
  scopes: AgentScope[]
  privileged: boolean
  allTools: boolean
  /** The tools picked for individual grants. */
  tools: AgentTool[]
  expiresInDays: number
  password: string
}

export interface CreateTokenRequest {
  name: string
  scopes: AgentScope[]
  privileged: boolean
  allTools: boolean
  grants: { flowId: string; triggerNodeId: string; fingerprint: string }[]
  expiresInDays: number
  password: string
}

const SCOPE_ORDER: AgentScope[] = ['call', 'read', 'author', 'deploy']

/**
 * Builds the POST /api/agent/tokens body. The server rejects unknown members,
 * so grants carry only the tool identity and fingerprint. Grants and the
 * privileged flag only matter with the call scope, and a privileged tool is
 * useless to a token without the flag, so those are left out.
 */
export function buildCreateTokenRequest(form: CreateTokenForm): CreateTokenRequest {
  const scopes = SCOPE_ORDER.filter(s => form.scopes.includes(s))
  const calls = scopes.includes('call')
  const privileged = calls && form.privileged
  const allTools = calls && form.allTools
  const grants = calls && !allTools
    ? form.tools
      .filter(t => privileged || t.risk !== 'privileged')
      .map(t => ({ flowId: t.flowId, triggerNodeId: t.triggerNodeId, fingerprint: t.fingerprint }))
    : []
  return { name: form.name.trim(), scopes, privileged, allTools, grants, expiresInDays: form.expiresInDays, password: form.password }
}

export interface SnippetLocation { protocol: string; hostname: string; port: string }
export interface SnippetTLS { mode: 'auto' | 'custom' | 'off'; caFingerprint: string }

export interface ClientSnippets {
  /** The /mcp endpoint URL. */
  url: string
  /** Shell commands for Claude Code. */
  claudeCode: string
  /** Shell command that saves the token to a private file (Claude Desktop). */
  tokenFile: string
  /** claude_desktop_config.json fragment for the piworker-mcp stdio bridge. */
  claudeDesktop: string
  /** Whether clients must trust PiWorker's local CA. */
  needsCA: boolean
}

/** The HTTPS listener's default port (addr = ":8443"). */
export const DEFAULT_HTTPS_PORT = '8443'
export const TOKEN_FILE = '~/.piworker-token'
export const TOKEN_FILE_PLACEHOLDER = '/path/to/piworker-token'

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`
}

/**
 * Client set-up for a new token (docs/design/mcp.md §12), aimed at the host
 * the editor is open on. An HTTPS page is served by the HTTPS listener, so
 * its port is reused; from an HTTP page the default HTTPS port is assumed
 * unless TLS is off.
 */
export function clientSnippets(opts: { token: string; location: SnippetLocation; tls: SnippetTLS | null }): ClientSnippets {
  const { token, location, tls } = opts
  const host = location.hostname.includes(':') && !location.hostname.startsWith('[') ? `[${location.hostname}]` : location.hostname
  const off = tls?.mode === 'off'
  let scheme = 'https'
  let port = location.port
  if (off) scheme = location.protocol === 'https:' ? 'https' : 'http'
  else if (location.protocol !== 'https:') port = DEFAULT_HTTPS_PORT
  const url = `${scheme}://${host}${port ? `:${port}` : ''}/mcp`
  const needsCA = !tls || tls.mode === 'auto'
  // Tokens are pwk_ plus base32 and underscores; anything else is quoted.
  const safe = /^[A-Za-z0-9_]+$/.test(token)
  const header = safe ? `"Authorization: Bearer ${token}"` : shellQuote(`Authorization: Bearer ${token}`)
  const claudeCode = [
    ...(needsCA ? [`export NODE_EXTRA_CA_CERTS=~/${caFileName()}`] : []),
    `claude mcp add --transport http --scope user piworker ${url} \\`,
    `  --header ${header}`
  ].join('\n')
  const tokenFile = `(umask 077 && printf '%s\\n' ${shellQuote(token)} > ${TOKEN_FILE})`
  const args = ['--url', url, '--token-file', TOKEN_FILE_PLACEHOLDER]
  if (tls?.mode === 'auto' && tls.caFingerprint) args.push('--ca-fingerprint', tls.caFingerprint)
  const claudeDesktop = JSON.stringify({ mcpServers: { piworker: { command: 'piworker-mcp', args } } }, null, 2)
  return { url, claudeCode, tokenFile, claudeDesktop, needsCA }
}

export interface MCPStatus {
  state: 'on' | 'off' | 'unknown'
  label: string
  /** What it means for agents, and how to change it. */
  detail: string
}

/**
 * Whether agents can connect to the MCP endpoint (mcp.enabled and, with TLS
 * off, mcp.lan_access; docs/design/mcp.md §2, §8, §11). Both live in
 * piworker.toml, so the advice names the setting, its environment variable
 * and the restart. Settings that cannot be read are reported as unknown.
 */
export function mcpStatus(settings: AgentSettings | null, tls: SnippetTLS | null): MCPStatus {
  if (!settings || typeof settings.mcpEnabled !== 'boolean') {
    return { state: 'unknown', label: 'MCP status unknown', detail: 'The server\'s agent settings could not be read.' }
  }
  if (!settings.mcpEnabled) {
    return {
      state: 'off',
      label: 'MCP is off',
      detail: 'Agents cannot connect yet. To turn it on, set enabled = true in the [mcp] section of piworker.toml (or the environment variable PIWORKER_MCP_ENABLED=true) and restart PiWorker.'
    }
  }
  if (tls?.mode === 'off' && !settings.lanAccess) {
    return {
      state: 'on',
      label: 'MCP is on (this device only)',
      detail: 'TLS is off, so only clients on this device can connect. To allow other devices, set lan_access = true in the [mcp] section of piworker.toml (or PIWORKER_MCP_LAN_ACCESS=true) and restart; their tokens then travel unencrypted, so prefer turning TLS back on.'
    }
  }
  if (tls?.mode === 'off') {
    return { state: 'on', label: 'MCP is on', detail: 'TLS is off and lan_access is on: devices on your network connect over unencrypted HTTP.' }
  }
  return { state: 'on', label: 'MCP is on', detail: 'Agents connect over HTTPS and send their token as a Bearer header.' }
}

export interface GrantStatus {
  state: 'active' | 'changed' | 'suspended' | 'missing'
  reason: string
  /** Whether re-confirming would resume or update the grant (needs the password). */
  needsConfirm: boolean
  /** The fingerprint to confirm: the tool's current one when it is published. */
  confirmFingerprint: string
}

/** Explains a grant's state against the currently published tool. */
export function grantStatus(grant: TokenGrant, tool: AgentTool | undefined): GrantStatus {
  const current = tool?.fingerprint ?? grant.fingerprint
  const changed = !!tool && tool.fingerprint !== grant.fingerprint
  if (grant.suspended) {
    let reason = 'Suspended. Re-confirm to let the token call this tool again.'
    if (changed) reason = 'Suspended after a redeploy changed the tool (higher risk or wider input). Review it and re-confirm to resume.'
    else if (!tool) reason = 'Suspended. The tool is not published right now (its flow is stopped or the node was removed).'
    return { state: 'suspended', reason, needsConfirm: true, confirmFingerprint: current }
  }
  if (!tool) {
    return { state: 'missing', reason: 'The tool is not published right now (its flow is stopped or the node was removed).', needsConfirm: false, confirmFingerprint: current }
  }
  if (changed) {
    return { state: 'changed', reason: 'The tool changed since you granted it. Re-confirm to accept the new version.', needsConfirm: true, confirmFingerprint: current }
  }
  return { state: 'active', reason: '', needsConfirm: false, confirmFingerprint: current }
}

export interface ApprovalRisk {
  risk: AgentRisk | 'unknown'
  label: string
  /** Where the risk comes from, or why it is unknown. */
  reason: string
}

const RISKS: readonly string[] = ['safe', 'external', 'privileged']

/**
 * The risk shown on an approval: the one recorded with the request
 * (ADR-0015), which is what approving runs. Approvals stored before the risk
 * was recorded fall back to the tool as published now; a tool call whose tool
 * is not published (or when the tools could not be loaded) says its risk is
 * unknown rather than showing none. Draft deploys get their risk on the
 * draft review.
 */
export function approvalRisk(approval: Pick<AgentApproval, 'kind' | 'toolRisk'>, tool: AgentTool | undefined, toolsUnavailable: boolean): ApprovalRisk | null {
  if (approval.kind !== 'tool_call') return null
  const recorded = approval.toolRisk
  if (recorded && RISKS.includes(recorded)) {
    return { risk: recorded, label: recorded, reason: 'Risk of the tool when the agent asked.' }
  }
  if (recorded) return { risk: 'unknown', label: 'risk unknown', reason: 'Risk unknown: the request recorded an unexpected value.' }
  if (tool) return { risk: tool.risk, label: tool.risk, reason: 'Risk of the tool as published now (this request did not record it).' }
  const reason = toolsUnavailable
    ? 'Risk unknown: the published tools could not be loaded.'
    : 'Risk unknown: the tool is not published now (its flow is stopped or the node was removed).'
  return { risk: 'unknown', label: 'risk unknown', reason }
}

/** The tool an approval names: as recorded with the request, else as published now, else the node id. */
export function approvalToolName(approval: Pick<AgentApproval, 'kind' | 'ref' | 'toolName'>, tool: AgentTool | undefined): string {
  if (approval.kind !== 'tool_call') return approval.ref
  return approval.toolName || tool?.toolName || approval.ref
}

/**
 * Why approving a pending tool call would run nothing: the server runs it
 * only when the tool still has the name and fingerprint recorded with the
 * request (ADR-0015). '' when it would run, or when the published tool is not
 * known here (the server then decides).
 */
export function approvalToolChange(approval: Pick<AgentApproval, 'kind' | 'status' | 'toolName' | 'toolFingerprint'>, tool: AgentTool | undefined): string {
  if (approval.kind !== 'tool_call' || approval.status !== 'pending') return ''
  if (!approval.toolName || !approval.toolFingerprint) {
    return 'This request did not record which version of the tool it was for, so approving it runs nothing. The agent can ask again.'
  }
  if (!tool) return ''
  if (tool.toolName !== approval.toolName || tool.fingerprint !== approval.toolFingerprint) {
    return 'The tool changed since the request (it was redeployed with another name, risk, input or confirmation), so approving it runs nothing. The agent can ask again.'
  }
  return ''
}

// Characters that are invisible or change how the text around them is shown,
// so that what an admin reads differs from what runs ("Trojan Source"):
// format characters (bidi embeddings, overrides and isolates, zero-width
// characters, the BOM, tag characters…), the other default-ignorable ones
// (variation selectors, Hangul fillers, the combining grapheme joiner), line
// and paragraph separators, lone surrogates, and control characters other
// than tab and line feed. JSON.stringify escapes none of the non-ASCII ones.
const HIDDEN_CHARS = /[\p{Cf}\p{Default_Ignorable_Code_Point}\p{Zl}\p{Zp}\p{Cs}\x00-\x08\x0B-\x1F\x7F-\x9F]/gu

/** A run of text, or one hidden character shown as an escape. */
export interface TextPart {
  text: string
  hidden: boolean
  /** "U+202E" for a hidden character. */
  code?: string
}

function escapeCodePoint(cp: number): string {
  const hex = cp.toString(16).toUpperCase()
  return cp > 0xFFFF ? `\\u{${hex}}` : `\\u${hex.padStart(4, '0')}`
}

/** Splits text into visible runs and \uXXXX escapes of its hidden characters. */
export function revealHidden(text: string): TextPart[] {
  const parts: TextPart[] = []
  let from = 0
  for (const m of text.matchAll(HIDDEN_CHARS)) {
    if (m.index > from) parts.push({ text: text.slice(from, m.index), hidden: false })
    const cp = m[0].codePointAt(0)!
    parts.push({ text: escapeCodePoint(cp), hidden: true, code: `U+${cp.toString(16).toUpperCase().padStart(4, '0')}` })
    from = m.index + m[0].length
  }
  if (from < text.length) parts.push({ text: text.slice(from), hidden: false })
  return parts
}

/** The text with every hidden or direction-changing character shown as an escape. */
export function visibleText(text: string): string {
  return text.replace(HIDDEN_CHARS, c => escapeCodePoint(c.codePointAt(0)!))
}

/** How many hidden or direction-changing characters the text has. */
export function countHidden(text: string): number {
  return text.match(HIDDEN_CHARS)?.length ?? 0
}

/**
 * Agent-provided data as indented JSON (strings stay as they are), raw: show
 * it through revealHidden or visibleText, never as is.
 */
export function jsonText(value: unknown): string {
  if (value === undefined) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2) ?? ''
  } catch {
    return String(value)
  }
}

/** Renders agent-provided data as indented JSON text with hidden characters escaped. */
export function formatJSONText(value: unknown): string {
  return visibleText(jsonText(value))
}

const RELATIVE_LIMIT_MS = 45 * 86_400_000

/** "in 12 min", "3 h ago", "just now"… for an ISO time relative to now; "on <date>" beyond 45 days. */
export function formatRelative(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const diff = t - now.getTime()
  const abs = Math.abs(diff)
  const future = diff > 0
  if (abs >= RELATIVE_LIMIT_MS) return `on ${new Date(t).toLocaleDateString()}`
  if (abs < 60_000) return future ? 'in a few seconds' : 'just now'
  let text: string
  if (abs < 3_600_000) text = `${Math.round(abs / 60_000)} min`
  else if (abs < 86_400_000) text = `${Math.round(abs / 3_600_000)} h`
  else {
    const days = Math.round(abs / 86_400_000)
    text = `${days} day${days === 1 ? '' : 's'}`
  }
  return future ? `in ${text}` : `${text} ago`
}

/** A local date and time for tables. */
export function formatDateTime(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

function rfc3339(local: string | undefined): string | undefined {
  if (!local) return undefined
  const d = new Date(local)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

export type AuditParams = Record<string, string | number>

/** GET /api/agent/audit query parameters; empty filters are left out. */
export function auditParams(filters: AuditFilters, limit: number, beforeId?: number): AuditParams {
  const params: AuditParams = {}
  for (const key of ['tokenId', 'tool', 'flowId', 'outcome'] as const) {
    const v = filters[key]?.trim()
    if (v) params[key] = v
  }
  const since = rfc3339(filters.since)
  const until = rfc3339(filters.until)
  if (since) params.since = since
  if (until) params.until = until
  if (beforeId) params.beforeId = beforeId
  params.limit = limit
  return params
}

/**
 * GET /api/agent/audit parameters for the rejection counters only. The server
 * filters counters by token and time alone, and limits them like entries, so
 * they are read apart with their own limit; beforeId=1 asks for no entries
 * (audit ids start at 1).
 */
export function rejectionParams(filters: AuditFilters, limit: number): AuditParams {
  const { tokenId, since, until } = auditParams(filters, limit)
  const params: AuditParams = {}
  if (tokenId) params.tokenId = tokenId
  if (since) params.since = since
  if (until) params.until = until
  params.beforeId = 1
  params.limit = limit
  return params
}

/** A map key for a tool (flow and trigger node ids cannot contain NUL). */
export function toolKey(flowId: string, triggerNodeId: string): string {
  return `${flowId}\u0000${triggerNodeId}`
}
