// Who started a run (flow_runs.actor, from the run's "actor" meta; set by
// synchronous tool calls): "editor" for the editor's Test tool,
// "token:<name>" for an agent token, empty when the flow's own trigger fired.
import { visibleText } from '@/lib/agentAccess'

export interface RunActor {
  kind: 'trigger' | 'editor' | 'agent' | 'other'
  label: string
  /** A longer explanation (tooltip). */
  title: string
}

const TOKEN_PREFIX = 'token:'

export function runActor(actor: string | undefined): RunActor {
  if (!actor) return { kind: 'trigger', label: 'Trigger', title: 'Started by the flow\'s trigger' }
  if (actor === 'editor') return { kind: 'editor', label: 'Editor', title: 'Started from the editor (Test tool)' }
  if (actor.startsWith(TOKEN_PREFIX)) {
    const name = visibleText(actor.slice(TOKEN_PREFIX.length)) || '(unnamed)'
    return { kind: 'agent', label: `Agent ${name}`, title: `Started by the agent token “${name}”` }
  }
  const shown = visibleText(actor)
  return { kind: 'other', label: shown, title: `Started by ${shown}` }
}
