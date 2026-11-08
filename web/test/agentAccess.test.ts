import { expect, test } from 'bun:test'
import {
  scopeOptions,
  buildCreateTokenRequest,
  clientSnippets,
  grantStatus,
  formatJSONText,
  jsonText,
  visibleText,
  revealHidden,
  countHidden,
  approvalRisk,
  approvalToolChange,
  approvalToolName,
  formatRelative,
  mcpStatus,
  auditParams,
  toolKey,
  EXPIRY_OPTIONS,
  DEPLOY_LEVELS,
  DESKTOP_CONFIG_PATHS,
  type AgentTool
} from '../src/lib/agentAccess'

const tool = (over: Partial<AgentTool> = {}): AgentTool => ({
  flowId: 'f1', flowName: 'Garage', triggerNodeId: 'n1', toolName: 'garage_open', title: 'Open garage',
  description: 'Opens the garage door for a minute', risk: 'external',
  annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: true },
  fingerprint: 'sha256:aaa', confirm: 'none', inputSchema: { type: 'object' }, ...over
})

test('author and deploy scopes are only offered when the server allows them', () => {
  const closed = scopeOptions({ agentDeploy: 'off', authorEnabled: false })
  expect(closed.map(s => s.scope)).toEqual(['call', 'read', 'author', 'deploy'])
  const byScope = Object.fromEntries(closed.map(s => [s.scope, s]))
  expect(byScope.call.available).toBe(true)
  expect(byScope.read.available).toBe(true)
  expect(byScope.author.available).toBe(false)
  expect(byScope.author.reason).toContain('author_enabled')
  expect(byScope.deploy.available).toBe(false)
  expect(byScope.deploy.reason).toContain('Agent deploy is off')

  const open = Object.fromEntries(scopeOptions({ agentDeploy: 'low', authorEnabled: true }).map(s => [s.scope, s]))
  expect(open.author.available).toBe(true)
  expect(open.deploy.available).toBe(true)
  expect(open.deploy.reason).toBe('')
  // Unknown settings (not loaded yet) fail closed.
  const unknown = Object.fromEntries(scopeOptions(null).map(s => [s.scope, s]))
  expect(unknown.author.available).toBe(false)
  expect(unknown.deploy.available).toBe(false)
})

test('the create request carries exactly the fields the server accepts', () => {
  const body = buildCreateTokenRequest({
    name: '  Claude Code laptop ', scopes: ['read', 'call'], privileged: false, allTools: false,
    tools: [tool(), tool({ triggerNodeId: 'n2', toolName: 'reboot', risk: 'privileged', fingerprint: 'sha256:bbb' })],
    expiresInDays: 30, password: 'secret-pass'
  })
  expect(body).toEqual({
    name: 'Claude Code laptop', scopes: ['call', 'read'], privileged: false, allTools: false,
    // The privileged tool is dropped: without the privileged flag it could never be called.
    grants: [{ flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:aaa' }],
    expiresInDays: 30, password: 'secret-pass'
  })
  // Extra tool fields never leak into grants (the server rejects unknown members).
  expect(Object.keys(body.grants[0]).sort()).toEqual(['fingerprint', 'flowId', 'triggerNodeId'])
})

test('all tools and tokens without the call scope send no individual grants', () => {
  const all = buildCreateTokenRequest({ name: 'a', scopes: ['call'], privileged: false, allTools: true, tools: [tool()], expiresInDays: 90, password: 'p' })
  expect(all.allTools).toBe(true)
  expect(all.grants).toEqual([])
  const readOnly = buildCreateTokenRequest({ name: 'a', scopes: ['read'], privileged: true, allTools: true, tools: [tool()], expiresInDays: 90, password: 'p' })
  expect(readOnly.allTools).toBe(false)
  expect(readOnly.grants).toEqual([])
  // Privileged without the call scope means nothing; keep the token narrow.
  expect(readOnly.privileged).toBe(false)
  const priv = buildCreateTokenRequest({ name: 'a', scopes: ['call'], privileged: true, allTools: false, tools: [tool({ risk: 'privileged' })], expiresInDays: 90, password: 'p' })
  expect(priv.privileged).toBe(true)
  expect(priv.grants).toHaveLength(1)
})

test('expiry choices default to 90 days and stay within the server limit', () => {
  expect(EXPIRY_OPTIONS.find(o => o.default)?.days).toBe(90)
  for (const o of EXPIRY_OPTIONS) {
    expect(o.days).toBeGreaterThanOrEqual(1)
    expect(o.days).toBeLessThanOrEqual(365)
  }
})

const TOKEN = 'pwk_abcdefghijklmnop_' + 'a'.repeat(52)
const FP = 'SHA256:AB:CD'

test('Claude Code snippet trusts the local CA and targets the HTTPS listener', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'raspberrypi.local', port: '8443' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(s.url).toBe('https://raspberrypi.local:8443/mcp')
  expect(s.claudeCode).toBe([
    'export NODE_EXTRA_CA_CERTS=~/piworker-ca.crt',
    'claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \\',
    `  --header "Authorization: Bearer ${TOKEN}"`
  ].join('\n'))
  const desktop = JSON.parse(s.claudeDesktop)
  expect(desktop.mcpServers.piworker.command).toBe('piworker-mcp')
  expect(desktop.mcpServers.piworker.args).toEqual(['--url', 'https://raspberrypi.local:8443/mcp', '--token-file', '/path/to/piworker-token', '--ca-fingerprint', FP])
  // The token goes into a private file, never into the desktop config.
  expect(s.claudeDesktop).not.toContain(TOKEN)
  expect(s.tokenFile).toContain(TOKEN)
  expect(s.tokenFile).toContain('umask 077')
})

test('Claude Desktop on Windows gets a PowerShell command that keeps the token file private', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'raspberrypi.local', port: '8443' }, tls: { mode: 'auto', caFingerprint: FP } })
  const ps = s.tokenFileWindows
  // Created empty, restricted to the current user, then filled.
  expect(ps).toContain(`$f = Join-Path $env:USERPROFILE 'piworker-token'`)
  expect(ps).toContain('icacls $f /inheritance:r /grant:r')
  expect(ps).toContain('[System.Security.Principal.WindowsIdentity]::GetCurrent().Name')
  expect(ps.indexOf('icacls')).toBeLessThan(ps.indexOf(TOKEN))
  expect(ps).toContain(`Set-Content -Path $f -Value '${TOKEN}' -NoNewline -Encoding ascii`)
  expect(ps).not.toContain('umask')
  // The same bridge flags, with a Windows path in the config.
  const desktop = JSON.parse(s.claudeDesktopWindows)
  expect(desktop.mcpServers.piworker.args).toEqual(['--url', 'https://raspberrypi.local:8443/mcp', '--token-file', 'C:\\Users\\YOU\\piworker-token', '--ca-fingerprint', FP])
  expect(s.claudeDesktopWindows).toContain('C:\\\\Users\\\\YOU\\\\piworker-token')
  expect(s.claudeDesktopWindows).not.toContain(TOKEN)
  expect(DESKTOP_CONFIG_PATHS.windows).toBe('%APPDATA%\\Claude\\claude_desktop_config.json')
  expect(DESKTOP_CONFIG_PATHS.macos).toBe('~/Library/Application Support/Claude/claude_desktop_config.json')
  // A quote in an unexpected token cannot end the PowerShell string.
  const odd = clientSnippets({ token: "pwk_it's", location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(odd.tokenFileWindows).toContain(`-Value 'pwk_it''s'`)
})

test('snippets use the HTTPS port even when the editor is opened over HTTP', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: '192.168.1.10', port: '8080' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(s.url).toBe('https://192.168.1.10:8443/mcp')
  const v6 = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'fe80::1', port: '' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(v6.url).toBe('https://[fe80::1]/mcp')
  const bracketed = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: '[fe80::1]', port: '9443' }, tls: null })
  expect(bracketed.url).toBe('https://[fe80::1]:9443/mcp')
})

test('without the local CA the snippets neither export the CA nor pin a fingerprint', () => {
  const custom = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'pi.example.lan', port: '443' }, tls: { mode: 'custom', caFingerprint: '' } })
  expect(custom.url).toBe('https://pi.example.lan:443/mcp')
  expect(custom.claudeCode).not.toContain('NODE_EXTRA_CA_CERTS')
  expect(JSON.parse(custom.claudeDesktop).mcpServers.piworker.args).not.toContain('--ca-fingerprint')
  const off = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: 'localhost', port: '8080' }, tls: { mode: 'off', caFingerprint: '' } })
  expect(off.url).toBe('http://localhost:8080/mcp')
  expect(off.claudeCode).not.toContain('NODE_EXTRA_CA_CERTS')
})

test('an unexpected token is shell-quoted so a snippet can never run extra commands', () => {
  const s = clientSnippets({ token: 'pwk_x"; rm -rf ~; "', location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(s.claudeCode).toContain(`--header 'Authorization: Bearer pwk_x"; rm -rf ~; "'`)
  const quote = clientSnippets({ token: "pwk_it's", location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(quote.tokenFile).toContain(`'pwk_it'\\''s'`)
})

test('grant status explains suspensions and tool changes', () => {
  const g = { flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:aaa', suspended: false }
  expect(grantStatus(g, tool())).toMatchObject({ state: 'active', needsConfirm: false })
  const changed = grantStatus(g, tool({ fingerprint: 'sha256:new' }))
  expect(changed).toMatchObject({ state: 'changed', needsConfirm: true, confirmFingerprint: 'sha256:new' })
  expect(changed.reason).toContain('changed')
  const raised = grantStatus({ ...g, suspended: true }, tool({ fingerprint: 'sha256:new' }))
  expect(raised).toMatchObject({ state: 'suspended', needsConfirm: true, confirmFingerprint: 'sha256:new' })
  expect(raised.reason).toContain('redeploy')
  const paused = grantStatus({ ...g, suspended: true }, tool())
  expect(paused).toMatchObject({ state: 'suspended', needsConfirm: true, confirmFingerprint: 'sha256:aaa' })
  const missing = grantStatus(g, undefined)
  expect(missing).toMatchObject({ state: 'missing', needsConfirm: false })
  expect(missing.reason).toContain('not published')
  const suspendedMissing = grantStatus({ ...g, suspended: true }, undefined)
  expect(suspendedMissing).toMatchObject({ state: 'suspended', needsConfirm: true, confirmFingerprint: 'sha256:aaa' })
})

test('agent-provided values are rendered as indented JSON text', () => {
  expect(formatJSONText({ door: 'main', html: '<img src=x onerror=alert(1)>' })).toBe('{\n  "door": "main",\n  "html": "<img src=x onerror=alert(1)>"\n}')
  expect(formatJSONText(undefined)).toBe('')
  expect(formatJSONText(null)).toBe('null')
  expect(formatJSONText('plain <b>text</b>')).toBe('plain <b>text</b>')
})

// Trojan Source: bidi controls and invisible characters make text display in
// a different order, or with different content, than what runs.
const RLO = '‮'
const LRI = '⁦'
const PDI = '⁩'

test('hidden and direction-changing characters are shown as escapes', () => {
  expect(visibleText(`/home/pi/${RLO}gnp.sh`)).toBe('/home/pi/\\u202E' + 'gnp.sh')
  expect(visibleText(`a${LRI}b${PDI}c`)).toBe('a\\u2066b\\u2069c')
  // Zero-width characters, marks, the BOM, the Arabic letter mark and other
  // invisible or ignorable characters.
  for (const c of ['​', '‌', '‍', '‎', '‏', '؜', '﻿', '­', '⁠', ' ', ' ', '͏', '️', 'ㅤ', '\u0085', '\u007F', '\u001B']) {
    const code = c.codePointAt(0)!.toString(16).toUpperCase().padStart(4, '0')
    expect(visibleText(`x${c}y`)).toBe(`x\\u${code}y`)
  }
  // Characters outside the BMP (tag characters) use the long form.
  expect(visibleText('a\u{E0041}b')).toBe('a\\u{E0041}b')
  // Ordinary text, line breaks and tabs are untouched, including RTL scripts.
  const plain = 'Open the door\n\tplease — ¿sí? שלום مرحبا 你好 😀'
  expect(visibleText(plain)).toBe(plain)
  expect(countHidden(plain)).toBe(0)
  expect(countHidden(`${RLO}x${LRI}y${PDI}`)).toBe(3)
  expect(revealHidden(`ab${RLO}cd`)).toEqual([
    { text: 'ab', hidden: false },
    { text: '\\u202E', hidden: true, code: 'U+202E' },
    { text: 'cd', hidden: false }
  ])
  expect(revealHidden('')).toEqual([])
})

test('JSON text from agents never carries hidden characters', () => {
  const args = { path: `/home/pi/${RLO}gnp.sh`, [`k${LRI}`]: `v${PDI}` }
  const text = formatJSONText(args)
  expect(text).not.toContain(RLO)
  expect(text).not.toContain(LRI)
  expect(text).not.toContain(PDI)
  expect(text).toContain('"path": "/home/pi/\\u202Egnp.sh"')
  expect(text).toContain('"k\\u2066": "v\\u2069"')
  expect(formatJSONText(`summary ${RLO}`)).toBe('summary \\u202E')
  // The raw text keeps them, for counting and highlighting.
  expect(countHidden(jsonText(args))).toBe(3)
})

test('approvals show the risk recorded with the request', () => {
  // The recorded risk wins over the tool as published now (ADR-0015).
  const recorded = approvalRisk({ kind: 'tool_call', toolRisk: 'privileged' }, tool({ risk: 'safe' }), false)!
  expect(recorded).toMatchObject({ risk: 'privileged', label: 'privileged' })
  expect(recorded.reason).toContain('when the agent asked')
  expect(approvalRisk({ kind: 'tool_call', toolRisk: 'safe' }, undefined, true)).toMatchObject({ risk: 'safe', label: 'safe' })
  // An unexpected recorded value is not trusted as a risk level.
  expect(approvalRisk({ kind: 'tool_call', toolRisk: 'bogus' as any }, undefined, false)).toMatchObject({ risk: 'unknown' })
})

test('older approvals without a recorded risk fall back to the published tool, or say why it is unknown', () => {
  const old = { kind: 'tool_call' }
  expect(approvalRisk(old, tool({ risk: 'privileged' }), false)).toMatchObject({ risk: 'privileged', label: 'privileged' })
  expect(approvalRisk(old, tool({ risk: 'privileged' }), false)!.reason).toContain('as published now')
  // A tool that is not published now has no known risk: never show none.
  const missing = approvalRisk(old, undefined, false)!
  expect(missing).toMatchObject({ risk: 'unknown', label: 'risk unknown' })
  expect(missing.reason).toContain('not published')
  const unavailable = approvalRisk(old, undefined, true)!
  expect(unavailable).toMatchObject({ risk: 'unknown', label: 'risk unknown' })
  expect(unavailable.reason).toContain('could not be loaded')
  // Draft deploys are reviewed with their own risk on the draft page.
  expect(approvalRisk({ kind: 'draft_deploy' }, undefined, false)).toBeNull()
})

test('approvals name the tool recorded with the request', () => {
  expect(approvalToolName({ kind: 'tool_call', ref: 'n1', toolName: 'garage_open' }, tool({ toolName: 'renamed' }))).toBe('garage_open')
  expect(approvalToolName({ kind: 'tool_call', ref: 'n1' }, tool({ toolName: 'published' }))).toBe('published')
  expect(approvalToolName({ kind: 'tool_call', ref: 'n1' }, undefined)).toBe('n1')
  expect(approvalToolName({ kind: 'draft_deploy', ref: 'draft-1' }, undefined)).toBe('draft-1')
})

test('a pending approval whose tool changed since the request says it will not run', () => {
  const a = { kind: 'tool_call', status: 'pending', toolName: 'garage_open', toolFingerprint: 'sha256:aaa' }
  expect(approvalToolChange(a, tool())).toBe('')
  expect(approvalToolChange(a, tool({ fingerprint: 'sha256:bbb' }))).toContain('changed since the request')
  expect(approvalToolChange(a, tool({ toolName: 'other' }))).toContain('changed since the request')
  // Unknown is not "changed": the server decides when the tools cannot be read.
  expect(approvalToolChange(a, undefined)).toBe('')
  expect(approvalToolChange({ ...a, status: 'approved' }, tool({ fingerprint: 'sha256:bbb' }))).toBe('')
  // Older approvals did not record the tool: approving them runs nothing.
  expect(approvalToolChange({ kind: 'tool_call', status: 'pending' }, tool())).toContain('did not record')
  expect(approvalToolChange({ kind: 'draft_deploy', status: 'pending' }, undefined)).toBe('')
})

test('the MCP status says whether agents can connect and how to turn it on', () => {
  const settings = (over: Record<string, unknown> = {}) => ({ agentDeploy: 'off', authorEnabled: false, mcpEnabled: true, lanAccess: false, ...over }) as any
  const auto = { mode: 'auto' as const, caFingerprint: FP }
  const off = mcpStatus(settings({ mcpEnabled: false }), auto)
  expect(off.state).toBe('off')
  expect(off.detail).toContain('enabled = true')
  expect(off.detail).toContain('[mcp]')
  expect(off.detail).toContain('PIWORKER_MCP_ENABLED=true')
  expect(off.detail).toContain('restart')
  expect(mcpStatus(settings(), auto)).toMatchObject({ state: 'on', label: 'MCP is on' })
  // Settings that cannot be read are not reported as off or on.
  expect(mcpStatus(null, auto).state).toBe('unknown')
  expect(mcpStatus(settings({ mcpEnabled: undefined }), auto).state).toBe('unknown')
  // With TLS off, only this device can connect unless lan_access is set.
  const plain = { mode: 'off' as const, caFingerprint: '' }
  const local = mcpStatus(settings(), plain)
  expect(local.state).toBe('on')
  expect(local.label).toContain('this device only')
  expect(local.detail).toContain('lan_access = true')
  expect(local.detail).toContain('PIWORKER_MCP_LAN_ACCESS=true')
  const lan = mcpStatus(settings({ lanAccess: true }), plain)
  expect(lan.label).toBe('MCP is on')
  expect(lan.detail).toContain('unencrypted')
  // lan_access only matters with TLS off.
  expect(mcpStatus(settings({ lanAccess: true }), auto).detail).not.toContain('unencrypted')
})

test('relative times read naturally in both directions', () => {
  const now = new Date('2026-11-08T12:00:00Z')
  expect(formatRelative('2026-11-08T12:00:20Z', now)).toBe('in a few seconds')
  expect(formatRelative('2026-11-08T12:12:00Z', now)).toBe('in 12 min')
  expect(formatRelative('2026-11-08T09:00:00Z', now)).toBe('3 h ago')
  expect(formatRelative('2026-11-01T12:00:00Z', now)).toBe('7 days ago')
  expect(formatRelative('2026-11-08T11:59:30Z', now)).toBe('just now')
  expect(formatRelative('', now)).toBe('')
  // Far-off times read better as dates.
  expect(formatRelative('2026-12-20T12:00:00Z', now)).toBe('in 42 days')
  expect(formatRelative('2099-01-01T00:00:00Z', now)).toBe('on ' + new Date('2099-01-01T00:00:00Z').toLocaleDateString())
  expect(formatRelative('2026-08-01T00:00:00Z', now)).toBe('on ' + new Date('2026-08-01T00:00:00Z').toLocaleDateString())
})

test('audit filters become API parameters, empty ones are left out', () => {
  expect(auditParams({}, 50)).toEqual({ limit: 50 })
  expect(auditParams({ tokenId: 'tok', tool: ' garage_open ', flowId: '', outcome: 'error' }, 50, 41)).toEqual({ tokenId: 'tok', tool: 'garage_open', outcome: 'error', beforeId: 41, limit: 50 })
  const local = '2026-11-08T12:30'
  const p = auditParams({ since: local, until: local }, 10)
  expect(p.since).toBe(new Date(local).toISOString().replace(/\.\d{3}Z$/, 'Z'))
  expect(p.until).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)
  expect(auditParams({ since: 'not a date' }, 10)).toEqual({ limit: 10 })
})

test('tools are identified by flow and trigger node', () => {
  expect(toolKey('f1', 'n1')).toBe('f1\u0000n1')
  expect(toolKey('a', 'bc')).not.toBe(toolKey('ab', 'c'))
})

test('deploy levels describe their risk', () => {
  expect(DEPLOY_LEVELS.map(l => l.value)).toEqual(['off', 'low', 'all'])
  const all = DEPLOY_LEVELS.find(l => l.value === 'all')!
  expect(all.risk).toContain('privileged')
  const low = DEPLOY_LEVELS.find(l => l.value === 'low')!
  expect(low.risk).toContain('external')
})
