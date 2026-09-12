import { test, expect } from '@playwright/test'
import { spawn, execFileSync, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
let server: ChildProcess
let directory: string
let binary: string
let setupCode: string
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('PIWORKER_')))
test.beforeAll(async () => {
 directory = mkdtempSync(join(tmpdir(), 'piworker-browser-'))
 binary = join(directory, 'piworker')
 execFileSync('go', ['build', '-o', binary, '.'], { cwd: resolve(process.cwd(), '..'), env })
 server = spawn(binary, ['-db', join(directory, 'auth.db'), '-addr', '127.0.0.1:18095', '-cors-origins', 'http://127.0.0.1:18096'], { cwd: directory, env })
 setupCode = await new Promise<string>((resolve, reject) => {
  let output = ''
  const timer = setTimeout(() => reject(new Error('Setup server did not start')), 15000)
  server.once('exit', () => { clearTimeout(timer); reject(new Error('Setup server exited')) })
  server.stdout!.on('data', data => {
   output += data.toString()
   const match = output.match(/Setup code .*: ([A-Za-z0-9_-]{43})/)
   if (match) { clearTimeout(timer); resolve(match[1]) }
  })
 })
})
test.afterAll(async () => {
 if (server && server.exitCode === null) {
  await new Promise<void>(resolve => { server.once('exit', () => resolve()); server.kill('SIGTERM') })
 }
 if (directory) rmSync(directory, { recursive: true, force: true })
})
test('installer setup, remembered session, password change, logout and local recovery', async ({ page, context }) => {
 await page.goto('/')
 await expect(page.getByRole('heading', { name: 'Create your administrator account' })).toBeVisible()
 await expect(page.getByRole('note', { name: 'Unencrypted connection' })).toBeVisible()
 await page.screenshot({ path: 'test-results/auth-setup.png', fullPage: true })
 await page.getByLabel('Setup code', { exact: true }).fill('wrong')
 await page.getByLabel('Username', { exact: true }).fill('owner')
 await page.getByLabel('Password', { exact: true }).fill('first-password')
 await page.getByLabel('Confirm password', { exact: true }).fill('first-password')
 await page.getByRole('button', { name: 'Create account', exact: true }).click()
 await expect(page.getByRole('alert')).toContainText('Invalid or expired')
 await page.getByLabel('Setup code', { exact: true }).fill(setupCode)
 await page.getByRole('button', { name: 'Create account', exact: true }).click()
 await expect(page.getByRole('button', { name: 'Account', exact: true })).toBeVisible()
 const cookie = (await context.cookies()).find(c => c.name === 'piworker_session')!
 expect(cookie.httpOnly).toBe(true); expect(cookie.secure).toBe(false); expect(cookie.sameSite).toBe('Strict')
 expect(await page.evaluate(() => localStorage.getItem('piworker_auth_token'))).toBeNull()
 await page.reload()
 await expect(page.getByRole('button', { name: 'Account', exact: true })).toBeVisible()
 await page.getByRole('button', { name: 'Account', exact: true }).click()
 await expect(page.getByLabel('Current password', { exact: true })).toBeVisible()
 await page.screenshot({ path: 'test-results/auth-account.png', fullPage: true })
 await page.getByLabel('Current password', { exact: true }).fill('first-password')
 await page.getByLabel('New password', { exact: true }).fill('second-password')
 await page.getByLabel('Confirm password', { exact: true }).fill('second-password')
 await page.getByRole('button', { name: 'Change password', exact: true }).click()
 await expect(page.getByRole('heading', { name: 'Sign in to continue' })).toBeVisible()
 await page.getByLabel('Username', { exact: true }).fill('owner')
 await page.getByLabel('Password', { exact: true }).fill('second-password')
 await page.getByRole('button', { name: 'Sign in', exact: true }).click()
 await expect(page.getByRole('button', { name: 'Account', exact: true })).toBeVisible()
 await page.getByRole('button', { name: 'Logout', exact: true }).click()
 await expect(page.getByRole('heading', { name: 'Sign in to continue' })).toBeVisible()
 await page.getByRole('button', { name: 'Forgot password?', exact: true }).click()
 await expect(page.getByText('Run this on the device hosting PiWorker')).toBeVisible()
 const output = execFileSync(binary, ['-db', join(directory, 'auth.db'), '-recover-account', 'owner'], { cwd: directory, env, encoding: 'utf8' })
 const recovery = output.match(/recovery code .*: ([A-Za-z0-9_-]{43})/)![1]
 await page.getByLabel('Recovery code', { exact: true }).fill(recovery)
 await page.getByLabel('Username', { exact: true }).fill('owner')
 await page.getByLabel('Password', { exact: true }).fill('third-password')
 await page.getByLabel('Confirm password', { exact: true }).fill('third-password')
 await page.getByRole('button', { name: 'Reset password', exact: true }).click()
 await expect(page.getByRole('button', { name: 'Account', exact: true })).toBeVisible()
})
