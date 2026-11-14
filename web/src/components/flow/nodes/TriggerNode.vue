<script setup lang="ts">
import BaseNode from './BaseNode.vue'
import type { NodeCategory } from '@/types'

interface Props {
  id: string
  data: {
    label: string
    nodeType: string
    category: NodeCategory
    icon: string
    config: Record<string, any>
  }
  selected?: boolean
}

defineProps<Props>()
</script>

<template>
  <BaseNode :id="id" :data="data" :selected="selected" />
</template>
