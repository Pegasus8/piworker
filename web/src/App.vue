<script setup lang="ts">
import { RouterView } from 'vue-router'
import HttpWarning from '@/components/HttpWarning.vue'
</script>

<template>
  <div class="flex h-dvh flex-col bg-background">
    <HttpWarning />
    <div class="min-h-0 flex-1 overflow-auto">
      <RouterView />
    </div>
  </div>
</template>
