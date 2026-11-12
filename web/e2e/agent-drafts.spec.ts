import { test, expect, type Page, type Route } from '@playwright/test'

// The drafts pages, the flow markers and the version history against a mocked
// /api (docs/design/mcp.md §10, wave 4 section F).

type Req = { method: string; path: string; query: URLSearchParams; body: any; headers: Record<string, string> }

const XSS = '<img src=x onerror="window.__xss=1">'
const now = Date.now()
const iso = (offsetMs: number) => new Date(now + offsetMs).toISOString()

const summary = (over: Record<string, unknown> = {}) => ({
  id: 'd1', flowId: 'f1', flowName: 'Garage', isNew: false, baseVersion: 3, currentVersion: 3, stale: false,
  status: 'pending', risk: 'external', alertCount: 3, issueCounts: { error: 0, warning: 1 },
  note: 'Send the door state to my chat ' + XSS, tokenId: 'tokA', tokenName: 'laptop', createdAt: iso(-60_000), decidedAt: null, ...over
})

const diff = {
  nodes: [
    {
      nodeId: 'tg1', type: 'action-telegram', typeName: 'Telegram', change: 'added', moved: false,
      fields: [
        { path: 'config.chatId', change: 'added', after: '12345', sensitive: false, destination: true },
        { path: 'config.botToken', change: 'added', after: '{{secret.BOT}}', sensitive: true, destination: false }
      ]
    },
    {
      nodeId: 'http1', type: 'action-http', typeName: 'HTTP Request', change: 'changed', moved: true,
      fields: [
        { path: 'config.url', change: 'changed', before: 'https://a.example/hook', after: 'https://evil.example/' + XSS, sensitive: false, destination: true },
        { path: 'config.apiKey', change: 'changed', before: '<hidden>', after: '<hidden>', sensitive: true, destination: false }
      ]
    }
  ],
  connections: { added: [{ id: 'c9', sourceNode: 'w1', sourcePort: 'output', targetNode: 'tg1', targetPort: 'input' }], removed: [] },
  riskBefore: 'safe', riskAfter: 'external',
  alerts: [
    { kind: 'risk_raised', message: 'The flow risk rises from safe to external', before: 'safe', after: 'external' },
    { kind: 'new_host', nodeId: 'http1', path: 'config.url', message: 'New host evil.example', after: 'evil.example' },
    { kind: 'destination_changed', nodeId: 'http1', path: 'config.url', message: 'The destination of url changed' },
    { kind: 'credentialed_request_changed', nodeId: 'http1', message: 'A new input reaches a node that uses a credential' }
  ]
}

const detail = (over: Record<string, unknown> = {}) => ({
  ...summary(), issues: [{ path: '/nodes/tg1', code: 'tool_delay_exceeds_timeout', severity: 'warning', message: 'A warning ' + XSS }],
  running: false, nameChange: { before: 'Garage', after: 'Garage door' + XSS }, descriptionChange: null, diff, lastApprovedVersion: 2,
  diffApproved: { ...diff, alerts: [diff.alerts[1]] },
  flow: { id: 'f1', name: 'Garage', nodes: [{ id: 'tg1', type: 'action-telegram', config: { chatId: '12345' } }], connections: [] }, ...over
})

interface MockOptions { draft?: Record<string, unknown>; approve?: (body: any) => { status: number; json: unknown }; drafts?: unknown[] }

async function mockServer(page: Page, opts: MockOptions = {}) {
  const requests: Req[] = []
  let current = detail(opts.draft)
  let versions = {
    current: 5, lastAdminApproved: 3,
    versions: [
      { version: 5, actorKind: 'agent', tokenId: 'tokA', tokenName: 'laptop', adminApproved: false, draftId: 'd0', createdAt: iso(-3_600_000), deployed: true },
      { version: 4, actorKind: 'agent', tokenId: 'tokA', tokenName: 'laptop', adminApproved: false, createdAt: iso(-7_200_000), deployed: false },
      { version: 3, actorKind: 'editor', adminApproved: true, note: 'approved draft d7 from laptop', createdAt: iso(-86_400_000), deployed: false }
    ]
  }
  let flowRunning = true
  let f1Deployment: Record<string, unknown> = { version: 5, actorKind: 'agent', tokenName: 'laptop', deployedAt: iso(-3_600_000) }
  const handle = async (route: Route) => {
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname.replace(/^\/api/, '')
    const raw = req.postData()
    const body = raw ? JSON.parse(raw) : undefined
    requests.push({ method: req.method(), path, query: url.searchParams, body, headers: req.headers() })
    const ok = (data: unknown, status = 200) => route.fulfill({ status, json: { success: true, data } })
    const key = `${req.method()} ${path}`
    switch (key) {
      case 'GET /auth/status': return ok({ enabled: true, setupRequired: false })
      case 'GET /auth/session': return ok({ username: 'owner' })
      case 'GET /tls': return ok({ mode: 'auto', caFingerprint: 'SHA256:AB', sans: ['localhost'], notAfter: '2027-12-10T00:00:00Z', caCertUrl: '/piworker-ca.crt' })
      case 'GET /agent/settings': return ok({ agentDeploy: 'low', authorEnabled: true, mcpEnabled: true, lanAccess: false })
      case 'GET /agent/approvals/pending-count': return ok({ pending: 0, drafts: current.status === 'pending' ? 1 : 0 })
      case 'GET /agent/tools': return ok({ version: 1, tools: [] })
      case 'GET /agent/drafts': return ok({ drafts: opts.drafts ?? [summary({ status: current.status, stale: current.stale })], nextBefore: '' })
      case 'GET /agent/drafts/d1': return ok(current)
      case 'POST /agent/drafts/d1/approve': {
        if (opts.approve) {
          const r = opts.approve(body)
          return route.fulfill({ status: r.status, json: r.json })
        }
        current = { ...current, status: 'approved', appliedVersion: 4, currentVersion: 4, lastApprovedVersion: 4, diffApproved: null, decidedAt: iso(0) }
        return ok({ flowId: 'f1', version: 4, deployed: !!body?.deploy })
      }
      case 'POST /agent/drafts/d1/reject':
        current = { ...current, status: 'rejected', reason: body?.reason ?? '', decidedAt: iso(0) }
        return ok({})
      case 'GET /flows': return ok({ flows: [
        { id: 'f1', name: 'Garage', state: flowRunning ? 'running' : 'inactive', nodes: [{}], version: versions.current, agentChanged: versions.current !== versions.lastAdminApproved,
          deployment: f1Deployment },
        { id: 'f2', name: 'Lights', state: 'inactive', nodes: [{}], version: 1, agentChanged: false, deployment: null },
        { id: 'f3', name: 'Pump\u202Egnp', description: 'Waters the garden\u200B', state: 'inactive', nodes: [], version: 1, agentChanged: false, deployment: null }
      ], total: 3 })
      case 'GET /flows/f1': return ok({
        flow: { id: 'f1', name: 'Garage', state: flowRunning ? 'running' : 'inactive', nodes: [{}], connections: [], version: versions.current,
          agentChanged: versions.current !== versions.lastAdminApproved, deployment: f1Deployment },
        running: flowRunning
      })
      case 'GET /flows/f1/versions': return ok(versions)
      case 'POST /flows/f1/rollback':
        versions = { current: 6, lastAdminApproved: 6, versions: [{ version: 6, actorKind: 'editor', adminApproved: true, note: 'rollback to v3', createdAt: iso(0), deployed: true }, ...versions.versions.map(v => ({ ...v, deployed: false }))] }
        flowRunning = true
        f1Deployment = { version: 6, actorKind: 'editor', deployedAt: iso(0) }
        return ok({ flowId: 'f1', version: 6, deployed: true })
      case 'PATCH /flows/f2/toggle':
        return route.fulfill({ status: 409, json: { success: false, error: 'The flow was changed since you loaded it', data: { currentVersion: 2 } } })
    }
    return route.fulfill({ status: 404, json: { success: false, error: `Unexpected ${key}` } })
  }
  await page.route('**/api/**', handle)
  return requests
}

const last = (requests: Req[], method: string, path: string) => [...requests].reverse().find(r => r.method === method && r.path === path)

test('the drafts list shows the badge, risk, alerts, issues and the agent text as text', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents/drafts')
  const tab = page.getByRole('link', { name: /Drafts/ })
  await expect(tab).toHaveAttribute('aria-current', 'page')
  await expect(tab.getByLabel('1 to review')).toBeVisible()
  const card = page.getByRole('article', { name: 'Draft Garage' })
  await expect(card.getByText('Waiting for review')).toBeVisible()
  await expect(card.getByText('Risk: External')).toBeVisible()
  await expect(card.getByText('3 alerts')).toBeVisible()
  await expect(card.getByText('1 warning')).toBeVisible()
  await expect(card.getByText('Send the door state to my chat ' + XSS)).toBeVisible()
  await expect(page.locator('img')).toHaveCount(0)
  // The badge's count is a background request.
  const poll = last(requests, 'GET', '/agent/approvals/pending-count')
  expect(poll?.headers['x-piworker-background']).toBe('1')
  await page.screenshot({ path: 'test-results/agent-drafts.png', fullPage: true })
  await card.getByRole('link', { name: 'Review the draft for Garage' }).click()
  await expect(page).toHaveURL(/\/agents\/drafts\/d1$/)
})

test('a draft is reviewed alerts first and approved with Deploy now', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents/drafts/d1')
  await expect(page.getByRole('heading', { name: 'Draft for Garage' })).toBeFocused()
  // Alerts come before the changes, each with its label in words.
  const alerts = page.getByRole('region', { name: /Alerts/ })
  await expect(alerts.getByText('Credential used differently')).toBeVisible()
  await expect(alerts.getByText('New host', { exact: true })).toBeVisible()
  await expect(alerts.getByText('The values are in the node\'s changes below.')).toBeVisible()
  const alertsBox = await alerts.boundingBox()
  const changesBox = await page.getByRole('region', { name: /^Changes compared/ }).boundingBox()
  expect(alertsBox!.y).toBeLessThan(changesBox!.y)
  await expect(page.getByText('Risk raised from Safe to External.')).toBeVisible()
  // The field diff: Sensitive values hidden, destinations highlighted, agent values as text.
  const changes = page.getByRole('region', { name: /^Changes compared/ })
  const http = changes.getByRole('article', { name: 'Changed node HTTP Request http1' })
  await expect(http.getByText('hidden', { exact: true })).toHaveCount(2)
  await expect(http.locator('tr[data-destination="true"]')).toContainText('Destination')
  await expect(http.getByText('https://evil.example/' + XSS)).toBeVisible()
  await expect(changes.getByText('Moved on the canvas.')).toBeVisible()
  await expect(changes.getByText('w1 · output → Telegram (tg1) · input')).toBeVisible()
  // A rename is a change too, shown as agent text.
  const rename = changes.getByRole('table', { name: 'Name and description' })
  await expect(rename.getByRole('row', { name: /Name/ })).toContainText('Garage door' + XSS)
  await expect(page.getByRole('region', { name: /Compared with the version you last approved/ })).toBeVisible()
  await expect(page.getByText('A warning ' + XSS)).toBeVisible()
  await expect(page.locator('img')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()
  await page.screenshot({ path: 'test-results/agent-draft-review.png', fullPage: true })

  await page.getByLabel('Deploy now').check()
  await page.getByRole('button', { name: 'Approve and deploy…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Approve this draft?' })
  // The current version (3) is not the one you last approved (2): the
  // confirmation compares with both, never a bare "No alerts".
  await expect(dialog).toContainText('Compared with the current version, 3: Risk raised from Safe to External; 4 alerts')
  await expect(dialog).toContainText('Compared with version 2, the one you last approved: Risk raised from Safe to External; 1 alert: New host.')
  await expect(dialog).toContainText('Approving also approves these earlier agent changes.')
  await expect(dialog).toContainText('It renames the flow from “Garage” to “Garage door' + XSS + '”.')
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await dialog.getByRole('button', { name: 'Approve and deploy' }).click()
  await expect.poll(() => last(requests, 'POST', '/agent/drafts/d1/approve')?.body).toEqual({ deploy: true })
  await expect(page.getByRole('status').filter({ hasText: 'Approved: saved as version 4 and deployed.' })).toBeVisible()
  await expect(page.getByText('You approved this draft; it was saved as version 4.')).toBeVisible()
  await expect(page.getByRole('button', { name: /Approve/ })).toHaveCount(0)
  // The decision section is gone: focus moves to the outcome, not <body>.
  await expect(page.locator('#draft-outcome')).toBeFocused()
  await expect(page.locator('#draft-outcome')).toBeInViewport()
  // A decided draft says what it was written for and what it became, and
  // that its changes are recomputed against the version it became.
  await expect(page.getByText('Written for version 3; saved as version 4', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Changes compared with version 4 (current, recomputed now)' })).toBeVisible()
  await expect(page.getByText(/This draft was saved as version 4, so its changes are already part of it/)).toBeVisible()
})

test('a running flow is always redeployed; a stale draft cannot be approved and says why', async ({ page }) => {
  await mockServer(page, { draft: { running: true } })
  await page.goto('/agents/drafts/d1')
  const deploy = page.getByLabel('Deploy now')
  await expect(deploy).toBeChecked()
  await expect(deploy).toBeDisabled()
  await expect(page.getByText('The flow is running, so approving always redeploys it')).toBeVisible()

  await mockServer(page, { draft: { stale: true, currentVersion: 5 } })
  await page.reload()
  await expect(page.getByRole('alert').filter({ hasText: 'now at version 5' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Approve/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Reject…' })).toBeEnabled()
})

test('a refused approval keeps the draft pending and lists the issues', async ({ page }) => {
  await mockServer(page, {
    approve: () => ({ status: 400, json: { success: false, error: 'The flow cannot be deployed', data: { code: 'invalid_arguments', flowIssues: [{ path: '/nodes/tg1/config/botToken', code: 'undefined_secret', severity: 'error', message: 'Secret BOT is not defined' }] } } })
  })
  await page.goto('/agents/drafts/d1')
  await page.getByRole('button', { name: 'Approve and save…' }).click()
  await page.getByRole('dialog', { name: 'Approve this draft?' }).getByRole('button', { name: 'Approve and save' }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Not approved: The flow cannot be deployed' })).toBeVisible()
  await expect(page.getByText('Secret BOT is not defined')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Approve and save…' })).toBeVisible()
  await expect(page.locator('#draft-outcome')).toBeFocused()
})

test('rejecting sends the reason', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/agents/drafts/d1')
  await page.getByLabel('Reason for rejecting (optional, shown to the agent)').fill('Use the existing chat')
  await page.getByRole('button', { name: 'Reject…' }).click()
  await page.getByRole('dialog', { name: 'Reject this draft?' }).getByRole('button', { name: 'Reject draft' }).click()
  await expect.poll(() => last(requests, 'POST', '/agent/drafts/d1/reject')?.body).toEqual({ reason: 'Use the existing chat' })
  await expect(page.getByText('Reason: Use the existing chat')).toBeVisible()
  await expect(page.locator('#draft-outcome')).toBeFocused()
  await expect(page.getByText('Written for version 3', { exact: true })).toBeVisible()
})

test('flows show agent markers, the history rolls back with confirmation, and a stale list toggle deploys nothing', async ({ page }) => {
  const requests = await mockServer(page)
  await page.goto('/')
  const card = page.getByRole('listitem').filter({ hasText: 'Garage' })
  await expect(card.getByText('Deployed by agent laptop')).toBeVisible()
  await expect(card.getByText('Changed by agent')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Agents: 1 draft to review' })).toBeVisible()
  await page.screenshot({ path: 'test-results/flows-agent-marks.png', fullPage: true })

  await card.getByRole('button', { name: 'More actions for Garage' }).click()
  await page.getByRole('menuitem', { name: 'Version history' }).click()
  const history = page.getByRole('dialog', { name: 'Version history' })
  await expect(history.getByText('Not approved by you')).toHaveCount(2)
  await expect(history.getByText('Last approved by you')).toBeVisible()
  await history.getByRole('button', { name: 'Roll back to version 3…' }).click()
  const confirm = page.getByRole('dialog', { name: 'Roll back to version 3?' })
  await expect(confirm).toContainText('redeployed')
  await confirm.getByRole('button', { name: 'Roll back to version 3' }).click()
  await expect.poll(() => last(requests, 'POST', '/flows/f1/rollback')).toBeTruthy()
  await expect(history.getByText('Rolled back to version 3: saved as version 6 and deployed.')).toBeVisible()
  await expect(history.getByText('The current version is the one you last approved')).toBeVisible()
  await history.getByRole('button', { name: 'Close version history' }).click()
  await expect(card.getByText('Changed by agent')).toHaveCount(0)
  await expect(card.getByText('Deployed by agent laptop')).toHaveCount(0)

  // Agents may name and describe flows: hidden characters are revealed in
  // the card, the labels and the dialogs.
  const pump = page.getByRole('listitem').filter({ hasText: 'Pump' })
  await expect(pump.getByText('Waters the garden\\u200B')).toBeVisible()
  await expect(pump.getByRole('button', { name: 'More actions for Pump\\u202Egnp' })).toBeVisible()
  await expect(pump.getByRole('switch', { name: 'Deploy Pump\\u202Egnp' })).toBeVisible()

  await page.getByRole('switch', { name: 'Deploy Lights' }).click()
  await expect.poll(() => last(requests, 'PATCH', '/flows/f2/toggle')?.body).toEqual({ enabled: true, version: 1 })
  await expect(page.getByRole('dialog', { name: 'This flow was changed' })).toContainText('Version 2 of “Lights”')
})
