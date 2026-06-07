<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Label } from '@/components/ui'
import { Workflow, LogIn, UserPlus, AlertCircle, Loader2 } from 'lucide-vue-next'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const api = useApi()

// Form state
const isRegisterMode = ref(false)
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const localError = ref<string | null>(null)

// Computed
const isLoading = computed(() => authStore.loading)
const formValid = computed(() => {
  if (!username.value.trim() || !password.value) return false
  if (isRegisterMode.value && password.value !== confirmPassword.value) return false
  return true
})

const passwordMismatch = computed(() => {
  return isRegisterMode.value && confirmPassword.value && password.value !== confirmPassword.value
})

// Actions
async function handleSubmit() {
  if (!formValid.value) return

  localError.value = null
  authStore.setLoading(true)
  authStore.clearError()

  try {
    if (isRegisterMode.value) {
      // Register then login
      await api.register(username.value, password.value)
    }

    // Login
    const response = await api.login(username.value, password.value)
    authStore.setAuth(response.token, username.value)

    // Redirect to original destination or home
    const redirect = route.query.redirect as string
    router.push(redirect || '/')
  } catch (e: any) {
    const message = e.response?.data?.error || e.message || 'Authentication failed'
    localError.value = message
    authStore.setError(message)
  } finally {
    authStore.setLoading(false)
  }
}

function toggleMode() {
  isRegisterMode.value = !isRegisterMode.value
  localError.value = null
  authStore.clearError()
  confirmPassword.value = ''
}
</script>

<template>
  <div class="min-h-screen bg-background flex items-center justify-center p-4">
    <div class="w-full max-w-md space-y-6">
      <!-- Logo -->
      <div class="text-center">
        <div class="inline-flex items-center justify-center w-16 h-16 rounded-full bg-primary/10 mb-4">
          <Workflow class="h-8 w-8 text-primary" />
        </div>
        <h1 class="text-2xl font-bold">PiWorker</h1>
        <p class="text-muted-foreground mt-1">
          {{ isRegisterMode ? 'Create your account' : 'Sign in to continue' }}
        </p>
      </div>

      <!-- Login Card -->
      <Card class="p-6">
        <form @submit.prevent="handleSubmit" class="space-y-4">
          <!-- Error Message -->
          <div
            v-if="localError"
            class="flex items-center gap-2 p-3 rounded-lg bg-destructive/10 text-destructive text-sm"
          >
            <AlertCircle class="h-4 w-4 flex-shrink-0" />
            <span>{{ localError }}</span>
          </div>

          <!-- Username -->
          <div class="space-y-2">
            <Label for="username">Username</Label>
            <Input
              id="username"
              v-model="username"
              type="text"
              placeholder="Enter your username"
              autocomplete="username"
              :disabled="isLoading"
            />
          </div>

          <!-- Password -->
          <div class="space-y-2">
            <Label for="password">Password</Label>
            <Input
              id="password"
              v-model="password"
              type="password"
              placeholder="Enter your password"
              autocomplete="current-password"
              :disabled="isLoading"
            />
          </div>

          <!-- Confirm Password (Register only) -->
          <div v-if="isRegisterMode" class="space-y-2">
            <Label for="confirmPassword">Confirm Password</Label>
            <Input
              id="confirmPassword"
              v-model="confirmPassword"
              type="password"
              placeholder="Confirm your password"
              autocomplete="new-password"
              :disabled="isLoading"
              :class="passwordMismatch ? 'border-destructive' : ''"
            />
            <p v-if="passwordMismatch" class="text-xs text-destructive">
              Passwords do not match
            </p>
          </div>

          <!-- Submit Button -->
          <Button
            type="submit"
            class="w-full"
            :disabled="!formValid || isLoading"
          >
            <Loader2 v-if="isLoading" class="mr-2 h-4 w-4 animate-spin" />
            <LogIn v-else-if="!isRegisterMode" class="mr-2 h-4 w-4" />
            <UserPlus v-else class="mr-2 h-4 w-4" />
            {{ isRegisterMode ? 'Create Account' : 'Sign In' }}
          </Button>
        </form>

        <!-- Toggle Mode -->
        <div class="mt-4 text-center text-sm text-muted-foreground">
          <span>{{ isRegisterMode ? 'Already have an account?' : "Don't have an account?" }}</span>
          <button
            type="button"
            class="ml-1 text-primary hover:underline font-medium"
            @click="toggleMode"
          >
            {{ isRegisterMode ? 'Sign in' : 'Create one' }}
          </button>
        </div>
      </Card>

      <!-- Footer -->
      <p class="text-center text-xs text-muted-foreground">
        Automation made simple
      </p>
    </div>
  </div>
</template>
