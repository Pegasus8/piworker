import type { FlowRun } from '@/types'

// What a flow card says about recent runs: when it last ran, how that run
// ended and how many of the recent runs had errors.
export interface RunSummary { lastRunAt: string | null; lastStatus: FlowRun['status'] | null; failed: number; total: number }

export function summarizeRuns(runs: Pick<FlowRun, 'startedAt' | 'status' | 'errorCount'>[]): RunSummary {
  if (!runs.length) return { lastRunAt: null, lastStatus: null, failed: 0, total: 0 }
  const newest = runs.reduce((a, b) => (new Date(b.startedAt).getTime() > new Date(a.startedAt).getTime() ? b : a))
  return {
    lastRunAt: newest.startedAt,
    lastStatus: newest.status,
    failed: runs.filter(r => r.status === 'error' || r.errorCount > 0).length,
    total: runs.length
  }
}
