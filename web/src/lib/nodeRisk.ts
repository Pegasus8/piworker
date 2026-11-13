// A node type's risk as the editor shows it (GET /api/node-types risk):
// the same levels agents see (safe, external, privileged), told by icon and
// words, never by color alone. The palette marks only external and
// privileged types, so the common safe ones stay uncluttered; the settings
// panel shows every level with its explanation.
import { msg } from '@/i18n'
import { riskInfo, type RiskInfo } from '@/lib/drafts'

export interface NodeRisk extends RiskInfo {
  /** What the level means, in a sentence. */
  hint: string
  /** Whether the palette marks it (anything but safe). */
  marked: boolean
}

/** How a node type's risk is shown; an unknown or missing level reads as the highest. */
export function nodeRisk(risk: string | undefined): NodeRisk {
  const t = msg().editor.riskHints
  const info = riskInfo(risk ?? '')
  const key = risk === 'safe' || risk === 'external' || risk === 'privileged' ? risk : 'unknown'
  return { ...info, hint: t[key], marked: key !== 'safe' }
}
