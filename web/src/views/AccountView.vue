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
  await api.changePassword(current.value, password.value)
  auth.clearAuth()
  current.value = ''; password.value = ''; confirm.value = ''
  await router.replace('/login?changed=1')
 } catch (e: any) {
  if (e.response?.status === 401) { auth.clearAuth(); await router.replace('/login') }
  else error.value = e.response?.data?.error || 'Could not change password. Please try again.'
 } finally { busy.value = false }
}
</script>
<template>
 <main class="min-h-screen bg-background p-6">
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
     <Button type="submit" class="w-full" :disabled="busy || !valid">{{ busy ? 'Saving…' : 'Change password' }}</Button>
    </form>
   </Card>
  </div>
 </main>
</template>
