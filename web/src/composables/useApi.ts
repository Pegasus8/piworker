import axios from 'axios'
import type { Flow, FlowListItem } from '@/types'

const TOKEN_KEY = 'piworker_auth_token'

const api = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json'
  }
})

// Add Authorization header to all requests if token exists
api.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Handle 401 responses (unauthorized)
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (error.response?.status === 401) {
      // Clear token, then navigate to login via the router (preserving the
      // return URL) instead of a hard window.location reload that would wipe
      // SPA state and drop the user at '/' after re-login.
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem('piworker_auth_user')
      const { default: router } = await import('@/router')
      const current = router.currentRoute.value
      if (current.name !== 'login') {
        router.push({ name: 'login', query: { redirect: current.fullPath } })
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

  // Debug
  async function testNode(nodeId: string, payload: any): Promise<any> {
    const { data } = await api.post(`/nodes/${nodeId}/test`, { payload })
    return data
  }

  // Authentication
  async function login(username: string, password: string): Promise<{ token: string }> {
    const { data } = await api.post('/auth/login', { username, password })
    return data.data
  }

  async function register(username: string, password: string): Promise<void> {
    await api.post('/auth/register', { username, password })
  }

  async function getAuthStatus(): Promise<{ enabled: boolean }> {
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
    // Auth
    login,
    register,
    getAuthStatus
  }
}
