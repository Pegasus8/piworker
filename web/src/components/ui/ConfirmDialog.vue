<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import Dialog from './Dialog.vue'
import Button from './Button.vue'
const open = ref(false)
const options = ref({ title: '', description: '', action: '', cancel: 'Cancel' })
let resolve: ((answer: boolean) => void) | undefined
function finish(answer = false) {
  open.value = false
  resolve?.(answer)
  resolve = undefined
}
function ask(title: string, description: string, action: string, cancel = 'Cancel'): Promise<boolean> {
  finish(false)
  options.value = { title, description, action, cancel }
  open.value = true
  return new Promise(done => { resolve = done })
}
onBeforeUnmount(() => finish(false))
defineExpose({ ask })
</script>
<template>
  <Dialog :open="open" :title="options.title" @update:open="finish(false)">
    <h2 class="text-lg font-semibold">{{ options.title }}</h2>
    <p class="mt-2 break-words text-sm text-muted-foreground">{{ options.description }}</p>
    <div class="mt-6 flex flex-wrap justify-end gap-2">
      <Button variant="outline" autofocus @click="finish(false)">{{ options.cancel }}</Button>
      <Button variant="destructive" @click="finish(true)">{{ options.action }}</Button>
    </div>
  </Dialog>
</template>
