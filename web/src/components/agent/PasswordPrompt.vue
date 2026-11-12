<script setup lang="ts">
// Asks for the administrator's password before a change that widens what
// agents may do (password step-up). Resolves with the password, or null when
// cancelled; the password is cleared as soon as the dialog closes.
import { ref, onBeforeUnmount } from 'vue'
import { Button, Dialog, Input, Label } from '@/components/ui'
import { messages as m } from '@/i18n'

const open = ref(false)
const password = ref('')
const options = ref({ title: '', description: '', action: '' })
let resolve: ((value: string | null) => void) | undefined

function finish(value: string | null) {
  open.value = false
  password.value = ''
  resolve?.(value)
  resolve = undefined
}
function submit() {
  if (password.value) finish(password.value)
}
function ask(title: string, description: string, action: string): Promise<string | null> {
  finish(null)
  options.value = { title, description, action }
  open.value = true
  return new Promise(done => { resolve = done })
}
onBeforeUnmount(() => finish(null))
defineExpose({ ask })
</script>

<template>
  <Dialog :open="open" :title="options.title" @update:open="finish(null)">
    <form @submit.prevent="submit">
      <h2 class="text-lg font-semibold">{{ options.title }}</h2>
      <p class="mt-2 break-words text-sm text-muted-foreground">{{ options.description }}</p>
      <div class="mt-4 space-y-2">
        <Label for="step-up-password">{{ m.agents.yourPassword }}</Label>
        <Input id="step-up-password" v-model="password" type="password" autocomplete="current-password" />
      </div>
      <div class="mt-6 flex flex-wrap justify-end gap-2">
        <Button variant="outline" @click="finish(null)">{{ m.common.cancel }}</Button>
        <Button type="submit" :disabled="!password">{{ options.action }}</Button>
      </div>
    </form>
  </Dialog>
</template>
