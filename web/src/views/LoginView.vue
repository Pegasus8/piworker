<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute, RouterLink } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Label } from '@/components/ui'
import { Workflow, AlertCircle, Loader2 } from 'lucide-vue-next'
import LanguageSelect from '@/components/LanguageSelect.vue'
import RestoreFromFile from '@/components/RestoreFromFile.vue'
import { messages as m } from '@/i18n'

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
// A fresh installation can restore a backup instead of creating an account
// (ADR-0032); the restore restarts PiWorker, then its accounts apply.
const restoringBackup = ref(false)
const restoreState = ref<'' | 'restarting' | 'slow' | 'done'>('')
async function onRestored(back: boolean) {
  if (!back) { restoreState.value = 'slow'; return }
  restoringBackup.value = false
  restoreState.value = 'done'
  await authStore.fetchAuthStatus()
}
const creating = computed(() => authStore.setupRequired)
const choosingPassword = computed(() => creating.value || recovering.value)
const title = computed(() => restoringBackup.value ? m.value.backups.setupRestoreTitle : creating.value ? m.value.auth.createTitle : recovering.value ? m.value.auth.recoverTitle : m.value.auth.signInTitle)
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
    localError.value = e.response?.data?.error || m.value.auth.signInFailed
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
        <p v-if="restoringBackup" class="text-sm text-muted-foreground mt-2">{{ m.backups.setupRestoreHint }}</p>
        <p v-else-if="creating" class="text-sm text-muted-foreground mt-2">{{ m.auth.createHint }}</p>
      </div>
      <Card v-if="creating && restoringBackup" class="p-6 space-y-4">
        <div v-if="restoreState" role="status" class="flex items-center gap-2 text-sm">
          <Loader2 v-if="restoreState === 'restarting'" class="h-4 w-4 animate-spin" aria-hidden="true" />
          <span>{{ restoreState === 'slow' ? m.backups.restartSlow : m.backups.restarting }}</span>
        </div>
        <!-- Kept mounted while PiWorker restarts: it reports when it is back. -->
        <div v-show="!restoreState" class="space-y-4">
          <p class="text-sm text-muted-foreground">{{ m.backups.setupCodeOnly }}</p>
          <RestoreFromFile mode="setup" @restarting="restoreState = 'restarting'" @restored="onRestored" />
          <button type="button" class="text-sm text-primary hover:underline" @click="restoringBackup = false">{{ m.backups.setupCreateLink }}</button>
        </div>
      </Card>
      <Card v-else class="p-6">
        <p v-if="restoreState === 'done'" role="status" class="mb-4 text-sm">{{ m.backups.setupRestored }}</p>
        <div v-if="creating" class="mb-5 text-sm text-muted-foreground space-y-2">
          <p>{{ m.auth.setupCodeHint }}</p>
          <p>{{ m.auth.setupCodeExpiry }}</p>
        </div>
        <div v-if="recovering" class="mb-5 text-sm text-muted-foreground space-y-2">
          <p>{{ m.auth.recoverRun }}</p>
          <code class="block rounded bg-muted p-3 break-words">piworker -db /path/to/piworker.db -recover-account USERNAME</code>
          <p>{{ m.auth.recoverHint }}</p>
        </div>
        <p v-if="route.query.changed && !choosingPassword" role="status" class="mb-4 text-sm">{{ m.auth.passwordChanged }}</p>
        <form @submit.prevent="handleSubmit" class="space-y-4">
          <div v-if="localError || authStore.error" role="alert" class="flex items-center gap-2 p-3 rounded-lg bg-destructive/10 text-destructive text-sm">
            <AlertCircle class="h-4 w-4 flex-shrink-0" /><span>{{ localError || authStore.error }}</span>
          </div>
          <div v-if="choosingPassword" class="space-y-2">
            <Label for="code">{{ creating ? m.auth.setupCode : m.auth.recoveryCode }}</Label>
            <Input id="code" v-model="code" autocomplete="off" :disabled="authStore.loading" />
          </div>
          <div class="space-y-2">
            <Label for="username">{{ m.auth.username }}</Label>
            <Input id="username" v-model="username" autocomplete="username" :disabled="authStore.loading" />
          </div>
          <div class="space-y-2">
            <Label for="password">{{ m.auth.password }}</Label>
            <Input id="password" v-model="password" type="password" :autocomplete="choosingPassword ? 'new-password' : 'current-password'" :disabled="authStore.loading" />
            <p v-if="choosingPassword" class="text-xs text-muted-foreground">{{ m.auth.passwordRule }}</p>
          </div>
          <div v-if="choosingPassword" class="space-y-2">
            <Label for="confirmPassword">{{ m.auth.confirmPassword }}</Label>
            <Input id="confirmPassword" v-model="confirmPassword" type="password" autocomplete="new-password" :disabled="authStore.loading" />
            <p v-if="confirmPassword && password !== confirmPassword" class="text-xs text-destructive">{{ m.auth.mismatch }}</p>
          </div>
          <Button type="submit" class="w-full" :disabled="!formValid || authStore.loading">
            <Loader2 v-if="authStore.loading" class="mr-2 h-4 w-4 animate-spin" />
            {{ creating ? m.auth.createAccount : recovering ? m.auth.resetPassword : m.auth.signIn }}
          </Button>
        </form>
        <button v-if="creating" type="button" class="mt-4 text-sm text-primary hover:underline" :disabled="authStore.loading" @click="restoringBackup = true; restoreState = ''">
          {{ m.backups.setupRestoreLink }}
        </button>
        <button v-if="!creating" type="button" class="mt-4 text-sm text-primary hover:underline" :disabled="authStore.loading" @click="toggleRecovery">
          {{ recovering ? m.auth.backToSignIn : m.auth.forgot }}
        </button>
      </Card>
      <p class="text-center text-xs text-muted-foreground">
        {{ m.auth.certWarning }} <RouterLink to="/trust" class="text-primary hover:underline">{{ m.auth.trustDevice }}</RouterLink>
      </p>
      <p class="text-center text-xs text-muted-foreground">{{ m.auth.tagline }}</p>
      <LanguageSelect class="mx-auto w-full max-w-[12rem]" />
    </div>
  </div>
</template>
