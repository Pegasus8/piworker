<script setup lang="ts">
// Keeps the pending approvals count fresh while someone is signed in (polling
// pauses in hidden tabs) and points to the queue from every other page, since
// a request waits only 15 minutes.
import { computed, onBeforeUnmount, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { BellRing } from 'lucide-vue-next'
import { useAgentStore } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { messages as m } from '@/i18n'

const agent = useAgentStore()
const auth = useAuthStore()
const route = useRoute()
let release: (() => void) | null = null

watch(() => auth.authEnabled && auth.user !== null, signedIn => {
  if (signedIn && !release) release = agent.watchApprovals()
  else if (!signedIn && release) {
    release()
    release = null
    agent.pendingCount = 0
    agent.pendingDrafts = 0
  }
}, { immediate: true })
onBeforeUnmount(() => release?.())

const visible = computed(() => agent.pendingCount > 0 && auth.user !== null && route.path !== '/agents/approvals')
const text = computed(() => m.value.agents.notice.waiting(agent.pendingCount))
</script>

<template>
  <aside
    v-if="visible"
    role="status"
    class="flex shrink-0 items-center justify-center gap-3 border-b border-primary/30 bg-primary/10 px-4 py-2 text-sm"
  >
    <BellRing class="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
    <p>
      {{ text }}
      <RouterLink to="/agents/approvals" class="font-semibold underline underline-offset-2">{{ m.agents.notice.review }}</RouterLink>
    </p>
  </aside>
</template>
