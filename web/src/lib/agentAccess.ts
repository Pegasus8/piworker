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
}

export type ApprovalStatus = 'pending' | 'approved' | 'rejected' | 'expired'

export interface AgentApproval {
  id: string
  kind: 'tool_call' | 'draft_deploy' | string
  tokenId: string
  tokenName?: string
  flowId?: string
  ref: string
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

/** Renders agent-provided data as indented JSON text (strings stay as they are). */
export function formatJSONText(value: unknown): string {
  if (value === undefined) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2) ?? ''
  } catch {
    return String(value)
  }
}

/** "in 12 min", "3 h ago", "just now"… for an ISO time relative to now. */
export function formatRelative(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const diff = t - now.getTime()
  const abs = Math.abs(diff)
  const future = diff > 0
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

/** A map key for a tool (flow and trigger node ids cannot contain NUL). */
export function toolKey(flowId: string, triggerNodeId: string): string {
  return `${flowId}\u0000${triggerNodeId}`
}
