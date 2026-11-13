// Deploy warnings: what a deploy went ahead with but the admin should know
// (POST /api/flows/{id}/deploy answers them as data.warnings, the warning
// issues of the server's Lint). An undefined secret that an optional
// credential references (unresolved_secret) is described in the interface
// language with the node's name; any other warning shows the server's
// message, which stays English like every server text.
import { msg } from '@/i18n'
import type { FlowIssue } from '@/lib/drafts'

/** The node ID and config property of an issue path ("/nodes/{id}/config/{prop}"). */
export function issueLocation(path: string): { nodeId: string; prop: string } | null {
  const match = /^\/nodes\/([^/]+)(?:\/config\/([^/]+))?/.exec(path)
  if (!match) return null
  const unescape = (s: string) => s.replace(/~1/g, '/').replace(/~0/g, '~')
  return { nodeId: unescape(match[1]), prop: match[2] ? unescape(match[2]) : '' }
}

/** The secret name an unresolved_secret message quotes ("…secret "NAME"…"). */
export function quotedSecret(message: string): string {
  return /secret "([^"]+)"/.exec(message)?.[1] ?? ''
}

/** One warning as a sentence; nodeName names a node by its ID. */
export function deployWarningText(w: FlowIssue, nodeName: (id: string) => string): string {
  const loc = issueLocation(w.path)
  const secret = quotedSecret(w.message)
  if (w.code === 'unresolved_secret' && loc && secret) {
    return msg().editor.unresolvedSecret(nodeName(loc.nodeId), loc.prop, secret)
  }
  return loc ? `${nodeName(loc.nodeId)}: ${w.message}` : w.message
}

/** The deploy's feedback text: a headline and each warning. */
export function deployWarningsSummary(warnings: FlowIssue[], nodeName: (id: string) => string): string {
  return [msg().editor.deployedWithWarnings(warnings.length), ...warnings.map(w => deployWarningText(w, nodeName))].join(' ')
}
