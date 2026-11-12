<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Label } from '@/components/ui'
import { messages as m } from '@/i18n'
const router = useRouter()
const auth = useAuthStore()
const api = useApi()
const current = ref('')
const password = ref('')
const confirm = ref('')
const keepAgentTokens = ref(false)
const busy = ref(false)
const error = ref('')
const valid = computed(() => {
 const length = new TextEncoder().encode(password.value).length
 return !!current.value && length >= 8 && length <= 72 && password.value === confirm.value
})
async function save() {
 if (!valid.value) return
 busy.value = true; error.value = ''
 try {
  await api.changePassword(current.value, password.value, keepAgentTokens.value)
  auth.clearAuth()
  current.value = ''; password.value = ''; confirm.value = ''; keepAgentTokens.value = false
  await router.replace('/login?changed=1')
 } catch (e: any) {
  if (e.response?.status === 401) { auth.clearAuth(); await router.replace('/login') }
  else error.value = e.response?.data?.error || m.value.auth.account.changeFailed
 } finally { busy.value = false }
}
</script>
<template>
 <main class="lab-page min-h-full bg-background p-6">
  <div class="mx-auto max-w-md space-y-6">
   <div><h1 class="text-2xl font-bold">{{ m.auth.account.title }}</h1><p class="text-muted-foreground">{{ auth.user?.username }}</p></div>
   <Card class="p-6">
    <h2 class="text-lg font-semibold mb-2">{{ m.auth.account.changePassword }}</h2>
    <p class="text-sm text-muted-foreground mb-5">{{ m.auth.account.signOutAll }}</p>
    <form class="space-y-4" @submit.prevent="save">
     <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
     <div class="space-y-2"><Label for="current">{{ m.auth.account.current }}</Label><Input id="current" v-model="current" type="password" autocomplete="current-password" :disabled="busy" /></div>
     <div class="space-y-2"><Label for="new">{{ m.auth.account.newPassword }}</Label><Input id="new" v-model="password" type="password" autocomplete="new-password" :disabled="busy" /></div>
     <p class="text-xs text-muted-foreground">{{ m.auth.passwordRule }}</p>
     <div class="space-y-2"><Label for="confirm">{{ m.auth.confirmPassword }}</Label><Input id="confirm" v-model="confirm" type="password" autocomplete="new-password" :disabled="busy" /></div>
     <div class="space-y-1">
      <label class="flex items-center gap-2 text-sm font-medium"><input id="keep-agent-tokens" v-model="keepAgentTokens" type="checkbox" class="h-4 w-4 accent-primary" :disabled="busy" />{{ m.auth.account.keepTokens }}</label>
      <p class="text-xs text-muted-foreground">{{ m.auth.account.keepTokensHint }}</p>
     </div>
     <Button type="submit" class="w-full" :disabled="busy || !valid">{{ busy ? m.auth.account.saving : m.auth.account.changePassword }}</Button>
    </form>
   </Card>
   <Card class="p-6">
    <h2 class="text-lg font-semibold mb-2">{{ m.auth.account.httpsTitle }}</h2>
    <p class="text-sm text-muted-foreground mb-4">{{ m.auth.account.httpsText }}</p>
    <Button variant="outline" @click="router.push('/trust')">{{ m.auth.trustDevice }}</Button>
   </Card>
  </div>
 </main>
</template>
