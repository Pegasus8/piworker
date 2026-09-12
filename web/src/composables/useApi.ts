import axios from 'axios'
import type { Flow, FlowListItem, FlowEvent, FlowRun, NodeEventRecord, NodeTestResult } from '@/types'

const api = axios.create({
  baseURL: '/api',
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json',
    'X-PiWorker-Request': '1'
  }
})

// Handle 401 responses (unauthorized)
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (error.response?.status === 401 && !error.config?.url?.startsWith('/auth/')) {
      const { useAuthStore } = await import('@/stores/auth')
      useAuthStore().clearAuth()
      const { default: router } = await import('@/router')
      const current = router.currentRoute.value
      if (current.name !== 'login') {
        await router.push({ name: 'login', query: { redirect: current.fullPath } })
      }
    }
    return Promise.reject(error)
  }
)

export function useApi() {
  // Flows
  async function getFlows(): Promise<FlowListItem[]> {
    const { data } = await api.get('/flows')
    // Transform API response to match FlowListItem interface
    return data.data.flows.map((flow: any) => ({
      id: flow.id,
      name: flow.name,
      description: flow.description,
      enabled: flow.enabled ?? false,
      running: flow.state === 'running',
      nodeCount: flow.nodes?.length ?? 0,
      createdAt: flow.createdAt,
      updatedAt: flow.updatedAt
    }))
  }

  async function getFlow(id: string): Promise<Flow> {
    const { data } = await api.get(`/flows/${id}`)
    // API returns { flow, running } - extract flow and add running status
    const flow = data.data.flow
    flow.running = data.data.running
    return flow
  }

  async function createFlow(flow: Partial<Flow>): Promise<Flow> {
    const { data } = await api.post('/flows', flow)
    return data.data
  }

  async function updateFlow(id: string, flow: Partial<Flow>): Promise<Flow> {
    const { data } = await api.put(`/flows/${id}`, flow)
    return data.data
  }

  async function deleteFlow(id: string): Promise<void> {
    await api.delete(`/flows/${id}`)
  }

  async function toggleFlow(id: string, enabled: boolean): Promise<void> {
    await api.patch(`/flows/${id}/toggle`, { enabled })
  }

  // Flow lifecycle: duplicate / export / import.
  async function duplicateFlow(id: string): Promise<Flow> {
    const { data } = await api.post(`/flows/${id}/duplicate`)
    return data.data
  }

  async function exportFlow(id: string): Promise<Flow> {
    const { data } = await api.get(`/flows/${id}/export`)
    return data.data
  }

  async function importFlow(flow: unknown): Promise<Flow> {
    const { data } = await api.post('/flows/import', flow)
    return data.data
  }

  // Secrets (values are write-only; the listing returns names).
  async function getSecrets(): Promise<string[]> {
    const { data } = await api.get('/secrets')
    return data.data?.names ?? []
  }

  async function setSecret(name: string, value: string): Promise<void> {
    await api.put(`/secrets/${encodeURIComponent(name)}`, { value })
  }

  async function deleteSecret(name: string): Promise<void> {
    await api.delete(`/secrets/${encodeURIComponent(name)}`)
  }

  // Node types
  async function getNodeTypes() {
    const { data } = await api.get('/node-types')
    return data.data
  }

  // Flow execution. The deploy/stop endpoints already return the resulting
  // running state, so callers can use it directly instead of issuing a second
  // full getFlow round-trip just to read one boolean.
  async function deployFlow(id: string): Promise<boolean> {
    const { data } = await api.post(`/flows/${id}/deploy`)
    return data.data?.running ?? true
  }

  async function stopFlow(id: string): Promise<boolean> {
    const { data } = await api.post(`/flows/${id}/stop`)
    return data.data?.running ?? false
  }

  // Debug: run a single processing node once against a sample payload.
  async function testNode(nodeType: string, config: Record<string, any>, payload: unknown, context: { topic?: string; meta?: Record<string, unknown> } = {}, signal?: AbortSignal): Promise<NodeTestResult> {
    const { data } = await api.post(`/nodes/${nodeType}/test`, { config, payload, ...context }, { signal })
    return data.data
  }

  // Observability: run/event history.
  async function getFlowRuns(flowId: string, limit = 50): Promise<FlowRun[]> {
    const { data } = await api.get(`/flows/${flowId}/runs`, { params: { limit } })
    return data.data?.runs ?? []
  }

  async function getRunEvents(flowId: string, runId: string): Promise<NodeEventRecord[]> {
    const { data } = await api.get(`/flows/${flowId}/runs/${runId}/events`)
    return data.data?.events ?? []
  }

  // subscribeFlowEvents opens the live Server-Sent Events stream for a flow using
  // fetch + ReadableStream with the HttpOnly session cookie. It auto-reconnects
  // with backoff and returns a close().
  function subscribeFlowEvents(
    flowId: string,
    handlers: { onEvent: (e: FlowEvent) => void; onStatus?: (connected: boolean) => void }
  ): () => void {
    const controller = new AbortController()
    let closed = false

    async function run() {
      let backoff = 1000
      while (!closed) {
        try {
          const resp = await fetch(`/api/flows/${flowId}/events`, {
            credentials: 'same-origin',
            signal: controller.signal
          })
          if (resp.status === 401) {
            handlers.onStatus?.(false)
            const { useAuthStore } = await import('@/stores/auth')
            useAuthStore().clearAuth()
            const { default: router } = await import('@/router')
            if (router.currentRoute.value.name !== 'login') {
              await router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
            }
            closed = true
            break
          }
          if (!resp.ok || !resp.body) throw new Error(`SSE status ${resp.status}`)

          handlers.onStatus?.(true)
          backoff = 1000

          const reader = resp.body.getReader()
          const decoder = new TextDecoder()
          let buffer = ''
          while (!closed) {
            const { done, value } = await reader.read()
            if (done) break
            buffer += decoder.decode(value, { stream: true })
            let sep: number
            while ((sep = buffer.indexOf('\n\n')) !== -1) {
              const frame = buffer.slice(0, sep)
              buffer = buffer.slice(sep + 2)
              // The server emits exactly one `data: <json>` per frame; anything
              // else (a `: keepalive` comment) is skipped.
              if (frame.startsWith('data:')) {
                const json = frame.slice(5).trim()
                if (json) {
                  try {
                    handlers.onEvent(JSON.parse(json))
                  } catch {
                    /* ignore malformed frame */
                  }
                }
              }
            }
          }
        } catch {
          if (closed || controller.signal.aborted) break
        }
        handlers.onStatus?.(false)
        if (closed) break
        await new Promise((r) => setTimeout(r, backoff))
        backoff = Math.min(backoff * 2, 15000)
      }
    }

    void run()
    return () => {
      closed = true
      controller.abort()
    }
  }

  // The browser manages the HttpOnly cookie; no session secret enters JavaScript.
  async function login(username: string, password: string): Promise<{ username: string }> {
    const { data } = await api.post('/auth/login', { username, password })
    return data.data
  }
  async function setup(username: string, password: string, code: string): Promise<void> {
    await api.post('/auth/setup', { username, password, code })
  }
  async function recover(username: string, password: string, code: string): Promise<void> {
    await api.post('/auth/recover', { username, password, code })
  }
  async function changePassword(currentPassword: string, password: string): Promise<void> {
    await api.post('/auth/password', { currentPassword, password })
  }
  async function logout(): Promise<void> { await api.post('/auth/logout') }
  async function getSession(): Promise<{ username: string }> {
    const { data } = await api.get('/auth/session')
    return data.data
  }
  async function getAuthStatus(): Promise<{ enabled: boolean; setupRequired: boolean }> {
    const { data } = await api.get('/auth/status')
    return data.data
  }

  return {
    getFlows,
    getFlow,
    createFlow,
    updateFlow,
    deleteFlow,
    toggleFlow,
    getNodeTypes,
    deployFlow,
    stopFlow,
    testNode,
    // Lifecycle
    duplicateFlow,
    exportFlow,
    importFlow,
    // Secrets
    getSecrets,
    setSecret,
    deleteSecret,
    // Observability
    getFlowRuns,
    getRunEvents,
    subscribeFlowEvents,
    // Auth
    login,
    setup,
    recover,
    changePassword,
    logout,
    getSession,
    getAuthStatus
  }
}
