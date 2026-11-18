import { expect, test } from 'bun:test'
import { runActor } from '../src/lib/runActor'

// Runs record who started them (flow_runs.actor, from the run's "actor"
// meta): the editor's Test tool, an agent token, or nobody (the trigger).
test('runs say who started them', () => {
  expect(runActor(undefined)).toMatchObject({ kind: 'trigger', label: 'Trigger' })
  expect(runActor('')).toMatchObject({ kind: 'trigger', label: 'Trigger' })
  expect(runActor('editor')).toMatchObject({ kind: 'editor', label: 'Editor' })
  const agent = runActor('token:laptop')
  expect(agent).toMatchObject({ kind: 'agent', label: 'Agent laptop' })
  expect(agent.title).toContain('“laptop”')
})

test('unexpected actors are shown as they are, with hidden characters escaped', () => {
  expect(runActor('scheduler')).toMatchObject({ kind: 'other', label: 'scheduler' })
  expect(runActor('token:a‮b').label).toBe('Agent a\\u202Eb')
  expect(runActor('token:')).toMatchObject({ kind: 'agent', label: 'Agent (unnamed)' })
})
