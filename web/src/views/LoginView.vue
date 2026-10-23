<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Label } from '@/components/ui'
import { Workflow, AlertCircle, Loader2 } from 'lucide-vue-next'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const api = useApi()
const recovering = ref(false)
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const code = ref('')
const localError = ref<string | null>(null)
const creating = computed(() => authStore.setupRequired)
const choosingPassword = computed(() => creating.value || recovering.value)
const title = computed(() => creating.value ? 'Create your administrator account' : recovering.value ? 'Recover your account' : 'Sign in to continue')
const formValid = computed(() => {
  if (!username.value.trim() || !password.value) return false
  if (choosingPassword.value) {
    const bytes = new TextEncoder().encode(password.value).length
    return !!code.value.trim() && bytes >= 8 && bytes <= 72 && password.value === confirmPassword.value
  }
  return true
})
async function handleSubmit() {
  if (!formValid.value) return
  localError.value = null
  authStore.setLoading(true)
  authStore.clearError()
  try {
    if (creating.value) {
      await api.setup(username.value.trim(), password.value, code.value.trim())
      authStore.setupRequired = false
    } else if (recovering.value) {
      await api.recover(username.value.trim(), password.value, code.value.trim())
      recovering.value = false
    }
    await authStore.signIn(username.value.trim(), password.value)
    password.value = ''; confirmPassword.value = ''; code.value = ''
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect.startsWith('/') && !redirect.startsWith('//') && !redirect.startsWith('/login') ? redirect : '/')
  } catch (e: any) {
    localError.value = e.response?.data?.error || 'Could not sign in. Please try again.'
  } finally { authStore.setLoading(false) }
}
function toggleRecovery() {
  recovering.value = !recovering.value
  localError.value = null
  password.value = ''; confirmPassword.value = ''; code.value = ''
}
</script>

<template>
  <div class="lab-page min-h-full bg-background flex items-center justify-center p-4">
    <div class="w-full max-w-md space-y-6">
      <div class="text-center">
        <div class="inline-flex items-center justify-center w-16 h-16 rounded-full bg-primary/10 mb-4">
          <Workflow class="h-8 w-8 text-primary" />
        </div>
        <p class="text-sm font-medium text-muted-foreground mb-2">PiWorker</p>
        <h1 class="text-2xl font-bold">{{ title }}</h1>
        <p v-if="creating" class="text-sm text-muted-foreground mt-2">One account to manage this installation.</p>
      </div>
      <Card class="p-6">
        <div v-if="creating" class="mb-5 text-sm text-muted-foreground space-y-2">
          <p>Enter the setup code shown in the terminal when PiWorker starts, then choose your credentials.</p>
          <p>The code expires after 30 minutes. Restart PiWorker to generate a new one, or use the local setup-code command.</p>
        </div>
        <div v-if="recovering" class="mb-5 text-sm text-muted-foreground space-y-2">
          <p>Run this on the device hosting PiWorker</p>
          <code class="block rounded bg-muted p-3 break-words">piworker -db /path/to/piworker.db -recover-account USERNAME</code>
          <p>Use the same database as your server and replace USERNAME with your account name. Enter the one-use code below within 30 minutes.</p>
        </div>
        <p v-if="route.query.changed && !choosingPassword" role="status" class="mb-4 text-sm">Password changed. Sign in again on each device.</p>
        <form @submit.prevent="handleSubmit" class="space-y-4">
          <div v-if="localError || authStore.error" role="alert" class="flex items-center gap-2 p-3 rounded-lg bg-destructive/10 text-destructive text-sm">
            <AlertCircle class="h-4 w-4 flex-shrink-0" /><span>{{ localError || authStore.error }}</span>
          </div>
          <div v-if="choosingPassword" class="space-y-2">
            <Label for="code">{{ creating ? 'Setup code' : 'Recovery code' }}</Label>
            <Input id="code" v-model="code" autocomplete="off" :disabled="authStore.loading" />
          </div>
          <div class="space-y-2">
            <Label for="username">Username</Label>
            <Input id="username" v-model="username" autocomplete="username" :disabled="authStore.loading" />
          </div>
          <div class="space-y-2">
            <Label for="password">Password</Label>
            <Input id="password" v-model="password" type="password" :autocomplete="choosingPassword ? 'new-password' : 'current-password'" :disabled="authStore.loading" />
            <p v-if="choosingPassword" class="text-xs text-muted-foreground">Use at least 8 characters (maximum 72 bytes).</p>
          </div>
          <div v-if="choosingPassword" class="space-y-2">
            <Label for="confirmPassword">Confirm password</Label>
            <Input id="confirmPassword" v-model="confirmPassword" type="password" autocomplete="new-password" :disabled="authStore.loading" />
            <p v-if="confirmPassword && password !== confirmPassword" class="text-xs text-destructive">Passwords do not match.</p>
          </div>
          <Button type="submit" class="w-full" :disabled="!formValid || authStore.loading">
            <Loader2 v-if="authStore.loading" class="mr-2 h-4 w-4 animate-spin" />
            {{ creating ? 'Create account' : recovering ? 'Reset password' : 'Sign in' }}
          </Button>
        </form>
        <button v-if="!creating" type="button" class="mt-4 text-sm text-primary hover:underline" :disabled="authStore.loading" @click="toggleRecovery">
          {{ recovering ? 'Back to sign in' : 'Forgot password?' }}
        </button>
      </Card>
      <p class="text-center text-xs text-muted-foreground">Automation made simple</p>
    </div>
  </div>
</template>
