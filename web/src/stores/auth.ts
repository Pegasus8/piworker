import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { useApi } from '@/composables/useApi'

export interface AuthUser { username: string }
export const useAuthStore = defineStore('auth', () => {
  const api = useApi()
  // Migration: a previously stored JWT is never evidence of a valid session.
  try {
    localStorage.removeItem('piworker_auth_token')
    localStorage.removeItem('piworker_auth_user')
  } catch { /* Storage can be unavailable; cookies still work. */ }
  const user = ref<AuthUser | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const authEnabled = ref(true)
  const setupRequired = ref(false)
  const isAuthenticated = computed(() => !authEnabled.value || user.value !== null)

  function clearAuth() { user.value = null }
  function setError(message: string) { error.value = message }
  function setLoading(value: boolean) { loading.value = value }
  function clearError() { error.value = null }
  async function signIn(username: string, password: string) {
    user.value = await api.login(username, password)
    setupRequired.value = false
    error.value = null
  }
  async function logout() {
    await api.logout()
    clearAuth()
  }
  async function fetchAuthStatus() {
    try {
      const status = await api.getAuthStatus()
      authEnabled.value = status.enabled
      setupRequired.value = status.setupRequired
      user.value = null
      if (status.enabled && !status.setupRequired) {
        try { user.value = await api.getSession() }
        catch (e: any) { if (e.response?.status !== 401) throw e }
      }
    } catch {
      authEnabled.value = true
      user.value = null
      error.value = 'Cannot connect to PiWorker. Please try again.'
    }
  }
  return { user, loading, error, authEnabled, setupRequired, isAuthenticated,
    clearAuth, setError, setLoading, clearError, signIn, logout, fetchAuthStatus }
})
