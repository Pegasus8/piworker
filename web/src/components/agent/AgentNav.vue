<script setup lang="ts">
import { RouterLink, useRouter } from 'vue-router'
import { ArrowLeft, Bot } from 'lucide-vue-next'
import { Button } from '@/components/ui'
import { useAgentStore } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const agent = useAgentStore()
const auth = useAuthStore()
const tabs = [
  { to: '/agents', label: 'Tokens' },
  { to: '/agents/approvals', label: 'Approvals' },
  { to: '/agents/audit', label: 'Audit' }
]
</script>

<template>
  <header class="space-y-4">
    <Button variant="ghost" @click="router.push('/')"><ArrowLeft class="mr-2 h-4 w-4" />Back to flows</Button>
    <div class="space-y-2">
      <h1 class="flex items-center gap-2 text-2xl font-bold"><Bot class="h-6 w-6 text-primary" aria-hidden="true" />Agent access</h1>
      <p class="text-muted-foreground">
        Let AI agents such as Claude Code call the flows you publish as tools. Each agent gets its own token with only
        the access you grant; you approve sensitive requests here and every call is recorded.
      </p>
    </div>
    <nav aria-label="Agent access sections" class="flex flex-wrap gap-2">
      <RouterLink
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        class="lab-button inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium"
        exact-active-class="border-primary bg-primary text-primary-foreground"
        :class="$route.path === tab.to ? '' : 'border-input hover:bg-accent'"
      >
        {{ tab.label }}
        <span
          v-if="tab.to === '/agents/approvals' && agent.pendingCount"
          class="rounded-full bg-destructive px-1.5 text-xs font-semibold text-destructive-foreground"
          :aria-label="`${agent.pendingCount} waiting`"
        >{{ agent.pendingCount }}</span>
      </RouterLink>
    </nav>
    <p v-if="!auth.authEnabled" role="alert" class="rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
      Agent access needs sign-in. This PiWorker runs with authentication turned off, so agent tokens, approvals and the
      audit log are not available.
    </p>
  </header>
</template>
