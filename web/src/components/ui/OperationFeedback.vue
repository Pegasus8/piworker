<script setup lang="ts">
import { watch, onUnmounted } from 'vue'
import type { Feedback } from '@/composables/useFeedback'
import { CheckCircle2, AlertCircle, AlertTriangle, Loader2, X } from 'lucide-vue-next'
import { messages as m } from '@/i18n'
// floating: shown as a toast in the corner (page-level results), so the
// page does not move; a success fades after a few seconds, an error stays
// until dismissed. Inside dialogs it stays inline: a modal dialog makes the
// rest of the page inert. A warning (done, with something to know) stays
// until dismissed too.
defineOptions({ inheritAttrs: false })
const props = defineProps<{ message: Feedback | null; floating?: boolean }>()
const emit = defineEmits<{ dismiss: [] }>()
let timer: ReturnType<typeof setTimeout> | undefined
watch(() => props.message, message => {
  clearTimeout(timer)
  if (props.floating && message?.kind === 'success') timer = setTimeout(() => emit('dismiss'), 5000)
})
onUnmounted(() => clearTimeout(timer))
</script>
<template>
  <Teleport to="#toast-region" defer :disabled="!floating">
    <Transition enter-active-class="transition duration-200 ease-out" enter-from-class="translate-y-2 opacity-0" leave-active-class="transition duration-150 ease-in" leave-to-class="opacity-0">
      <div v-if="message" v-bind="$attrs" :role="message.kind === 'error' ? 'alert' : 'status'" aria-atomic="true"
        class="flex items-start gap-2 rounded-lg border px-3 py-2 text-sm break-words"
        :class="[message.kind === 'error' ? 'border-destructive/40 bg-destructive/10 text-destructive' : message.kind === 'warning' ? 'border-amber-500/50 bg-amber-500/10' : 'border-primary/30 bg-primary/10', floating ? 'pointer-events-auto w-full bg-card shadow-lg sm:w-96' : '']">
        <AlertCircle v-if="message.kind === 'error'" class="mt-0.5 h-4 w-4 shrink-0" />
        <AlertTriangle v-else-if="message.kind === 'warning'" class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <Loader2 v-else-if="message.kind === 'pending'" class="mt-0.5 h-4 w-4 shrink-0 animate-spin" />
        <CheckCircle2 v-else class="mt-0.5 h-4 w-4 shrink-0 text-primary" />
        <span class="min-w-0 flex-1">{{ message.text }}</span>
        <button v-if="message.kind !== 'pending'" :aria-label="m.common.dismiss" class="shrink-0 rounded p-1 hover:bg-accent" @click="emit('dismiss')"><X class="h-4 w-4" /></button>
      </div>
    </Transition>
  </Teleport>
</template>
