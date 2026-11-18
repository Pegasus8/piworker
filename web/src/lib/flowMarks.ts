// What the flows list and the editor mark on a flow an agent touched, and the
// rules of its version history (docs/design/mcp.md §10, H6): "Deployed by
// agent <token>" when an agent deployed it last, "Changed by agent" when its
// current version was saved by an agent and not approved by the admin, and a
// rollback to the version the admin last approved. Token names are agent-side
// text: the labels escape hidden characters (visibleText).
import { formatDateTime, visibleText } from '@/lib/agentAccess'
import { msg } from '@/i18n'
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
  const t = msg().drafts.marks
  const out: FlowMarker[] = []
  const d = f.deployment
  if (d && d.actorKind === 'agent') {
    const name = visibleText(d.tokenName ?? '')
    const when = formatDateTime(d.deployedAt)
    const moved = f.version && d.version && f.version !== d.version ? f.version : undefined
    out.push({
      kind: 'deployed_by_agent',
      label: t.deployedBy(f.running, name),
      title: t.deployedTitle(d.version, name, when, moved)
    })
  }
  if (f.agentChanged) {
    out.push({
      kind: 'changed_by_agent',
      label: t.changedByAgent,
      title: t.changedTitle
    })
  }
  return out
}

/** Who saved a version. */
export function versionActor(e: Pick<VersionEntry, 'actorKind' | 'tokenName' | 'tokenId'>): string {
  const t = msg().drafts.marks
  if (e.actorKind !== 'agent') return t.you
  const name = visibleText(e.tokenName || e.tokenId || '')
  return name ? t.agent(name) : t.anAgent
}

export interface VersionTag {
  /** Stable, for styling: never compare the label. */
  kind: 'current' | 'deployed' | 'notApproved' | 'lastApproved' | 'fromDraft'
  label: string
}

/** What a version is now: current, deployed, approved or not, from a draft. */
export function versionTags(e: VersionEntry, list: Pick<VersionList, 'current' | 'lastAdminApproved'>): VersionTag[] {
  const t = msg().drafts.marks
  const tags: VersionTag[] = []
  const add = (kind: VersionTag['kind']) => tags.push({ kind, label: t[kind] })
  if (e.version === list.current) add('current')
  if (e.deployed) add('deployed')
  if (!e.adminApproved) add('notApproved')
  else if (e.version === list.lastAdminApproved) add('lastApproved')
  if (e.draftId) add('fromDraft')
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
  if (!v) return { possible: false, version: 0, reason: msg().drafts.marks.noRollback }
  if (v >= list.current) return { possible: false, version: v, reason: msg().drafts.marks.nothingToRollBack }
  return { possible: true, version: v, reason: '' }
}

/** The rollback confirmation: which version comes back and what happens to a running flow. */
export function rollbackConfirmText(list: Pick<VersionList, 'current' | 'lastAdminApproved'>, flowName: string, running: boolean): string {
  const name = visibleText(flowName)
  return msg().drafts.marks.rollbackConfirm(name, list.lastAdminApproved, list.current, running)
}
