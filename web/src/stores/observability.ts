import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useApi } from '@/composables/useApi'
import type { FlowEvent, FlowRun, NodePhase } from '@/types'

const MAX_DEBUG_ENTRIES = 200

export interface NodeRunState {
  phase: NodePhase
  durMs?: number
  error?: string
  ts: number
}

export interface DebugEntry {
  nodeId: string
  nodeType: string
  corrId?: string
  debug: unknown
  ts: number
}

// useObservabilityStore tracks the live execution state of the flow currently
// open in the editor: per-node status (for the canvas), a debug message log (the
// inspector) and the persisted run history. It owns the SSE subscription.
export const useObservabilityStore = defineStore('observability', () => {
  const api = useApi()

  const connected = ref(false)
  const activeFlowId = ref<string | null>(null)
  const nodeStates = ref<Record<string, NodeRunState>>({})
  const debugLog = ref<DebugEntry[]>([])
  const runs = ref<FlowRun[]>([])

  let unsub: (() => void) | null = null
  let runsTimer: ReturnType<typeof setTimeout> | null = null

  function nodeState(nodeId: string): NodeRunState | undefined {
    return nodeStates.value[nodeId]
  }

  function handleEvent(e: FlowEvent) {
    if (e.phase === 'debug') {
      debugLog.value.unshift({
        nodeId: e.nodeId,
        nodeType: e.nodeType,
        corrId: e.corrId,
        debug: e.debug,
        ts: e.ts ?? Date.now()
      })
      if (debugLog.value.length > MAX_DEBUG_ENTRIES) {
        debugLog.value.length = MAX_DEBUG_ENTRIES
      }
      return
    }

    // running | success | error → update the node's live state on the canvas.
    nodeStates.value[e.nodeId] = {
      phase: e.phase,
      durMs: e.durMs,
      error: e.error,
      ts: e.ts ?? Date.now()
    }

    // A terminal event means a run advanced; refresh history (debounced).
    if (e.phase === 'success' || e.phase === 'error') {
      scheduleRunsRefresh()
    }
  }

  function scheduleRunsRefresh() {
    if (runsTimer || !activeFlowId.value) return
    runsTimer = setTimeout(() => {
      runsTimer = null
      void refreshRuns()
    }, 800)
  }

  async function refreshRuns() {
    if (!activeFlowId.value) return
    try {
      runs.value = await api.getFlowRuns(activeFlowId.value)
    } catch {
      /* best-effort */
    }
  }

  function connect(flowId: string) {
    if (activeFlowId.value === flowId && connected.value) return
    disconnect()
    activeFlowId.value = flowId
    void refreshRuns()
    unsub = api.subscribeFlowEvents(flowId, {
      onEvent: handleEvent,
      onStatus: (c) => {
        connected.value = c
      }
    })
  }

  function disconnect() {
    if (unsub) {
      unsub()
      unsub = null
    }
    if (runsTimer) {
      clearTimeout(runsTimer)
      runsTimer = null
    }
    connected.value = false
  }

  // reset clears everything (used when leaving the editor).
  function reset() {
    disconnect()
    activeFlowId.value = null
    nodeStates.value = {}
    debugLog.value = []
    runs.value = []
  }

  function clearActivity() {
    nodeStates.value = {}
    debugLog.value = []
  }

  return {
    connected,
    activeFlowId,
    nodeStates,
    debugLog,
    runs,
    nodeState,
    connect,
    disconnect,
    refreshRuns,
    reset,
    clearActivity
  }
})
