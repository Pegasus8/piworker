<script setup lang="ts">
import { computed, ref } from 'vue'
const props = withDefaults(defineProps<{ value: unknown; label?: string; depth?: number }>(), { label: 'payload', depth: 0 })
const expanded = ref(props.depth === 0)
const complex = computed(() => props.value !== null && typeof props.value === 'object')
const entries = computed(() => complex.value ? Object.entries(props.value as Record<string, unknown>) : [])
const summary = computed(() => Array.isArray(props.value) ? `Array · ${entries.value.length} items` : `Object · ${entries.value.length} fields`)
</script>
<template>
  <details v-if="complex && depth < 12" :open="expanded" @toggle="expanded = ($event.target as HTMLDetailsElement).open" class="min-w-0 rounded py-1 text-sm">
    <summary class="cursor-pointer break-all font-mono"><span class="text-primary">{{ label }}</span> <span class="text-xs text-muted-foreground">{{ summary }}</span></summary>
    <div v-if="expanded" class="ml-3 border-l pl-3">
      <PayloadTree v-for="[key, entry] in entries.slice(0, 100)" :key="key" :value="entry" :label="key" :depth="depth + 1" />
      <p v-if="entries.length > 100" class="text-xs text-muted-foreground">Showing the first 100 entries. Full data is available under Raw message.</p>
    </div>
  </details>
  <div v-else class="break-all py-1 font-mono text-sm"><span class="text-primary">{{ label }}:</span> {{ complex ? '[Nested data — see Raw message]' : JSON.stringify(value) }}</div>
</template>
