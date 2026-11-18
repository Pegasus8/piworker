<script setup lang="ts">
// What is known of a suggested setting's value, under its field
// (lib/suggestions.ts suggestionHint): information in infoClass, or a
// warning. Nothing when there is nothing to say. id: what the field's
// aria-describedby names.
import type { SuggestionHint } from '@/lib/suggestions'

defineProps<{ id: string; hint: SuggestionHint; infoClass: string }>()
</script>

<template>
  <p v-if="hint.text" :id="id" class="mt-1 break-words text-xs" :class="hint.warning ? 'text-amber-600 dark:text-amber-400' : infoClass">{{ hint.text }}</p>
</template>
