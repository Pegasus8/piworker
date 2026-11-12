<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Bot, Info } from 'lucide-vue-next'
import { Badge } from '@/components/ui'
import { useAgentStore } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { mcpStatus } from '@/lib/agentAccess'

const route = useRoute()
const agent = useAgentStore()
const auth = useAuthStore()
// The badges come from the passive pending count the app polls in the
// background (ApprovalsNotice): approvals waiting, drafts to review.
const tabs = computed(() => [
  { to: '/agents', label: 'Tokens', count: 0, countLabel: '' },
  { to: '/agents/approvals', label: 'Approvals', count: agent.pendingCount, countLabel: `${agent.pendingCount} waiting` },
  { to: '/agents/drafts', label: 'Drafts', count: agent.pendingDrafts, countLabel: `${agent.pendingDrafts} to review` },
  { to: '/agents/audit', label: 'Audit', count: 0, countLabel: '' }
])
// A draft's review page belongs to the Drafts tab.
const isActive = (to: string) => route.path === to || (to === '/agents/drafts' && route.path.startsWith('/agents/drafts/'))

// Whether agents can connect at all (mcp.enabled, and lan_access with TLS
// off): read once per visit, shared with the page's own reads.
const mcp = computed(() => mcpStatus(agent.settings, agent.tls))
const mcpVariant = computed(() => mcp.value.state === 'on' ? 'secondary' : mcp.value.state === 'off' ? 'destructive' : 'outline')
onMounted(() => {
  if (!auth.authEnabled) return
  if (!agent.settings) agent.fetchSettings().catch(() => { /* shown as unknown */ })
  if (!agent.tls) agent.fetchTLS().catch(() => { /* the TLS mode stays unknown */ })
})
</script>

<template>
  <header class="space-y-4">
    <div class="space-y-2">
      <h1 class="flex items-center gap-2 text-2xl font-bold"><Bot class="h-6 w-6 text-primary" aria-hidden="true" />Agent access</h1>
      <p class="text-muted-foreground">
        Let AI agents such as Claude Code call the flows you publish as tools. Each agent gets its own token with only
        the access you grant; you approve sensitive requests here and every call is recorded.
      </p>
    </div>
    <section v-if="auth.authEnabled" aria-label="MCP endpoint" class="flex flex-wrap items-start gap-2 rounded-md border p-3 text-sm"
      :class="mcp.state === 'off' && 'border-destructive/40 bg-destructive/5'">
      <Badge :variant="mcpVariant">{{ mcp.label }}</Badge>
      <p class="min-w-0 flex-1 text-muted-foreground">{{ mcp.detail }}</p>
    </section>
    <nav v-if="auth.authEnabled" aria-label="Agent access sections" class="flex flex-wrap gap-2">
      <RouterLink
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        class="lab-button inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium"
        :class="isActive(tab.to) ? 'border-primary bg-primary text-primary-foreground' : 'border-input hover:bg-accent'"
        :aria-current="isActive(tab.to) ? 'page' : undefined"
      >
        {{ tab.label }}
        <span
          v-if="tab.count"
          class="rounded-full bg-destructive px-1.5 text-xs font-semibold text-destructive-foreground"
          :aria-label="tab.countLabel"
        >{{ tab.count }}</span>
      </RouterLink>
    </nav>
    <!-- Not an error: a valid set-up without accounts, where agents have no place. -->
    <p v-if="!auth.authEnabled" role="note" class="flex gap-2 rounded-lg border bg-muted/40 p-3 text-sm">
      <Info class="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
      <span>Agent access needs sign-in. This PiWorker runs with authentication turned off, so agent tokens, approvals and the
      audit log are not available. Start PiWorker without <code>-auth=false</code> to use them.</span>
    </p>
  </header>
</template>
