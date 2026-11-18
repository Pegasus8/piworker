// Who started a run (flow_runs.actor, from the run's "actor" meta; set by
// synchronous tool calls): "editor" for the editor's Test tool,
// "token:<name>" for an agent token, empty when the flow's own trigger fired.
import { visibleText } from '@/lib/agentAccess'
import { msg } from '@/i18n'

export interface RunActor {
  kind: 'trigger' | 'editor' | 'agent' | 'other'
  label: string
  /** A longer explanation (tooltip). */
  title: string
}

const TOKEN_PREFIX = 'token:'

export function runActor(actor: string | undefined): RunActor {
  const t = msg().agents.actor
  if (!actor) return { kind: 'trigger', label: t.trigger, title: t.triggerTitle }
  if (actor === 'editor') return { kind: 'editor', label: t.editor, title: t.editorTitle }
  if (actor.startsWith(TOKEN_PREFIX)) {
    const name = visibleText(actor.slice(TOKEN_PREFIX.length)) || t.unnamed
    return { kind: 'agent', label: t.agent(name), title: t.agentTitle(name) }
  }
  const shown = visibleText(actor)
  return { kind: 'other', label: shown, title: t.otherTitle(shown) }
}
