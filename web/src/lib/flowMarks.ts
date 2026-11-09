// What the flows list and the editor mark on a flow an agent touched, and the
// rules of its version history (docs/design/mcp.md §10, H6): "Deployed by
// agent <token>" when an agent deployed it last, "Changed by agent" when its
// current version was saved by an agent and not approved by the admin, and a
// rollback to the version the admin last approved. Token names are agent-side
// text: the labels escape hidden characters (visibleText).
import { formatDateTime, visibleText } from '@/lib/agentAccess'
import type { DeploymentMark, VersionEntry, VersionList } from '@/types'

export interface FlowMarker {
  kind: 'deployed_by_agent' | 'changed_by_agent'
  /** Short text for the badge (never color alone). */
  label: string
  /** The longer explanation (tooltip and screen readers). */
  title: string
}

export interface MarkedFlow {
  deployment?: DeploymentMark | null
  agentChanged?: boolean
  running?: boolean
  /** The flow's current version. */
  version?: number
}

/** The agent markers of a flow, in display order. */
export function flowMarkers(f: MarkedFlow): FlowMarker[] {
  const out: FlowMarker[] = []
  const d = f.deployment
  if (d && d.actorKind === 'agent') {
    const name = visibleText(d.tokenName ?? '')
    const who = name ? `agent ${name}` : 'an agent'
    const when = formatDateTime(d.deployedAt)
    const moved = f.version && d.version && f.version !== d.version ? ` The flow is now at version ${f.version}.` : ''
    out.push({
      kind: 'deployed_by_agent',
      label: `${f.running ? 'Deployed' : 'Last deployed'} by ${who}`,
      title: `Version ${d.version} was deployed by ${name ? `the agent token “${name}”` : 'an agent token'}${when ? ` on ${when}` : ''}.${moved}`
    })
  }
  if (f.agentChanged) {
    out.push({
      kind: 'changed_by_agent',
      label: 'Changed by agent',
      title: 'The current version was saved by an agent and not approved by you. Open the version history to roll back to the version you last approved.'
    })
  }
  return out
}

/** Who saved a version. */
export function versionActor(e: Pick<VersionEntry, 'actorKind' | 'tokenName' | 'tokenId'>): string {
  if (e.actorKind !== 'agent') return 'You (editor)'
  const name = visibleText(e.tokenName || e.tokenId || '')
  return name ? `Agent ${name}` : 'An agent'
}

/** What a version is now: current, deployed, approved or not, from a draft. */
export function versionTags(e: VersionEntry, list: Pick<VersionList, 'current' | 'lastAdminApproved'>): string[] {
  const tags: string[] = []
  if (e.version === list.current) tags.push('Current')
  if (e.deployed) tags.push('Deployed')
  if (!e.adminApproved) tags.push('Not approved by you')
  else if (e.version === list.lastAdminApproved) tags.push('Last approved by you')
  if (e.draftId) tags.push('From a draft')
  return tags
}

export interface RollbackTarget {
  possible: boolean
  /** The version a rollback restores (0: none). */
  version: number
  /** Why there is nothing to roll back ('' when possible). */
  reason: string
}

/** Whether a rollback is possible and which version it restores. */
export function rollbackTarget(list: Pick<VersionList, 'current' | 'lastAdminApproved'>): RollbackTarget {
  const v = list.lastAdminApproved
  if (!v) return { possible: false, version: 0, reason: 'There is no version you approved to roll back to (no version you approved is kept).' }
  if (v >= list.current) return { possible: false, version: v, reason: 'The current version is the one you last approved: there is nothing to roll back.' }
  return { possible: true, version: v, reason: '' }
}

/** The rollback confirmation: which version comes back and what happens to a running flow. */
export function rollbackConfirmText(list: Pick<VersionList, 'current' | 'lastAdminApproved'>, flowName: string, running: boolean): string {
  const name = visibleText(flowName)
  const restore = `“${name}” goes back to version ${list.lastAdminApproved}, the version you last approved: it is saved again as a new version, after version ${list.current}. Later versions stay in the history.`
  return running
    ? `${restore} The flow is running, so it is redeployed with that version.`
    : `${restore} The flow is not running; nothing is deployed.`
}
