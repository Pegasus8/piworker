import { expect, test } from 'bun:test'
import { flowMarkers, rollbackConfirmText, rollbackTarget, versionActor, versionTags } from '../src/lib/flowMarks'
import type { VersionEntry, VersionList } from '../src/types'

const agentDeploy = { version: 4, actorKind: 'agent', tokenName: 'laptop', deployedAt: '2026-11-08T12:00:00Z' }

test('a flow an agent deployed is marked with the token, running or not', () => {
  expect(flowMarkers({ deployment: agentDeploy, running: true, version: 4 }).map(m => [m.kind, m.label])).toEqual([
    ['deployed_by_agent', 'Deployed by agent laptop']
  ])
  const stopped = flowMarkers({ deployment: agentDeploy, running: false, version: 4 })
  expect(stopped[0].label).toBe('Last deployed by agent laptop')
  expect(stopped[0].title).toContain('Version 4')
  expect(stopped[0].title).toContain('“laptop”')
  // The marker names the version deployed when the flow moved on since.
  expect(flowMarkers({ deployment: agentDeploy, running: true, version: 6 })[0].title).toContain('now at version 6')
  // Hidden characters in the token name are shown as escapes.
  expect(flowMarkers({ deployment: { ...agentDeploy, tokenName: 'lap‮top' }, running: true })[0].label).toBe('Deployed by agent lap\\u202Etop')
  expect(flowMarkers({ deployment: { ...agentDeploy, tokenName: '' }, running: true })[0].label).toBe('Deployed by an agent')
})

test('editor deployments and unmarked flows carry no agent marker', () => {
  expect(flowMarkers({ deployment: { ...agentDeploy, actorKind: 'editor', tokenName: undefined }, running: true })).toEqual([])
  expect(flowMarkers({ deployment: null, running: false })).toEqual([])
  expect(flowMarkers({})).toEqual([])
})

test('a current version an agent saved, unapproved, is marked as changed by an agent', () => {
  const marks = flowMarkers({ agentChanged: true, deployment: agentDeploy, running: true, version: 5 })
  expect(marks.map(m => m.kind)).toEqual(['deployed_by_agent', 'changed_by_agent'])
  expect(marks[1].label).toBe('Changed by agent')
  expect(marks[1].title).toContain('not approved')
  expect(marks[1].title).toContain('history')
})

const entry = (version: number, over: Partial<VersionEntry> = {}): VersionEntry => ({
  version, actorKind: 'editor', adminApproved: true, createdAt: '2026-11-08T12:00:00Z', deployed: false, ...over
})
const list = (over: Partial<VersionList> = {}): VersionList => ({
  current: 5, lastAdminApproved: 3,
  versions: [
    entry(5, { actorKind: 'agent', tokenName: 'laptop', adminApproved: false, deployed: true, draftId: 'd9' }),
    entry(4, { actorKind: 'agent', tokenName: 'laptop', adminApproved: false }),
    entry(3, { note: 'approved draft d1 from laptop', draftId: 'd1' })
  ],
  ...over
})

test('versions say who saved them and what they are now', () => {
  expect(versionActor(entry(1))).toBe('You (editor)')
  expect(versionActor(entry(2, { actorKind: 'agent', tokenName: 'lap​top' }))).toBe('Agent lap\\u200Btop')
  expect(versionActor(entry(2, { actorKind: 'agent', tokenId: 'tok9' }))).toBe('Agent tok9')
  expect(versionActor(entry(2, { actorKind: 'agent' }))).toBe('An agent')
  const l = list()
  expect(versionTags(l.versions[0], l)).toEqual(['Current', 'Deployed', 'Not approved by you', 'From a draft'])
  expect(versionTags(l.versions[1], l)).toEqual(['Not approved by you'])
  expect(versionTags(l.versions[2], l)).toEqual(['Last approved by you', 'From a draft'])
  expect(versionTags(entry(5), list({ lastAdminApproved: 5 }))).toEqual(['Current', 'Last approved by you'])
})

test('rolling back restores the version you last approved, when the current one is not it', () => {
  expect(rollbackTarget(list())).toEqual({ possible: true, version: 3, reason: '' })
  expect(rollbackTarget(list({ lastAdminApproved: 5 }))).toMatchObject({ possible: false, version: 5 })
  expect(rollbackTarget(list({ lastAdminApproved: 5 })).reason).toContain('nothing to roll back')
  expect(rollbackTarget(list({ lastAdminApproved: 0 }))).toMatchObject({ possible: false, version: 0 })
  expect(rollbackTarget(list({ lastAdminApproved: 0 })).reason).toContain('no version you approved')
})

test('the rollback confirmation names the version and says whether the flow is redeployed', () => {
  const running = rollbackConfirmText(list(), 'Garage‮', true)
  expect(running).toContain('version 3')
  expect(running).toContain('version 5')
  expect(running).toContain('Garage\\u202E')
  expect(running).toContain('redeployed')
  expect(rollbackConfirmText(list(), 'Garage', false)).toContain('not running')
})
