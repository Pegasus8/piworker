<script setup lang="ts">
import { computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import HttpWarning from '@/components/HttpWarning.vue'
import ApprovalsNotice from '@/components/agent/ApprovalsNotice.vue'
import AppSidebar from '@/components/AppSidebar.vue'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const auth = useAuthStore()
// The editor uses the whole screen and has its own way back; sign-in and
// public pages seen while signed out have no navigation.
const withNavigation = computed(() => route.name !== undefined && route.name !== 'login' && route.name !== 'editor' && auth.isAuthenticated)
</script>

<template>
  <div class="flex h-dvh flex-col bg-background">
    <HttpWarning />
    <ApprovalsNotice />
    <div class="flex min-h-0 flex-1 flex-col md:flex-row">
      <AppSidebar v-if="withNavigation" />
      <div class="min-h-0 min-w-0 flex-1 overflow-auto">
        <RouterView />
      </div>
    </div>
    <!-- Page-level results appear here as toasts (OperationFeedback floating). -->
    <div id="toast-region" class="pointer-events-none fixed inset-x-4 bottom-4 z-[60] flex flex-col items-end gap-2 sm:inset-x-auto sm:right-4" />
  </div>
</template>
