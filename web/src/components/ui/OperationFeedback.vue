<script setup lang="ts">
import type { Feedback } from '@/composables/useFeedback'
import { CheckCircle2, AlertCircle, Loader2, X } from 'lucide-vue-next'
defineProps<{ message: Feedback | null }>()
defineEmits<{ dismiss: [] }>()
</script>
<template>
  <div v-if="message" :role="message.kind === 'error' ? 'alert' : 'status'" aria-atomic="true"
    class="flex items-start gap-2 rounded-lg border px-3 py-2 text-sm break-words"
    :class="message.kind === 'error' ? 'border-destructive/40 bg-destructive/10 text-destructive' : 'border-primary/30 bg-primary/10'">
    <AlertCircle v-if="message.kind === 'error'" class="mt-0.5 h-4 w-4 shrink-0" />
    <Loader2 v-else-if="message.kind === 'pending'" class="mt-0.5 h-4 w-4 shrink-0 animate-spin" />
    <CheckCircle2 v-else class="mt-0.5 h-4 w-4 shrink-0 text-primary" />
    <span class="min-w-0 flex-1">{{ message.text }}</span>
    <button v-if="message.kind !== 'pending'" aria-label="Dismiss message" class="shrink-0 rounded p-1 hover:bg-accent" @click="$emit('dismiss')"><X class="h-4 w-4" /></button>
  </div>
</template>
