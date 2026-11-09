import { test, expect, type Page, type Route } from '@playwright/test'

// The editor's agent pages against a mocked /api (docs/design/mcp.md §7, §9, §10, §12).

type Req = { method: string; path: string; query: URLSearchParams; body: any; headers: Record<string, string> }

const TOKEN = 'pwk_abcdefghijklmnop_' + 'q'.repeat(52)
const FP = 'SHA256:AB:CD:EF'
const XSS = '<img src=x onerror="window.__xss=1">'

const tools = [
  { flowId: 'f1', flowName: 'Garage', triggerNodeId: 'n1', toolName: 'garage_open', title: 'Open the garage', description: 'Opens the garage door for one minute.', risk: 'external', annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: true }, fingerprint: 'sha256:new', confirm: 'admin', inputSchema: { type: 'object' } },
  { flowId: 'f2', flowName: 'Maintenance', triggerNodeId: 'n2', toolName: 'reboot_pi', title: 'Reboot', description: 'Reboots the Raspberry Pi.', risk: 'privileged', annotations: { readOnlyHint: false, destructiveHint: true, idempotentHint: false, openWorldHint: true }, fingerprint: 'sha256:p1', confirm: 'none', inputSchema: { type: 'object' } }
]

function tokens() {
  return [
    { id: 'tokA', name: 'laptop ' + XSS, prefix: 'pwk_tokA', scopes: ['call', 'read'], privileged: false, allTools: false, grants: [{ flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:old', suspended: true }], expiresAt: '2099-01-01T00:00:00Z', createdAt: '2026-11-01T00:00:00Z', lastUsedAt: '2026-11-07T00:00:00Z', status: 'active' },
    { id: 'tokB', name: 'old desktop', prefix: 'pwk_tokB', scopes: ['read'], privileged: false, allTools: false, grants: [], expiresAt: '2026-12-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', revokedAt: '2026-11-02T00:00:00Z', status: 'revoked' }
  ]
}

const now = Date.now()
const iso = (offsetMs: number) => new Date(now + offsetMs).toISOString()
const approvals = [
  { id: 'ap1', kind: 'tool_call', tokenId: 'tokA', tokenName: 'laptop', flowId: 'f1', ref: 'n1', toolName: 'garage_open', toolFingerprint: 'sha256:new', toolRisk: 'external', args: { door: 'main', note: XSS }, summary: 'Open it please ' + XSS, status: 'pending', createdAt: iso(-60_000), expiresAt: iso(14 * 60_000), tokenStatus: 'active' },
  { id: 'ap2', kind: 'draft_deploy', tokenId: 'tokA', tokenName: 'laptop', flowId: 'f1', ref: 'draft-1', summary: 'Deploy my draft', status: 'pending', createdAt: iso(-120_000), expiresAt: iso(3_600_000), tokenStatus: 'active' }
]

function auditPage(from: number, n: number, preview = '{"door":"main"}') {
  return Array.from({ length: n }, (_, i) => ({ id: from - i, ts: iso(-i * 60_000), tokenId: 'tokA', tokenName: 'laptop', tool: i === 0 ? 'reboot_pi' : 'garage_open', flowId: 'f1', runId: `run-${from - i}`, argsPreview: i === 0 ? XSS : preview, outcome: i === 0 ? 'error' : 'completed', durMs: 12, privileged: i === 0 }))
}

interface MockOptions { toolsMissing?: boolean; pending?: number; extraApprovals?: unknown[]; auditPreview?: string; rejectionsTruncated?: boolean; settingsFail?: boolean; executionError?: string; approveResult?: unknown; mcpEnabled?: boolean; tlsOff?: boolean; pendingCounts?: number[]; secrets?: { name: string; short: boolean }[]; secretsFail?: boolean }

async function mockServer(page: Page, opts: MockOptions = {}) {
  const requests: Req[] = []
  let settings = { agentDeploy: 'off', authorEnabled: false, mcpEnabled: opts.mcpEnabled ?? true, lanAccess: false }
  const handle = async (route: Route) => {
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname.replace(/^\/api/, '')
    const raw = req.postData()
    const body = raw ? JSON.parse(raw) : undefined
    requests.push({ method: req.method(), path, query: url.searchParams, body, headers: req.headers() })
    const ok = (data: unknown, status = 200) => route.fulfill({ status, json: { success: true, data } })
    const fail = (status: number, error: string) => route.fulfill({ status, json: { success: false, error } })
    const key = `${req.method()} ${path}`
    switch (key) {
      case 'GET /auth/status': return ok({ enabled: true, setupRequired: false })
      case 'GET /auth/session': return ok({ username: 'owner' })
      case 'POST /auth/password': return ok(null)
      case 'GET /flows': return ok({ flows: [{ id: 'f1', name: 'Garage', state: 'running', nodes: [] }, { id: 'f2', name: 'Maintenance', state: 'running', nodes: [] }] })
      case 'GET /tls': return opts.tlsOff
        ? ok({ mode: 'off', caFingerprint: '', sans: [], notAfter: null, caCertUrl: '' })
        : ok({ mode: 'auto', caFingerprint: FP, sans: ['localhost'], notAfter: '2027-12-10T00:00:00Z', caCertUrl: '/piworker-ca.crt' })
      case 'GET /agent/tokens': return ok({ tokens: tokens() })
      case 'POST /agent/tokens':
        if (body.password !== 'right-password') return fail(403, 'The password is incorrect')
        return ok({ token: TOKEN, info: { ...tokens()[0], id: 'tokN', name: body.name } }, 201)
      case 'DELETE /agent/tokens/tokA': return ok(null)
      case 'POST /agent/tokens/revoke-all': return ok({ revoked: 1 })
      case 'POST /agent/tokens/tokA/grants': {
        const grants = tokens()[0].grants.map(g => ({ ...g, suspended: body.action === 'suspend', fingerprint: body.fingerprint ?? g.fingerprint }))
        return ok({ ...tokens()[0], grants: body.action === 'remove' ? [] : grants })
      }
      case 'GET /agent/tools': return opts.toolsMissing ? fail(404, 'Not found') : ok({ version: 7, tools })
      case 'GET /agent/settings': return opts.settingsFail ? fail(500, 'Cannot read agent settings') : ok(settings)
      case 'PUT /agent/settings':
        settings = { ...settings, agentDeploy: body.agentDeploy }
        return ok(settings)
      case 'GET /secrets': return opts.secretsFail ? fail(500, 'Failed to read secrets') : ok({ secrets: opts.secrets ?? [], shortBelow: 16 })
      case 'GET /agent/approvals/pending-count': return ok({ pending: opts.pendingCounts?.shift() ?? opts.pending ?? 2 })
      case 'GET /agent/approvals': {
        const status = url.searchParams.get('status')
        return ok({ approvals: status === 'pending' || status === null ? [...approvals, ...(opts.extraApprovals ?? [])] : [], pending: opts.pending ?? 2 })
      }
      case 'POST /agent/approvals/ap1/approve':
        if (opts.approveResult !== undefined) return ok({ approval: { ...approvals[0], status: 'approved', decidedBy: 'owner', decidedAt: iso(0), result: opts.approveResult }, executed: true })
        return ok({ approval: { ...approvals[0], status: 'approved', decidedBy: 'owner', decidedAt: iso(0) }, executed: false, executionError: 'not available' })
      case 'POST /agent/approvals/ap3/approve': return ok({ approval: { ...(opts.extraApprovals![0] as object), status: 'approved', decidedBy: 'owner', decidedAt: iso(0) }, executed: false, executionError: opts.executionError })
      case 'POST /agent/approvals/ap1/reject': return ok({ approval: { ...approvals[0], status: 'rejected' }, executed: false })
      case 'GET /agent/audit': {
        const before = Number(url.searchParams.get('beforeId') ?? 0)
        const entries = before ? auditPage(before - 1, 2) : auditPage(200, 50, opts.auditPreview)
        // The per-minute counters of this response are not what the page shows.
        return ok({ entries, rejections: [{ tokenId: 'tokA', at: iso(-60_000), reason: 'cooldown', count: 1 }] })
      }
      case 'GET /agent/audit/rejections':
        return ok({
          totals: [{ tokenId: 'tokA', tokenName: 'laptop ' + XSS, reason: 'busy', count: 5, last: iso(-60_000) }, { tokenId: 'gone', tokenName: '', reason: 'invalid_args', count: 1, last: iso(-120_000) }],
          truncated: !!opts.rejectionsTruncated
        })
    }
    if (req.method() === 'PUT' && path.startsWith('/secrets/')) {
      // Values are never listed: the mock keeps the name and its short mark.
      const name = decodeURIComponent(path.slice('/secrets/'.length))
      const short = [...String(body?.value ?? '')].length < 16
      opts.secrets = [...(opts.secrets ?? []).filter(s => s.name !== name), { name, short }].sort((a, b) => a.name.localeCompare(b.name))
      return ok(null)
    }
    return fail(404, `Unexpected ${key}`)
  }
  await page.route('**/api/**', handle)
  return requests
}

const last = (requests: Req[], method: string, path: string) => [...requests].reverse().find(r => r.method === method && r.path === path)
const lastEntries = (requests: Req[]) => last(requests, 'GET', '/agent/audit')
const lastTotals = (requests: Req[]) => last(requests, 'GET', '/agent/audit/rejections')

test('the navigation badge counts pending approvals and leads to the agent pages', async ({ page }) => {
  await mockServer(page)
  await page.goto('/')
  const agents = page.getByRole('button', { name: 'Agents: 2 requests waiting for approval' })
  await expect(agents).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'waiting for your approval' })).toBeVisible()
  await agents.click()
  await expect(page).toHaveURL(/\/agents\/approvals$/)
  await expect(page.getByRole('heading', { name: 'Agent access', level: 1 })).toBeVisible()
  // The app-wide notice is not repeated on the queue itself.
  await expect(page.getByRole('status').filter({ hasText: 'waiting for your approval' })).toHaveCount(0)
  await page.getByRole('link', { name: 'Tokens' }).click()
  await expect(page).toHaveURL(/\/agents$/)
  await page.getByRole('link', { name: 'Audit' }).click()
  await expect(page).toHaveURL(/\/agents\/audit$/)
})

test('polling pauses while the tab is hidden', async ({ page }) => {
  const requests = await mockServer(page, { pending: 0 })
  await page.clock.install()
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Agents', exact: true })).toBeVisible()
  // The badge polls the lightweight count only.
  const polls = () => requests.filter(r => r.path === '/agent/approvals/pending-count').length
  await expect.poll(polls).toBe(1)
  await page.clock.runFor(10_500)
  await expect.poll(polls).toBe(2)
  expect(requests.filter(r => r.path === '/agent/approvals')).toHaveLength(0)
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.clock.runFor(30_000)
  expect(polls()).toBe(2)
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(polls).toBe(3)
})

test('a new token is shown once, with client snippets for this host', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents')
  await expect(page.getByText('laptop ' + XSS)).toBeVisible()
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()
  await page.getByRole('button', { name: 'New token' }).click()
  const dialog = page.getByRole('dialog', { name: 'New agent token' })
  await expect(dialog.getByLabel('Author drafts')).toBeDisabled()
  await expect(dialog.getByText('author_enabled = true')).toBeVisible()
  await expect(dialog.getByLabel('Deploy', { exact: true })).toBeDisabled()
  await expect(dialog.getByText('Agent deploy is off')).toBeVisible()

  await dialog.getByLabel('Name').fill('Claude Code laptop')
  await dialog.getByLabel('Read').check()
  await expect(dialog.getByLabel('garage_open')).toBeVisible()
  await expect(dialog.getByLabel('reboot_pi')).toBeDisabled()
  await dialog.getByLabel('Allow privileged tools').check()
  await expect(dialog.getByText('Anyone holding this token')).toBeVisible()
  await expect(dialog.getByLabel('reboot_pi')).toBeEnabled()
  await dialog.getByLabel('Allow privileged tools').uncheck()
  await dialog.getByLabel('garage_open').check()
  await dialog.getByLabel('Expires after').selectOption('30')
  await dialog.getByLabel('Your password').fill('wrong-password')
  await dialog.getByRole('button', { name: 'Create token' }).click()
  await expect(dialog.getByRole('alert')).toContainText('The password is incorrect')
  await dialog.getByLabel('Your password').fill('right-password')
  await dialog.getByRole('button', { name: 'Create token' }).click()

  expect(last(requests, 'POST', '/agent/tokens')!.body).toEqual({
    name: 'Claude Code laptop', scopes: ['call', 'read'], privileged: false, allTools: false,
    grants: [{ flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:new' }], expiresInDays: 30, password: 'right-password'
  })
  const secret = page.getByRole('dialog', { name: 'Copy your new token' })
  await expect(secret.getByLabel('New token')).toHaveText(TOKEN)
  // The CA is checked, then trusted for the claude command only.
  await expect(secret.getByLabel('Check the CA certificate')).toHaveText('openssl x509 -in ~/piworker-ca.crt -noout -fingerprint -sha256')
  await expect(secret.getByLabel('Expected CA fingerprint')).toHaveText(FP.replace(/^SHA256:/, ''))
  await expect(secret.getByLabel('Claude Code commands')).toContainText('claude mcp add --transport http --scope user piworker https://127.0.0.1:8443/mcp')
  await expect(secret.getByLabel('Claude Code commands')).toContainText(`--header "Authorization: Bearer ${TOKEN}"`)
  await expect(secret.getByLabel('Start Claude Code')).toHaveText('NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude')
  for (const label of ['Check the CA certificate', 'Claude Code commands', 'Start Claude Code']) await expect(secret.getByLabel(label)).not.toContainText('export')
  // Claude Desktop: the bridge by its full path, the token read at a silent prompt.
  const desktopConfig = secret.getByLabel('Claude Desktop configuration')
  await expect(desktopConfig).toContainText('"command": "/usr/local/bin/piworker-mcp"')
  await expect(desktopConfig).toContainText('"~/.piworker-token"')
  await expect(desktopConfig).toContainText('"--ca-fingerprint"')
  await expect(desktopConfig).toContainText(FP)
  await expect(secret).toContainText('"command" must be the full path of piworker-mcp')
  await expect(secret.getByLabel('Save the token to a file', { exact: true })).toContainText('read -rs t')
  await expect(secret.getByLabel('Save the token to a file', { exact: true })).not.toContainText(TOKEN)
  await page.screenshot({ path: 'test-results/agent-token-created.png', fullPage: true })
  // Windows: PowerShell keeps the token file private, and the config names Windows paths.
  await secret.getByRole('group', { name: 'Operating system' }).getByRole('button', { name: 'Windows' }).click()
  const ps = secret.getByLabel('Save the token to a file (PowerShell)')
  await expect(ps).toContainText('icacls $f /inheritance:r /grant:r')
  await expect(ps).toContainText('if ($LASTEXITCODE -ne 0)')
  await expect(ps).toContainText('-AsSecureString')
  await expect(ps).not.toContainText(TOKEN)
  const windowsConfig = secret.getByLabel('Claude Desktop configuration (Windows)')
  await expect(windowsConfig).toContainText('"--token-file",')
  await expect(windowsConfig).toContainText('"~\\\\.piworker-token"')
  await expect(windowsConfig).toContainText('piworker-mcp.exe')
  await expect(windowsConfig).toContainText(FP)
  await expect(secret).toContainText('%APPDATA%\\Claude\\claude_desktop_config.json')
  await expect(secret.getByLabel('Save the token to a file', { exact: true })).toHaveCount(0)
  // Clicking outside or pressing Escape must not lose the token.
  await page.keyboard.press('Escape')
  await expect(secret).toBeVisible()
  await secret.getByRole('button', { name: 'Done, I saved it' }).click()
  await expect(page.getByText(TOKEN)).toHaveCount(0)
})

// piworker-mcp takes plain http only to 127.0.0.1 or [::1], and with TLS off
// /mcp answers only this device unless lan_access is on: the dialog says both.
test('with TLS off the token dialog points the bridge at this machine and says why', async ({ page }) => {
  await mockServer(page, { tlsOff: true })
  await page.goto('/agents')
  await page.getByRole('button', { name: 'New token' }).click()
  const dialog = page.getByRole('dialog', { name: 'New agent token' })
  await dialog.getByLabel('Name').fill('desktop')
  await dialog.getByLabel('garage_open').check()
  await dialog.getByLabel('Your password').fill('right-password')
  await dialog.getByRole('button', { name: 'Create token' }).click()
  const secret = page.getByRole('dialog', { name: 'Copy your new token' })
  await expect(secret.getByLabel('Claude Desktop configuration')).toContainText('"http://127.0.0.1:18096/mcp"')
  await expect(secret.getByLabel('Claude Desktop configuration')).not.toContainText('--ca-fingerprint')
  await expect(secret.getByRole('note')).toContainText('only to this machine (127.0.0.1 or [::1])')
  await expect(secret.getByRole('note')).toContainText('mode = "auto"')
  await expect(secret.getByLabel('Check the CA certificate')).toHaveCount(0)
  await expect(secret.getByLabel('Start Claude Code')).toHaveCount(0)
  await expect(secret).toContainText('MCP is on (this device only)')
  await expect(secret).toContainText('lan_access = true')
})

// Agents change the pending count at will; the reload it triggers on the
// Approvals page must not count as the admin's activity.
test('the approvals page reloads new requests as background requests', async ({ page }) => {
  const requests = await mockServer(page, { pendingCounts: [2, 3] })
  await page.clock.install()
  await page.goto('/agents/approvals')
  const lists = () => requests.filter(r => r.method === 'GET' && r.path === '/agent/approvals')
  await expect.poll(() => lists().length).toBeGreaterThanOrEqual(1)
  expect(lists()[0].headers['x-piworker-background']).toBeUndefined()
  const before = lists().length
  await page.clock.runFor(10_500)
  await expect.poll(() => lists().length).toBeGreaterThan(before)
  expect(lists().at(-1)!.headers['x-piworker-background']).toBe('1')
  // A refresh the admin asks for is activity.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => lists().length).toBeGreaterThan(before + 1)
  expect(lists().at(-1)!.headers['x-piworker-background']).toBeUndefined()
})

test('without published tools the token dialog says so', async ({ page }) => {
  await mockServer(page, { toolsMissing: true })
  await page.goto('/agents')
  await page.getByRole('button', { name: 'New token' }).click()
  await expect(page.getByRole('dialog', { name: 'New agent token' }).getByText('No tools published yet')).toBeVisible()
})

test('grants show why they are suspended and are re-confirmed with the password', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents')
  const token = page.getByRole('listitem').filter({ hasText: 'pwk_tokA' })
  await expect(token.getByText('1 suspended')).toBeVisible()
  await token.getByRole('button', { name: 'Show grants (1)' }).click()
  const grant = token.getByRole('listitem').filter({ hasText: 'garage_open' })
  await expect(grant.getByText('Suspended', { exact: true })).toBeVisible()
  await expect(grant.getByText(/after a redeploy changed the tool/)).toBeVisible()
  await grant.getByRole('button', { name: 'Re-confirm' }).click()
  const prompt = page.getByRole('dialog', { name: 'Re-confirm grant' })
  await prompt.getByLabel('Your password').fill('right-password')
  await prompt.getByRole('button', { name: 'Re-confirm' }).click()
  await expect.poll(() => last(requests, 'POST', '/agent/tokens/tokA/grants')?.body).toEqual({ action: 'confirm', flowId: 'f1', triggerNodeId: 'n1', fingerprint: 'sha256:new', password: 'right-password' })
  await expect(grant.getByText('Active', { exact: true })).toBeVisible()

  await grant.getByRole('button', { name: 'Suspend' }).click()
  await expect.poll(() => last(requests, 'POST', '/agent/tokens/tokA/grants')?.body).toEqual({ action: 'suspend', flowId: 'f1', triggerNodeId: 'n1' })

  // A privileged tool is useless to a token without the privileged flag.
  await expect(token.getByLabel('Add a tool').locator('option:checked')).toHaveText('Choose a published tool')
  await expect(token.getByLabel('Add a tool').locator('option', { hasText: 'reboot_pi' })).toBeDisabled()
  await page.setViewportSize({ width: 375, height: 812 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.setViewportSize({ width: 1280, height: 720 })

  await token.getByRole('button', { name: 'Revoke' }).click()
  await page.getByRole('dialog', { name: 'Revoke token?' }).getByRole('button', { name: 'Revoke token' }).click()
  await expect.poll(() => last(requests, 'DELETE', '/agent/tokens/tokA')).toBeTruthy()
  await page.screenshot({ path: 'test-results/agent-tokens.png', fullPage: true })
})

test('revoke all is one request, without a password', async ({ page }) => {
  const requests = await mockServer(page, { pending: 0 })
  await page.goto('/agents')
  await page.getByRole('button', { name: 'Revoke all' }).click()
  const confirm = page.getByRole('dialog', { name: 'Revoke all tokens?' })
  await expect(confirm).toContainText('requests waiting for approval are rejected')
  await confirm.getByRole('button', { name: 'Revoke all tokens' }).click()
  await expect(page.getByText('Revoked 1 token.')).toBeVisible()
  expect(requests.filter(r => r.method === 'POST' && r.path === '/agent/tokens/revoke-all')).toHaveLength(1)
  expect(last(requests, 'POST', '/agent/tokens/revoke-all')!.body).toBeUndefined()
  expect(requests.filter(r => r.method === 'DELETE')).toHaveLength(0)
})

test('the agent pages say whether MCP is on and how to turn it on', async ({ page }) => {
  await mockServer(page, { mcpEnabled: false, pending: 0 })
  await page.goto('/agents/audit')
  const status = page.getByRole('region', { name: 'MCP endpoint' })
  await expect(status).toContainText('MCP is off')
  await expect(status).toContainText('enabled = true in the [mcp] section of piworker.toml')
  await expect(status).toContainText('PIWORKER_MCP_ENABLED=true')
  await page.getByRole('link', { name: 'Tokens' }).click()
  await expect(page.getByRole('region', { name: 'MCP endpoint' })).toContainText('MCP is off')

  await page.unrouteAll()
  await mockServer(page, { mcpEnabled: true, pending: 0 })
  await page.goto('/agents/approvals')
  await expect(page.getByRole('region', { name: 'MCP endpoint' })).toContainText('MCP is on')
  await expect(page.getByText('PIWORKER_MCP_ENABLED')).toHaveCount(0)
})

test('the agent deploy level shows its risk and needs the password', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents')
  const level = page.getByRole('radiogroup', { name: 'Agent deploy level' })
  await expect(level.getByLabel('Off (recommended)')).toBeChecked()
  await level.getByLabel('All flows').check()
  await expect(page.getByText('could take over this device')).toBeVisible()
  await page.getByLabel('Password to change the level').fill('right-password')
  await page.getByRole('button', { name: 'Change agent deploy' }).click()
  await expect.poll(() => last(requests, 'PUT', '/agent/settings')?.body).toEqual({ agentDeploy: 'all', password: 'right-password' })
  // With agent deploy on, new tokens may get the deploy scope.
  await page.getByRole('button', { name: 'New token' }).click()
  await expect(page.getByRole('dialog', { name: 'New agent token' }).getByLabel('Deploy', { exact: true })).toBeEnabled()
})

test('the deploy levels explain the guessing residual and count the short secrets', async ({ page }) => {
  const opts: MockOptions = { secrets: [{ name: 'api_key', short: false }, { name: 'door_pin', short: true }] }
  const requests = await mockServer(page, opts)
  await page.goto('/agents')
  const level = page.getByRole('radiogroup', { name: 'Agent deploy level' })
  // At every level, off included: a call-scope token and a granted tool
  // that repeats its arguments are enough.
  await expect(level).toHaveAccessibleDescription(/with the call scope, from a granted tool that repeats its arguments/)
  await expect(page.getByText('Guessable secrets, at every level:')).toBeVisible()
  // What low and all add, next to them only.
  for (const name of ['Low-risk flows', 'All flows']) {
    await expect(level.getByLabel(name)).toHaveAccessibleDescription(/author and read scopes can guess without your approval/)
  }
  await expect(level.getByLabel('Off (recommended)')).not.toHaveAccessibleDescription(/guess/)
  const short = page.getByRole('status').filter({ hasText: 'Short secrets:' })
  await expect(short).toContainText('1 of 2 secrets is short (fewer than 16 characters): an agent could confirm a guess of it.')
  // One read with the page; nothing polls it.
  await page.waitForTimeout(300)
  expect(requests.filter(r => r.method === 'GET' && r.path === '/secrets')).toHaveLength(1)

  // The secrets dialog marks the short one with an icon and words.
  await short.getByRole('button', { name: 'Review secrets' }).click()
  const dialog = page.getByRole('dialog', { name: 'Manage secrets' })
  const marks = dialog.getByText('Short value:', { exact: false })
  await expect(marks).toHaveCount(1)
  await expect(dialog).toContainText('An agent could confirm a guess of it (see Agents). Use 16 random characters or more.')
  // A short value is flagged while typed; replacing it clears the mark. The
  // hint sits in a polite live region that exists before it is filled, so
  // a screen reader announces it as it appears, not only on focus.
  const hint = dialog.locator('#secret-value-short')
  await expect(hint).toHaveAttribute('aria-live', 'polite')
  await expect(hint).toHaveText('')
  await dialog.getByLabel('Secret name').fill('door_pin')
  await dialog.getByLabel('Secret value').fill('1234')
  await expect(hint).toContainText('Short value:')
  await expect(dialog.getByLabel('Secret value')).toHaveAccessibleDescription(/Short value/)
  await dialog.getByLabel('Secret value').fill('a much longer passphrase')
  await expect(dialog.getByLabel('Secret value')).not.toHaveAccessibleDescription(/Short value/)
  await expect(hint).toHaveText('')
  await dialog.getByRole('button', { name: 'Add secret' }).click()
  await expect(marks).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Done' }).click()
  await expect(short).toContainText('No secret is shorter than 16 characters. Use random values: a long word or date can still be guessed.')
})

test('secrets that cannot be read leave the short count unknown', async ({ page }) => {
  await mockServer(page, { secretsFail: true })
  await page.goto('/agents')
  await expect(page.getByRole('status').filter({ hasText: 'Short secrets:' })).toContainText('Could not check the secrets for short values.')
})

test('an unreadable agent deploy level is shown as unknown, not as off', async ({ page }) => {
  const opts: MockOptions = { settingsFail: true }
  await mockServer(page, opts)
  await page.goto('/agents')
  const level = page.getByRole('radiogroup', { name: 'Agent deploy level' })
  const error = page.getByRole('alert').filter({ hasText: 'Cannot read agent settings' })
  await expect(error).toBeVisible()
  for (const name of ['Off (recommended)', 'Low-risk flows', 'All flows']) {
    await expect(level.getByLabel(name)).not.toBeChecked()
    await expect(level.getByLabel(name)).toBeDisabled()
  }
  await expect(page.getByText('Author scope: unknown')).toBeVisible()
  opts.settingsFail = false
  await error.getByRole('button', { name: 'Retry' }).click()
  await expect(level.getByLabel('Off (recommended)')).toBeChecked()
  await expect(error).toHaveCount(0)
  await expect(page.getByText('Author scope: disabled')).toBeVisible()
})

test('approvals render agent text as plain text and report the execution', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents/approvals')
  const call = page.getByRole('article', { name: 'Request garage_open' })
  await expect(call.getByText('Garage', { exact: true })).toBeVisible()
  await expect(call.getByText('laptop', { exact: true })).toBeVisible()
  await expect(call.getByText('external', { exact: true })).toBeVisible()
  await expect(call.getByLabel('Arguments')).toContainText('"note": "<img src=x onerror=\\"window.__xss=1\\">"')
  await expect(call.getByText('Open it please ' + XSS)).toBeVisible()
  await expect(call.locator('img')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()

  const draft = page.getByRole('article', { name: 'Request draft-1' })
  await expect(draft.getByText('Review draft')).toBeVisible()
  await expect(draft.getByRole('button', { name: 'Approve…' })).toBeDisabled()
  await page.screenshot({ path: 'test-results/agent-approvals.png', fullPage: true })

  await call.getByRole('button', { name: 'Approve…' }).click()
  await page.getByRole('dialog', { name: 'Approve and run?' }).getByRole('button', { name: 'Approve and run' }).click()
  await expect.poll(() => last(requests, 'POST', '/agent/approvals/ap1/approve')).toBeTruthy()
  await expect(page.getByRole('alert')).toContainText('Approved, but it did not run: not available')
  await expect(call.getByText('Approved', { exact: true })).toBeVisible()
  await expect(call.getByRole('button', { name: 'Approve…' })).toHaveCount(0)
})

test('an approval for a tool that is not published says its risk is unknown', async ({ page }) => {
  // An approval stored before the tool's name and risk were recorded.
  await mockServer(page, { extraApprovals: [{ ...approvals[0], id: 'ap4', ref: 'gone', flowId: 'f2', args: {}, toolName: undefined, toolFingerprint: undefined, toolRisk: undefined }] })
  await page.goto('/agents/approvals')
  const call = page.getByRole('article', { name: 'Request gone' })
  await expect(call.getByText('risk unknown', { exact: true })).toBeVisible()
  await expect(call.getByText('Risk unknown: the tool is not published now')).toBeVisible()
  await expect(page.getByRole('article', { name: 'Request garage_open' }).getByText('external', { exact: true })).toBeVisible()
  await expect(page.getByRole('article', { name: 'Request garage_open' }).getByText(/risk unknown/i)).toHaveCount(0)
  // Draft deploys get their risk on the draft review.
  await expect(page.getByRole('article', { name: 'Request draft-1' }).getByText(/risk/i)).toHaveCount(0)
})

test('approvals show the tool and risk recorded with the request, and refuse a tool that changed since', async ({ page }) => {
  // The node now publishes reboot_pi (privileged, sha256:p1); the agent asked for an earlier version.
  await mockServer(page, { extraApprovals: [{ ...approvals[0], id: 'ap5', flowId: 'f2', ref: 'n2', toolName: 'reboot_v1', toolFingerprint: 'sha256:p0', toolRisk: 'safe', args: {} }] })
  await page.goto('/agents/approvals')
  const stale = page.getByRole('article', { name: 'Request reboot_v1' })
  await expect(stale.getByText('safe', { exact: true })).toBeVisible()
  await expect(stale.getByText('privileged', { exact: true })).toHaveCount(0)
  await expect(stale.getByRole('note')).toContainText('The tool changed since the request')
  await expect(stale.getByRole('button', { name: 'Approve…' })).toBeDisabled()
  await expect(stale.getByRole('button', { name: 'Reject…' })).toBeEnabled()
  // The current request still matches the published tool.
  const current = page.getByRole('article', { name: 'Request garage_open' })
  await expect(current.getByRole('note')).toHaveCount(0)
  await expect(current.getByRole('button', { name: 'Approve…' })).toBeEnabled()
})

test('an approved call shows its stored result as plain text', async ({ page }) => {
  await mockServer(page, { approveResult: { status: 'completed', runId: 'ap1', result: { message: XSS }, durationMs: 5 } })
  await page.goto('/agents/approvals')
  const call = page.getByRole('article', { name: 'Request garage_open' })
  await call.getByRole('button', { name: 'Approve…' }).click()
  await page.getByRole('dialog', { name: 'Approve and run?' }).getByRole('button', { name: 'Approve and run' }).click()
  await expect(page.getByText('garage_open: Approved and run.')).toBeVisible()
  const result = call.getByLabel('Result')
  await expect(result).toContainText('"status": "completed"')
  await expect(result).toContainText('"message": "<img src=x onerror=\\"window.__xss=1\\">"')
  // The "Decided just now" summary shows it too.
  await expect(page.getByLabel('Result')).toHaveCount(2)
  await expect(page.locator('main img')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()
})

test('hidden and direction-changing characters from agents are shown as escapes', async ({ page }) => {
  // Trojan Source: "/home/pi/<RLO>gnp.sh" displays as "/home/pi/hs.png".
  const RLO = '\u202E'
  const LRI = '\u2066'
  const PDI = '\u2069'
  const path = `/home/pi/${RLO}gnp.sh`
  await mockServer(page, {
    extraApprovals: [{ ...approvals[0], id: 'ap3', ref: 'n9', flowId: 'f2', toolName: 'convert_image', toolFingerprint: 'sha256:c1', toolRisk: 'privileged', args: { path }, summary: `Convert ${LRI}the image${PDI}` }],
    auditPreview: JSON.stringify({ path }),
    executionError: `cannot open /home/pi/${RLO}gnp.sh`
  })
  await page.goto('/agents/approvals')
  const call = page.getByRole('article', { name: 'Request convert_image' })
  const args = call.getByLabel('Arguments')
  await expect(args).toContainText('"path": "/home/pi/\\u202Egnp.sh"')
  expect(await args.textContent()).not.toContain(RLO)
  await expect(args.locator('mark')).toHaveAttribute('title', 'Hidden character U+202E')
  // What is displayed is the order in which the arguments are sent.
  await expect(args).toHaveCSS('unicode-bidi', 'bidi-override')
  await expect(args).toHaveCSS('direction', 'ltr')
  const summary = call.getByLabel('Summary')
  await expect(summary).toContainText('Convert \\u2066the image\\u2069')
  expect(await summary.textContent()).not.toMatch(/[\u2066\u2069]/)
  await expect(call.getByText('3 hidden characters')).toBeVisible()
  // A request without them carries no warning.
  await expect(page.getByRole('article', { name: 'Request garage_open' }).getByText(/hidden character/)).toHaveCount(0)
  await call.getByRole('button', { name: 'Approve…' }).click()
  const confirm = page.getByRole('dialog', { name: 'Approve and run?' })
  await expect(confirm).toContainText('3 hidden characters')
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  // Errors can echo the arguments too.
  await call.getByRole('button', { name: 'Approve…' }).click()
  await confirm.getByRole('button', { name: 'Approve and run' }).click()
  const outcome = page.getByRole('alert').filter({ hasText: 'did not run' })
  await expect(outcome).toContainText('cannot open /home/pi/\\u202Egnp.sh')
  expect(await outcome.textContent()).not.toContain(RLO)

  await page.goto('/agents/audit')
  const table = page.getByRole('table', { name: 'Agent calls' })
  await expect(table.getByText('/home/pi/\\u202Egnp.sh').first()).toBeVisible()
  expect(await table.textContent()).not.toContain(RLO)
})

test('the audit log filters, pages and counts rejections', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents/audit')
  const table = page.getByRole('table', { name: 'Agent calls' })
  await expect(table.getByRole('row')).toHaveCount(51)
  await expect(table.getByText(XSS)).toBeVisible()
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()
  const rejections = page.getByRole('table', { name: 'Rejected calls' })
  // Totals are summed by the server, per token and reason; only the token and time filters apply.
  const busy = rejections.getByRole('row').filter({ hasText: 'busy' })
  await expect(busy).toContainText('5')
  await expect(busy).toContainText('laptop ' + XSS)
  await expect(rejections.getByRole('row').filter({ hasText: 'invalid_args' })).toContainText('gone')
  await expect(rejections.getByRole('row').filter({ hasText: 'cooldown' })).toHaveCount(0)
  expect([...lastTotals(requests)!.query.keys()]).toEqual([])
  await expect(page.getByText('the tool, flow and outcome filters do not apply to these counts')).toBeVisible()
  await expect(page.getByText(/largest totals are listed/)).toHaveCount(0)
  await page.getByRole('button', { name: 'Load more' }).click()
  await expect.poll(() => lastEntries(requests)?.query.get('beforeId')).toBe('151')
  await expect(table.getByRole('row')).toHaveCount(53)
  await expect(page.getByRole('button', { name: 'Load more' })).toHaveCount(0)

  await page.getByLabel('Tool').fill('garage_open')
  await page.getByLabel('Outcome').fill('error')
  await page.getByLabel('Token').selectOption({ label: 'laptop ' + XSS })
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect.poll(() => {
    const q = lastEntries(requests)?.query
    return q && [q.get('tool'), q.get('outcome'), q.get('tokenId'), q.get('beforeId')]
  }).toEqual(['garage_open', 'error', 'tokA', null])
  await expect.poll(() => {
    const q = lastTotals(requests)?.query
    return q && [q.get('tool'), q.get('outcome'), q.get('tokenId'), q.get('beforeId'), q.get('limit')]
  }).toEqual([null, null, 'tokA', null, null])
  await page.screenshot({ path: 'test-results/agent-audit.png', fullPage: true })
})

test('rejection totals say when only the largest are listed', async ({ page }) => {
  await mockServer(page, { rejectionsTruncated: true })
  await page.goto('/agents/audit')
  await expect(page.getByRole('table', { name: 'Rejected calls' }).getByRole('row').filter({ hasText: 'busy' })).toContainText('5')
  await expect(page.getByText('Only the 2 largest totals are listed; narrow the token or time filters to see the others.')).toBeVisible()
})

test('a password change can keep agent tokens', async ({ page }) => {
  const requests = await mockServer(page, { pending: 0 })
  await page.goto('/account')
  const keep = page.getByLabel('Keep agent tokens')
  await expect(keep).not.toBeChecked()
  await keep.check()
  await page.getByLabel('Current password', { exact: true }).fill('first-password')
  await page.getByLabel('New password', { exact: true }).fill('second-password')
  await page.getByLabel('Confirm password', { exact: true }).fill('second-password')
  await page.getByRole('button', { name: 'Change password', exact: true }).click()
  await expect.poll(() => last(requests, 'POST', '/auth/password')?.body).toEqual({ currentPassword: 'first-password', password: 'second-password', keepAgentTokens: true })
})
