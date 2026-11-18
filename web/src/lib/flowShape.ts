// The two shapes of a flow: the stored one (flow.Flow: nodes whose category
// is input, processing or output, connections between ports) and the
// editor's (Vue Flow nodes whose category is trigger, process or action,
// edges between handles). Saving a flow and the flow assistant's "change
// the current flow" request convert the same way.
import type { FlowEdge, NodeCategory, StoredFlowConnection } from '@/types'

/** The stored categories (flow.Node.Category) as the editor names them. */
export const EDITOR_CATEGORY: Readonly<Record<string, NodeCategory>> = { input: 'trigger', processing: 'process', output: 'action' }

/** The editor's categories as they are stored. */
export const STORED_CATEGORY: Readonly<Record<string, string>> = { trigger: 'input', process: 'processing', action: 'output' }

/** An edge as a stored connection: a handle that is not set is the node's default port. */
export function storedConnection(edge: FlowEdge): StoredFlowConnection {
  return {
    id: edge.id,
    sourceNode: edge.source,
    sourcePort: edge.sourceHandle ?? 'output',
    targetNode: edge.target,
    targetPort: edge.targetHandle ?? 'input'
  }
}
