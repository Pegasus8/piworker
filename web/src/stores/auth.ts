import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { useApi } from '@/composables/useApi'

const TOKEN_KEY = 'piworker_auth_token'
const USER_KEY = 'piworker_auth_user'

export interface AuthUser {
  username: string
}

export const useAuthStore = defineStore('auth', () => {
  const api = useApi()

  // State - restore from localStorage
  const token = ref<string | null>(localStorage.getItem(TOKEN_KEY))
  const user = ref<AuthUser | null>((() => {
    // Guard against corrupt/tampered localStorage: an unhandled JSON.parse throw
    // here runs during store construction (app bootstrap) and would render a
    // blank white screen with only a console error.
    try {
      const stored = localStorage.getItem(USER_KEY)
      return stored ? JSON.parse(stored) : null
    } catch {
      localStorage.removeItem(USER_KEY)
      return null
    }
  })())
  const loading = ref(false)
  const error = ref<string | null>(null)
  const authEnabled = ref(true) // Safe default: assume auth required until checked

  // Computed
  const isAuthenticated = computed(() => !authEnabled.value || !!token.value)

  // Actions
  function setAuth(newToken: string, username: string) {
    token.value = newToken
    user.value = { username }
    localStorage.setItem(TOKEN_KEY, newToken)
    localStorage.setItem(USER_KEY, JSON.stringify({ username }))
    error.value = null
  }

  function clearAuth() {
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  function setError(message: string) {
    error.value = message
  }

  function setLoading(value: boolean) {
    loading.value = value
  }

  function clearError() {
    error.value = null
  }

  function logout() {
    clearAuth()
  }

  async function fetchAuthStatus() {
    try {
      const status = await api.getAuthStatus()
      authEnabled.value = status.enabled
    } catch {
      // On failure, keep default (auth enabled) — fail-secure
      authEnabled.value = true
    }
  }

  return {
    // State
    token,
    user,
    loading,
    error,
    authEnabled,
    // Computed
    isAuthenticated,
    // Actions
    setAuth,
    clearAuth,
    setError,
    setLoading,
    clearError,
    logout,
    fetchAuthStatus
  }
})
