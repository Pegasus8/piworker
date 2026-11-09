<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Label } from '@/components/ui'
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
  else error.value = e.response?.data?.error || 'Could not change password. Please try again.'
 } finally { busy.value = false }
}
</script>
<template>
 <main class="lab-page min-h-full bg-background p-6">
  <div class="mx-auto max-w-md space-y-6">
   <Button variant="ghost" @click="router.push('/')">Back to flows</Button>
   <div><h1 class="text-2xl font-bold">Account</h1><p class="text-muted-foreground">{{ auth.user?.username }}</p></div>
   <Card class="p-6">
    <h2 class="text-lg font-semibold mb-2">Change password</h2>
    <p class="text-sm text-muted-foreground mb-5">All devices will be signed out, including this one.</p>
    <form class="space-y-4" @submit.prevent="save">
     <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
     <div class="space-y-2"><Label for="current">Current password</Label><Input id="current" v-model="current" type="password" autocomplete="current-password" :disabled="busy" /></div>
     <div class="space-y-2"><Label for="new">New password</Label><Input id="new" v-model="password" type="password" autocomplete="new-password" :disabled="busy" /></div>
     <p class="text-xs text-muted-foreground">Use at least 8 characters (maximum 72 bytes).</p>
     <div class="space-y-2"><Label for="confirm">Confirm password</Label><Input id="confirm" v-model="confirm" type="password" autocomplete="new-password" :disabled="busy" /></div>
     <div class="space-y-1">
      <label class="flex items-center gap-2 text-sm font-medium"><input id="keep-agent-tokens" v-model="keepAgentTokens" type="checkbox" class="h-4 w-4 accent-primary" :disabled="busy" />Keep agent tokens</label>
      <p class="text-xs text-muted-foreground">Otherwise every agent token is revoked; leave this off if your password or a token may have leaked.</p>
     </div>
     <Button type="submit" class="w-full" :disabled="busy || !valid">{{ busy ? 'Saving…' : 'Change password' }}</Button>
    </form>
   </Card>
   <Card class="p-6">
    <h2 class="text-lg font-semibold mb-2">HTTPS and this device</h2>
    <p class="text-sm text-muted-foreground mb-4">Download PiWorker's certificate authority, check its fingerprint and trust it on your computers and phones.</p>
    <Button variant="outline" @click="router.push('/trust')">Trust this device</Button>
   </Card>
  </div>
 </main>
</template>
