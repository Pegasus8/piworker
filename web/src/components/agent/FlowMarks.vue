<script setup lang="ts">
// "Deployed by agent <token>" and "Changed by agent" on a flow (H6), each
// with an icon and its text; the title explains it.
import { computed } from 'vue'
import { Bot, FileEdit } from 'lucide-vue-next'
import { Badge } from '@/components/ui'
import { flowMarkers, type MarkedFlow } from '@/lib/flowMarks'

const props = defineProps<{ flow: MarkedFlow }>()
const markers = computed(() => flowMarkers(props.flow))
</script>

<template>
  <template v-for="m in markers" :key="m.kind">
    <Badge variant="outline" class="max-w-full gap-1 border-amber-500/60 text-amber-800 dark:text-amber-300" :title="m.title" :data-marker="m.kind">
      <Bot v-if="m.kind === 'deployed_by_agent'" class="h-3 w-3 shrink-0" aria-hidden="true" />
      <FileEdit v-else class="h-3 w-3 shrink-0" aria-hidden="true" />
      <span class="truncate">{{ m.label }}</span>
      <span class="sr-only">. {{ m.title }}</span>
    </Badge>
  </template>
</template>
