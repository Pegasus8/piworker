import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

// Compiling the server takes about a minute on a cold CI cache (cgo SQLite,
// every module downloaded): as long as Playwright's whole hook timeout. The
// specs that run a real server build it once per worker through this helper,
// from a beforeAll that first raises its own timeout to BUILD_TIMEOUT_MS.
export const BUILD_TIMEOUT_MS = 5 * 60_000

let built: string | undefined

export function serverBinary(env: NodeJS.ProcessEnv): string {
  if (built) return built
  const directory = mkdtempSync(join(tmpdir(), 'piworker-e2e-bin-'))
  const binary = join(directory, 'piworker')
  execFileSync('go', ['build', '-o', binary, '.'], { cwd: resolve(process.cwd(), '..'), env })
  process.once('exit', () => rmSync(directory, { recursive: true, force: true }))
  built = binary
  return built
}
