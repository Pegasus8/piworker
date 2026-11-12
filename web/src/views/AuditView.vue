<script setup lang="ts">
// The MCP audit log (docs/design/mcp.md §9): agent calls, most recent first,
// paged backwards by id, and the per-minute counters of rejected calls.
// Argument previews come from agents and are shown as plain text.
import { computed, onMounted, ref } from 'vue'
import { Badge, Button, Card, Input, Label } from '@/components/ui'
import AgentNav from '@/components/agent/AgentNav.vue'
import AgentText from '@/components/agent/AgentText.vue'
import { useAgentStore, apiErrorMessage } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime, type AuditFilters } from '@/lib/agentAccess'
import { messages as m } from '@/i18n'

const agent = useAgentStore()
const auth = useAuthStore()
const loading = ref(false)
const error = ref('')
const form = ref<Required<AuditFilters>>({ tokenId: '', tool: '', flowId: '', outcome: '', since: '', until: '' })
// Outcomes the call service records; other values can be typed.
const outcomes = ['completed', 'completed_no_reply', 'error', 'running', 'expired', 'approval_required', 'rejected', 'busy', 'cooldown', 'invalid_args', 'forbidden']

const tokenName = (id: string) => agent.tokens.find(t => t.id === id)?.name ?? id
const flowName = (id?: string) => id ? (agent.flowNames[id] ?? id) : ''
const flows = computed(() => Object.entries(agent.flowNames).map(([id, name]) => ({ id, name })))
const outcomeVariant = (o: string) => o === 'completed' || o === 'completed_no_reply' ? 'secondary' : o === 'error' || o === 'rejected' || o === 'forbidden' ? 'destructive' : 'outline'

// Totals per token and reason over the window, summed by the server.
const rejectionTotals = computed(() => agent.rejections)
const rejectionToken = (r: { tokenId: string; tokenName: string }) => r.tokenName || tokenName(r.tokenId)

async function run(action: () => Promise<void>) {
  loading.value = true
  error.value = ''
  try {
    await action()
  } catch (e) {
    error.value = apiErrorMessage(e, m.value.agents.audit.loadFailed)
  } finally {
    loading.value = false
  }
}
const apply = () => run(() => agent.fetchAudit({ ...form.value }))
const loadMore = () => run(() => agent.loadMoreAudit())
function clear() {
  form.value = { tokenId: '', tool: '', flowId: '', outcome: '', since: '', until: '' }
  void apply()
}

onMounted(async () => {
  if (!auth.authEnabled) return
  await Promise.allSettled([agent.fetchTokens(), agent.fetchFlowNames()])
  await apply()
})

const inputClass = 'flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50'
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-6xl space-y-6">
      <AgentNav />

      <template v-if="auth.authEnabled">
        <Card class="space-y-4 p-4 sm:p-6">
          <div>
            <h2 class="text-lg font-semibold">{{ m.agents.audit.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.agents.audit.intro }}</p>
          </div>
          <form class="grid gap-3 sm:grid-cols-3 lg:grid-cols-6" @submit.prevent="apply">
            <div class="space-y-1">
              <Label for="audit-token" class="text-xs">{{ m.agents.audit.token }}</Label>
              <select id="audit-token" v-model="form.tokenId" :class="inputClass" :disabled="loading">
                <option value="">{{ m.agents.audit.allTokens }}</option>
                <option v-for="t in agent.tokens" :key="t.id" :value="t.id">{{ t.name }}</option>
              </select>
            </div>
            <div class="space-y-1">
              <Label for="audit-tool" class="text-xs">{{ m.agents.audit.tool }}</Label>
              <Input id="audit-tool" v-model="form.tool" class="h-9" placeholder="garage_open" :disabled="loading" />
            </div>
            <div class="space-y-1">
              <Label for="audit-flow" class="text-xs">{{ m.agents.audit.flow }}</Label>
              <select id="audit-flow" v-model="form.flowId" :class="inputClass" :disabled="loading">
                <option value="">{{ m.agents.audit.allFlows }}</option>
                <option v-for="f in flows" :key="f.id" :value="f.id">{{ f.name }}</option>
              </select>
            </div>
            <div class="space-y-1">
              <Label for="audit-outcome" class="text-xs">{{ m.agents.audit.outcome }}</Label>
              <Input id="audit-outcome" v-model="form.outcome" class="h-9" list="audit-outcomes" :placeholder="m.agents.audit.any" :disabled="loading" />
              <datalist id="audit-outcomes"><option v-for="o in outcomes" :key="o" :value="o" /></datalist>
            </div>
            <div class="space-y-1">
              <Label for="audit-since" class="text-xs">{{ m.agents.audit.from }}</Label>
              <Input id="audit-since" v-model="form.since" type="datetime-local" class="h-9" :disabled="loading" />
            </div>
            <div class="space-y-1">
              <Label for="audit-until" class="text-xs">{{ m.agents.audit.to }}</Label>
              <Input id="audit-until" v-model="form.until" type="datetime-local" class="h-9" :disabled="loading" />
            </div>
            <div class="flex flex-wrap gap-2 sm:col-span-3 lg:col-span-6">
              <Button type="submit" size="sm" :disabled="loading">{{ m.agents.audit.apply }}</Button>
              <Button size="sm" variant="outline" :disabled="loading" @click="clear">{{ m.agents.audit.clear }}</Button>
            </div>
          </form>
        </Card>

        <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>

        <Card v-if="rejectionTotals.length" class="space-y-3 p-4 sm:p-6">
          <div>
            <h2 class="text-lg font-semibold">{{ m.agents.audit.rejectedTitle }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.agents.audit.rejectedIntro }}</p>
            <p v-if="agent.rejectionsTruncated" class="text-sm text-amber-700 dark:text-amber-400">
              {{ m.agents.audit.truncated(rejectionTotals.length) }}
            </p>
          </div>
          <div class="overflow-x-auto">
            <table :aria-label="m.agents.audit.rejectedTitle" class="w-full text-left text-sm">
              <thead class="text-xs text-muted-foreground">
                <tr><th class="py-2 pr-4 font-medium">{{ m.agents.audit.token }}</th><th class="py-2 pr-4 font-medium">{{ m.agents.audit.reason }}</th><th class="py-2 pr-4 font-medium">{{ m.agents.audit.count }}</th><th class="py-2 font-medium">{{ m.agents.audit.last }}</th></tr>
              </thead>
              <tbody>
                <tr v-for="r in rejectionTotals" :key="r.tokenId + r.reason" class="border-t">
                  <td class="break-words py-2 pr-4">{{ rejectionToken(r) }}</td>
                  <td class="py-2 pr-4 font-mono text-xs">{{ r.reason }}</td>
                  <td class="py-2 pr-4">{{ r.count }}</td>
                  <td class="whitespace-nowrap py-2">{{ formatDateTime(r.last) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </Card>

        <Card class="p-0">
          <p v-if="!loading && !agent.audit.length && !error" class="p-6 text-center text-sm text-muted-foreground">{{ m.agents.audit.empty }}</p>
          <div v-else class="overflow-x-auto">
            <table :aria-label="m.agents.audit.calls" class="w-full text-left text-sm">
              <thead class="text-xs text-muted-foreground">
                <tr>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.time }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.token }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.tool }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.flow }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.run }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.outcome }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.duration }}</th>
                  <th class="px-4 py-2 font-medium">{{ m.agents.audit.args }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="e in agent.audit" :key="e.id" class="border-t align-top">
                  <td class="whitespace-nowrap px-4 py-2">{{ formatDateTime(e.ts) }}</td>
                  <td class="break-words px-4 py-2">{{ e.tokenName || e.tokenId }}</td>
                  <td class="px-4 py-2">
                    <span class="break-all font-mono text-xs"><AgentText :text="e.tool" /></span>
                    <Badge v-if="e.privileged" variant="destructive" class="ml-1">{{ m.agents.audit.privileged }}</Badge>
                  </td>
                  <td class="break-words px-4 py-2">{{ flowName(e.flowId) }}</td>
                  <td class="break-all px-4 py-2 font-mono text-xs">{{ e.runId }}</td>
                  <td class="px-4 py-2"><Badge :variant="outcomeVariant(e.outcome)">{{ e.outcome }}</Badge></td>
                  <td class="whitespace-nowrap px-4 py-2">{{ e.durMs }} ms</td>
                  <td class="min-w-[12rem] px-4 py-2"><code dir="ltr" class="whitespace-pre-wrap break-all text-xs [unicode-bidi:bidi-override]"><AgentText :text="e.argsPreview ?? ''" /></code></td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="agent.auditHasMore" class="border-t p-3 text-center">
            <Button variant="outline" size="sm" :disabled="loading" @click="loadMore">{{ m.agents.audit.loadMore }}</Button>
          </div>
        </Card>
      </template>
    </div>
  </main>
</template>
