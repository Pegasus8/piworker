<script setup lang="ts">
import { ref, watch } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  open?: boolean
  class?: string
}

const props = withDefaults(defineProps<Props>(), {
  open: false
})

const emit = defineEmits<{
  'update:open': [value: boolean]
}>()

const isOpen = ref(props.open)

watch(() => props.open, (newVal) => {
  isOpen.value = newVal
})

function close() {
  isOpen.value = false
  emit('update:open', false)
}

function handleBackdropClick(event: MouseEvent) {
  if (event.target === event.currentTarget) {
    close()
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="isOpen"
        class="fixed inset-0 z-50 flex items-center justify-center"
      >
        <!-- Backdrop -->
        <div
          class="fixed inset-0 bg-black/50 backdrop-blur-sm"
          @click="handleBackdropClick"
        />

        <!-- Dialog -->
        <div
          :class="cn(
            'relative z-50 w-full max-w-lg rounded-lg border bg-background p-6 shadow-lg',
            props.class
          )"
        >
          <slot :close="close" />
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
