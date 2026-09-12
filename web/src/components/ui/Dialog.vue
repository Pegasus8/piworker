<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{ open?: boolean; class?: string; title?: string }>(), { open: false, title: 'Dialog' })
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
const dialog = ref<HTMLDialogElement>()
function close() { emit('update:open', false) }
function onBackdrop(event: MouseEvent) {
  if (event.target !== dialog.value) return
  const bounds = dialog.value.getBoundingClientRect()
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close()
}
watch(() => props.open, async open => {
  await nextTick()
  if (open && !dialog.value?.open) dialog.value?.showModal()
  else if (!open) dialog.value?.close()
}, { immediate: true })
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" :aria-label="title"
      :class="cn('lab-dialog m-auto w-[calc(100%-2rem)] max-w-lg max-h-[calc(100dvh-2rem)] overflow-y-auto rounded-xl border bg-card p-6 shadow-2xl', props.class)"
      @cancel.prevent="close" @click="onBackdrop">
      <slot :close="close" />
    </dialog>
  </Teleport>
</template>
