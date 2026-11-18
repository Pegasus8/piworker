// What the settings' suggestions read (lib/suggestions.ts SuggestionContext),
// from the stores that hold the lists: the broker connections and Zigbee
// networks, the LLM connections and their models, the published tools and
// the knowledge collections, and the flow being edited. The node settings
// and the flow assistant's model field read it the same way; config is the
// settings of the form being edited. The stores load the lists
// (loadSuggestions).
import { computed, type ComputedRef } from 'vue'
import { useFlowsStore } from '@/stores/flows'
import { useLLMStore } from '@/stores/llm'
import { useZigbeeStore } from '@/stores/zigbee'
import type { SuggestionContext } from '@/lib/suggestions'

export function useSuggestionContext(config: () => Record<string, unknown>): ComputedRef<SuggestionContext> {
  const zigbee = useZigbeeStore()
  const llm = useLLMStore()
  const flows = useFlowsStore()
  return computed((): SuggestionContext => ({
    connections: zigbee.connections,
    networks: zigbee.networks,
    llmConnections: llm.connections,
    models: llm.models,
    modelErrors: llm.modelErrors,
    tools: llm.tools,
    collections: llm.collections,
    loaded: { llmConnections: llm.connectionsLoaded, tools: llm.toolsLoaded, collections: llm.collectionsLoaded },
    flowId: flows.currentFlow?.id ?? '',
    config: config()
  }))
}
