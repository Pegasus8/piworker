import { expect, test } from 'bun:test'
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
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
  formatRelative,
  mcpStatus,
  auditParams,
  toolKey,
  EXPIRY_OPTIONS,
  DEPLOY_LEVELS,
  GUESSING_AT_EVERY_LEVEL,
  LOWERING_KEEPS_DEPLOYED,
  lowersDeployLevel,
  DESKTOP_CONFIG_PATHS,
  BRIDGE_COMMAND,
  DESKTOP_COMMAND_NOTE,
  DESKTOP_PLAIN_HTTP_NOTE,
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

test('Claude Code checks the CA, then trusts it for the claude command only', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'raspberrypi.local', port: '8443' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(s.url).toBe('https://raspberrypi.local:8443/mcp')
  expect(s.needsCA).toBe(true)
  // A CA swapped on the way would be trusted for every site Node.js reaches.
  expect(s.caCheck).toBe('openssl x509 -in ~/piworker-ca.crt -noout -fingerprint -sha256')
  expect(s.claudeCode).toBe([
    'claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \\',
    `  --header "Authorization: Bearer ${TOKEN}"`
  ].join('\n'))
  // Scoped to the command, never exported into the shell (profile).
  expect(s.claudeStart).toBe('NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude')
  expect(`${s.caCheck}\n${s.claudeCode}\n${s.claudeStart}`).not.toContain('export')
})

test('Claude Desktop runs the bridge by its full path with the private token file', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'raspberrypi.local', port: '8443' }, tls: { mode: 'auto', caFingerprint: FP } })
  const desktop = JSON.parse(s.claudeDesktop)
  // Claude Desktop does not use the shell PATH.
  expect(desktop.mcpServers.piworker.command).toBe('/usr/local/bin/piworker-mcp')
  expect(BRIDGE_COMMAND.posix).toBe('/usr/local/bin/piworker-mcp')
  // The bridge expands a leading ~ itself.
  expect(desktop.mcpServers.piworker.args).toEqual(['--url', 'https://raspberrypi.local:8443/mcp', '--token-file', '~/.piworker-token', '--ca-fingerprint', FP])
  expect(s.claudeDesktop).not.toContain('/path/to')
  expect(s.desktopPlainHttp).toBe(false)
  // The token goes into a private file, never into the desktop config.
  expect(s.claudeDesktop).not.toContain(TOKEN)
  expect(DESKTOP_COMMAND_NOTE).toContain('full path')
  expect(DESKTOP_COMMAND_NOTE).toContain('PATH')
  expect(DESKTOP_COMMAND_NOTE).not.toMatch(/in your PATH or/i)
})

// The token is pasted at a silent prompt, so it never lands in the shell
// history, and the file is created afresh, private to the user.
test('the macOS/Linux token-file command keeps the token out of the history and the file private', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(s.tokenFile).not.toContain(TOKEN)
  expect(s.tokenFile).toContain('umask 077')
  expect(s.tokenFile).toContain('read -rs')

  const home = mkdtempSync(join(tmpdir(), 'pwk-home-'))
  try {
    const run = (input: string) => Bun.spawnSync(['bash', '-c', s.tokenFile], { env: { HOME: home, PATH: process.env.PATH ?? '/usr/bin:/bin' }, stdin: new TextEncoder().encode(input) })
    // An existing file with wider permissions is replaced, not reused.
    const file = join(home, '.piworker-token')
    writeFileSync(file, 'old\n', { mode: 0o644 })
    expect(run(`${TOKEN}\n`).exitCode).toBe(0)
    expect(readFileSync(file, 'utf8')).toBe(`${TOKEN}\n`)
    expect(statSync(file).mode & 0o777).toBe(0o600)
    // Anything else is refused and leaves the file alone.
    const refused = run('not a token\n')
    expect(refused.exitCode).not.toBe(0)
    expect(readFileSync(file, 'utf8')).toBe(`${TOKEN}\n`)
  } finally {
    rmSync(home, { recursive: true, force: true })
  }
})

test('Claude Desktop on Windows gets a PowerShell command that keeps the token file private', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'raspberrypi.local', port: '8443' }, tls: { mode: 'auto', caFingerprint: FP } })
  const ps = s.tokenFileWindows
  // Read at a masked prompt: not in the PSReadLine history.
  expect(ps).not.toContain(TOKEN)
  expect(ps).toContain('Read-Host')
  expect(ps).toContain('-AsSecureString')
  // Any failure stops it before the token is written.
  expect(ps).toContain(`$ErrorActionPreference = 'Stop'`)
  expect(ps).toContain(`$f = Join-Path $env:USERPROFILE '.piworker-token'`)
  // Created afresh (no explicit entries left from an older file), restricted
  // to the current user by SID (no name to resolve), checked, then filled.
  const remove = ps.indexOf('Remove-Item -Force $f')
  const create = ps.indexOf('New-Item -ItemType File -Path $f')
  const icacls = ps.indexOf('icacls $f /inheritance:r /grant:r "*$([System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value):(F)"')
  const check = ps.indexOf('if ($LASTEXITCODE -ne 0)')
  const write = ps.indexOf('Set-Content -Path $f')
  for (const i of [remove, create, icacls, check, write]) expect(i).toBeGreaterThanOrEqual(0)
  expect(remove).toBeLessThan(create)
  expect(create).toBeLessThan(icacls)
  expect(icacls).toBeLessThan(check)
  expect(check).toBeLessThan(write)
  expect(ps).not.toMatch(/icacls[^\n]*Out-Null/)
  expect(ps).toContain('-NoNewline -Encoding ascii')
  expect(ps).not.toContain('umask')
  // The same bridge flags, with the full path of the .exe and ~ for the token.
  const desktop = JSON.parse(s.claudeDesktopWindows)
  expect(desktop.mcpServers.piworker.command).toBe('C:\\Users\\YOU\\AppData\\Local\\Programs\\piworker-mcp\\piworker-mcp.exe')
  expect(BRIDGE_COMMAND.windows).toBe(desktop.mcpServers.piworker.command)
  expect(desktop.mcpServers.piworker.args).toEqual(['--url', 'https://raspberrypi.local:8443/mcp', '--token-file', '~\\.piworker-token', '--ca-fingerprint', FP])
  expect(s.claudeDesktopWindows).toContain('"~\\\\.piworker-token"')
  expect(s.claudeDesktopWindows).not.toContain(TOKEN)
  expect(DESKTOP_CONFIG_PATHS.windows).toBe('%APPDATA%\\Claude\\claude_desktop_config.json')
  expect(DESKTOP_CONFIG_PATHS.macos).toBe('~/Library/Application Support/Claude/claude_desktop_config.json')
})

test('snippets use the HTTPS port even when the editor is opened over HTTP', () => {
  const s = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: '192.168.1.10', port: '8080' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(s.url).toBe('https://192.168.1.10:8443/mcp')
  expect(JSON.parse(s.claudeDesktop).mcpServers.piworker.args[1]).toBe('https://192.168.1.10:8443/mcp')
  const v6 = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'fe80::1', port: '' }, tls: { mode: 'auto', caFingerprint: FP } })
  expect(v6.url).toBe('https://[fe80::1]/mcp')
  const bracketed = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: '[fe80::1]', port: '9443' }, tls: null })
  expect(bracketed.url).toBe('https://[fe80::1]:9443/mcp')
})

test('without the local CA the snippets neither check nor trust the CA, nor pin a fingerprint', () => {
  const custom = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'pi.example.lan', port: '443' }, tls: { mode: 'custom', caFingerprint: '' } })
  expect(custom.url).toBe('https://pi.example.lan:443/mcp')
  expect(custom.needsCA).toBe(false)
  expect(custom.caCheck).toBe('')
  expect(custom.claudeStart).toBe('')
  expect(custom.claudeCode).not.toContain('NODE_EXTRA_CA_CERTS')
  expect(JSON.parse(custom.claudeDesktop).mcpServers.piworker.args).not.toContain('--ca-fingerprint')
  const off = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: 'localhost', port: '8080' }, tls: { mode: 'off', caFingerprint: '' } })
  expect(off.claudeCode).not.toContain('NODE_EXTRA_CA_CERTS')
  expect(off.caCheck).toBe('')
})

// piworker-mcp accepts plain http only to a loopback IP literal (127.0.0.1 or
// [::1]): with TLS off the bridge config names this machine by its IP, and
// the dialog explains why and recommends HTTPS for other devices.
test('with TLS off, Claude Desktop connects over plain HTTP only to this machine', () => {
  const plain = { mode: 'off' as const, caFingerprint: '' }
  for (const hostname of ['localhost', '192.168.1.10', 'raspberrypi.local', '127.0.0.1']) {
    const s = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname, port: '8080' }, tls: plain })
    expect(JSON.parse(s.claudeDesktop).mcpServers.piworker.args).toEqual(['--url', 'http://127.0.0.1:8080/mcp', '--token-file', '~/.piworker-token'])
    expect(JSON.parse(s.claudeDesktopWindows).mcpServers.piworker.args[1]).toBe('http://127.0.0.1:8080/mcp')
    expect(s.desktopPlainHttp).toBe(true)
  }
  for (const hostname of ['::1', '[::1]']) {
    const s = clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname, port: '8080' }, tls: plain })
    expect(JSON.parse(s.claudeDesktop).mcpServers.piworker.args[1]).toBe('http://[::1]:8080/mcp')
    expect(s.url).toBe('http://[::1]:8080/mcp')
  }
  // Claude Code on this machine uses the same address; from another device it
  // keeps the page's host, which answers only with lan_access.
  expect(clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: 'localhost', port: '8080' }, tls: plain }).url).toBe('http://127.0.0.1:8080/mcp')
  expect(clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: '192.168.1.10', port: '8080' }, tls: plain }).url).toBe('http://192.168.1.10:8080/mcp')
  expect(clientSnippets({ token: TOKEN, location: { protocol: 'http:', hostname: 'localhost', port: '' }, tls: plain }).url).toBe('http://127.0.0.1/mcp')
  expect(DESKTOP_PLAIN_HTTP_NOTE).toContain('127.0.0.1')
  expect(DESKTOP_PLAIN_HTTP_NOTE).toContain('[::1]')
  expect(DESKTOP_PLAIN_HTTP_NOTE).toContain('mode = "auto"')
  // Behind an HTTPS reverse proxy (TLS off in PiWorker) the page's https
  // address works for both.
  const proxied = clientSnippets({ token: TOKEN, location: { protocol: 'https:', hostname: 'pi.example.lan', port: '' }, tls: plain })
  expect(proxied.url).toBe('https://pi.example.lan/mcp')
  expect(JSON.parse(proxied.claudeDesktop).mcpServers.piworker.args[1]).toBe('https://pi.example.lan/mcp')
  expect(proxied.desktopPlainHttp).toBe(false)
})

test('an unexpected token is shell-quoted so a snippet can never run extra commands', () => {
  const s = clientSnippets({ token: 'pwk_x"; rm -rf ~; "', location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(s.claudeCode).toContain(`--header 'Authorization: Bearer pwk_x"; rm -rf ~; "'`)
  const quote = clientSnippets({ token: "pwk_it's", location: { protocol: 'https:', hostname: 'pi', port: '8443' }, tls: null })
  expect(quote.claudeCode).toContain(`--header 'Authorization: Bearer pwk_it'\\''s'`)
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
  // Every tool call records its tool's risk (ADR-0015; storage refuses one
  // that does not): that is what the admin is shown, never the published one.
  const recorded = approvalRisk({ toolRisk: 'privileged' })
  expect(recorded).toMatchObject({ risk: 'privileged', label: 'privileged' })
  expect(recorded.reason).toContain('when the agent asked')
  expect(approvalRisk({ toolRisk: 'safe' })).toMatchObject({ risk: 'safe', label: 'safe' })
  // An unexpected or missing value is not trusted as a risk level.
  for (const toolRisk of ['bogus', '', undefined]) {
    const unknown = approvalRisk({ toolRisk: toolRisk as any })
    expect(unknown).toMatchObject({ risk: 'unknown', label: 'risk unknown' })
    expect(unknown.reason).toContain('unexpected value')
  }
})

test('a pending approval whose tool changed since the request says it will not run', () => {
  const a = { status: 'pending', toolName: 'garage_open', toolFingerprint: 'sha256:aaa' }
  expect(approvalToolChange(a, tool())).toBe('')
  expect(approvalToolChange(a, tool({ fingerprint: 'sha256:bbb' }))).toContain('changed since the request')
  expect(approvalToolChange(a, tool({ toolName: 'other' }))).toContain('changed since the request')
  // Unknown is not "changed": the server decides when the tools cannot be read.
  expect(approvalToolChange(a, undefined)).toBe('')
  expect(approvalToolChange({ ...a, status: 'approved' }, tool({ fingerprint: 'sha256:bbb' }))).toBe('')
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
  // The token dialog shows the detail whenever it limits who can connect.
  expect(local.localOnly).toBe(true)
  expect(local.notice).toBe(true)
  expect(lan.localOnly).toBe(false)
  expect(mcpStatus(settings(), auto)).toMatchObject({ localOnly: false, notice: false })
  expect(off.notice).toBe(true)
  // TLS settings that could not be read make no claim about HTTPS.
  const unknownTls = mcpStatus(settings(), null)
  expect(unknownTls.state).toBe('on')
  expect(unknownTls.detail).not.toContain('over HTTPS')
  expect(unknownTls.detail).toContain('could not be read')
  expect(unknownTls).toMatchObject({ localOnly: false, notice: true })
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
  // Every kind of change the low level refuses (service.LowDeployRefusedAlerts),
  // in words the admin reads before turning it on.
  for (const words of ['privileged node', 'destination', 'new host', 'secret reference', 'risk switch',
    'credential', 'how often', 'global variable']) {
    expect(low.risk).toContain(words)
  }
})

test('the low level says what it still allows without your review', () => {
  const low = DEPLOY_LEVELS.find(l => l.value === 'low')!
  expect(low.risk).toContain('raise the flow\'s risk')
  // What an agent may still change in approved flows, with the example the
  // admin can map to a device: an MQTT command (no Operation marker, so
  // whether or not the broker has a password) and an HTTP body sent
  // without a credential.
  for (const words of ['without your review', 'what approved flows send', 'MQTT command', '{"state":"UNLOCK"}',
    'whether or not the broker needs a password', 'HTTP body', 'no credential', 'conditions and processing', 'stop them',
    'agent tools', 'off']) {
    expect(low.allows).toContain(words)
  }
  // ADR-0022's residual: nodes run on data the agent may not see.
  for (const words of ['cannot read', 'which of its nodes run or fail']) {
    expect(low.allows).toContain(words)
  }
  expect(DEPLOY_LEVELS.find(l => l.value === 'off')!.allows).toBeUndefined()
  expect(DEPLOY_LEVELS.find(l => l.value === 'all')!.allows).toBeUndefined()
})

test('lowering the level does not undo what agents deployed', () => {
  expect(lowersDeployLevel('all', 'low')).toBe(true)
  expect(lowersDeployLevel('all', 'off')).toBe(true)
  expect(lowersDeployLevel('low', 'off')).toBe(true)
  expect(lowersDeployLevel('off', 'low')).toBe(false)
  expect(lowersDeployLevel('low', 'all')).toBe(false)
  expect(lowersDeployLevel('low', 'low')).toBe(false)
  // Where to look and how to undo it.
  for (const words of ['keep running', 'privileged ones included', 'Deployed by agent', 'Changed by agent',
    'History', 'Roll back', 'Audit']) {
    expect(LOWERING_KEEPS_DEPLOYED).toContain(words)
  }
})

test('the guessing residual is explained for every level, off included', () => {
  // How (agent text that comes back hidden), by which routes even at off
  // (call scope and an echoing tool, an error a run records, an approved
  // draft), what is at stake (short or predictable values) and what to do.
  for (const words of ['hides known secret values', 'call scope', 'repeats its arguments', 'many guesses per call',
    'read scope', 'error a run records', 'webhook', 'draft you approve', 'short or predictable values',
    'typed into a setting', 'as secrets']) {
    expect(GUESSING_AT_EVERY_LEVEL).toContain(words)
  }
})

test('the levels that let agents deploy explain what they add to the guessing residual', () => {
  expect(DEPLOY_LEVELS.find(l => l.value === 'off')!.residual).toBeUndefined()
  for (const value of ['low', 'all'] as const) {
    const residual = DEPLOY_LEVELS.find(l => l.value === value)!.residual!
    // Who (author and read scopes besides deploy) and what changes: no
    // approval per guess, a flow it deploys. Agents never see the errors of
    // runs of a version no admin approved (ADR-0022).
    for (const words of ['author and read scopes', 'guess', 'flow it deploys', 'without your approval']) {
      expect(residual).toContain(words)
    }
    expect(residual).not.toContain('error a run of it records')
    // The recorder keeps node errors only, never a run's output.
    expect(residual).not.toContain('output')
  }
})
