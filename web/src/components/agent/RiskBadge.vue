<script setup lang="ts">
// A risk level told by icon and text, never by color alone.
import { computed } from 'vue'
import { Badge } from '@/components/ui'
import { draftIcon, riskInfo } from '@/lib/drafts'

const props = defineProps<{ risk: string; prefix?: string }>()
const info = computed(() => riskInfo(props.risk))
</script>

<template>
  <Badge :variant="info.variant" class="gap-1">
    <component :is="draftIcon(info.icon)" class="h-3 w-3" aria-hidden="true" />
    {{ prefix ? `${prefix}: ${info.label}` : info.label }}
  </Badge>
</template>
