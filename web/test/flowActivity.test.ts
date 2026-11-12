import { expect, test } from 'bun:test'
import { summarizeRuns } from '../src/lib/flowActivity'

test('no runs means never run', () => {
  expect(summarizeRuns([])).toEqual({ lastRunAt: null, lastStatus: null, failed: 0, total: 0 })
})
test('the newest run and how many of the recent ones failed', () => {
  const runs = [
    { startedAt: '2026-11-12T10:00:00Z', status: 'error', errorCount: 1 },
    { startedAt: '2026-11-12T11:00:00Z', status: 'success', errorCount: 0 },
    { startedAt: '2026-11-12T09:00:00Z', status: 'success', errorCount: 2 }
  ] as any
  expect(summarizeRuns(runs)).toEqual({ lastRunAt: '2026-11-12T11:00:00Z', lastStatus: 'success', failed: 2, total: 3 })
})
