<script setup lang="ts">
import { ref, computed } from 'vue'
import { useObservabilityStore } from '@/stores/observability'
import { useFlowsStore } from '@/stores/flows'
import { Button, Badge, Dialog } from '@/components/ui'
import { Activity, ChevronDown, ChevronUp, Trash2, RefreshCw } from 'lucide-vue-next'
import { runActor } from '@/lib/runActor'
import { messages as m, formatDateTime, locale as currentLocale } from '@/i18n'

const flows = useFlowsStore()
const obs = useObservabilityStore()

const collapsed = ref(true)
const tab = ref<'runs' | 'debug'>('runs')

// Live while the stream is open; a stopped flow is idle, not offline.
const running = computed(() => !!flows.currentFlow?.running)
const status = computed(() => obs.connected ? m.value.activity.live : running.value ? m.value.activity.connecting : m.value.activity.idle)
const statusColor = computed(() => (obs.connected ? 'bg-emerald-500' : running.value ? 'bg-amber-500 animate-pulse' : 'bg-muted-foreground'))
const nodeName = (id: string, type: string) => flows.nodes.find(n => n.id === id)?.data.label || type
const phase = (value: string) => (m.value.activity.phases as Record<string, string>)[value] ?? value
const selectedRun = computed(() => obs.runs.find(r => r.id === obs.selectedRunId))

function fmtTime(ts: number | string): string {
  const d = typeof ts === 'number' ? new Date(ts) : new Date(ts)
  return d.toLocaleTimeString(currentLocale.value)
}

function runBadgeVariant(status: string): 'default' | 'secondary' | 'destructive' {
  if (status === 'error') return 'destructive'
  if (status === 'running') return 'secondary'
  return 'default'
}

function pretty(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}
</script>

<template>
  <div class="shrink-0 border-t bg-card">
    <!-- Header bar -->
    <div class="flex min-h-9 flex-wrap items-center gap-1 px-2 text-sm">
      <button class="flex items-center gap-1 rounded px-1 font-medium hover:bg-accent" :aria-expanded="!collapsed" @click="collapsed = !collapsed">
        <Activity class="h-4 w-4 text-primary" aria-hidden="true" />
        {{ m.activity.title }}
      </button>
      <span class="flex items-center gap-1 text-xs text-muted-foreground" :title="running ? '' : m.activity.idleHint">
        <span :class="['h-2 w-2 rounded-full', statusColor]" />
        {{ status }}
      </span>

      <div class="flex items-center gap-1">
        <button
          class="rounded px-2 py-0.5 text-xs"
          :class="tab === 'runs' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'"
          @click="tab = 'runs'; collapsed = false"
        >
          {{ m.activity.runs }} <span class="opacity-60">({{ obs.runs.length }})</span>
        </button>
        <button
          class="rounded px-2 py-0.5 text-xs"
          :class="tab === 'debug' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'"
          @click="tab = 'debug'; collapsed = false"
        >
          {{ m.activity.debug }} <span class="opacity-60">({{ obs.debugLog.length }})</span>
        </button>
      </div>

      <div class="ml-auto flex items-center gap-1">
        <Button variant="ghost" size="icon" class="h-7 w-7"   :title="m.activity.refresh" :aria-label="m.activity.refresh" @click="obs.refreshRuns()">
          <RefreshCw class="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" class="h-7 w-7" :title="m.activity.clear" :aria-label="m.activity.clear" @click="obs.clearActivity()">
          <Trash2 class="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" :aria-label="collapsed ? m.activity.expand : m.activity.collapse" class="h-9 w-9" @click="collapsed = !collapsed">
          <ChevronDown v-if="!collapsed" class="h-4 w-4" />
          <ChevronUp v-else class="h-4 w-4" />
        </Button>
      </div>
    </div>

    <!-- Body -->
    <div v-if="!collapsed" class="h-48 overflow-auto border-t">
      <!-- Runs -->
      <div v-if="tab === 'runs'" class="divide-y">
        <p v-if="obs.runsLoading" role="status" class="p-4 text-sm">{{ m.activity.loadingRuns }}</p>
        <p v-if="obs.runsError" role="alert" class="p-4 text-sm text-destructive">{{ obs.runsError }} <Button size="sm" variant="outline" @click="obs.refreshRuns()">{{ m.common.retry }}</Button></p>
        <p v-if="!obs.runs.length && !obs.runsLoading && !obs.runsError" class="p-4 text-center text-xs text-muted-foreground">
          {{ m.activity.noRuns }}
        </p>
        <button
          v-for="run in obs.runs"
          :aria-label="m.activity.viewRun(run.id)" @click="obs.openRun(run.id)"
          :key="run.id"
          class="flex w-full flex-wrap items-center gap-3 px-3 py-2 text-left text-xs hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Badge :variant="runBadgeVariant(run.status)" class="w-16 justify-center">
            {{ phase(run.status) }}
          </Badge>
          <span class="font-mono text-muted-foreground">{{ run.id.slice(0, 8) }}</span>
          <span :title="runActor(run.actor).title" data-testid="run-actor" class="max-w-[12rem] truncate"
            :class="runActor(run.actor).kind === 'agent' ? 'font-medium text-primary' : 'text-muted-foreground'">{{ runActor(run.actor).label }}</span>
          <span>{{ m.activity.nodeCount(run.nodeCount) }}</span>
          <span v-if="run.errorCount > 0" class="text-destructive">{{ m.activity.errorCount(run.errorCount) }}</span>
          <span class="ml-auto text-muted-foreground">{{ fmtTime(run.startedAt) }}</span>
        </button>
      </div>

      <!-- Debug inspector -->
      <div v-else class="divide-y">
        <p v-if="!obs.debugLog.length" class="p-4 text-center text-xs text-muted-foreground">
          {{ m.activity.noDebug }}
        </p>
        <div v-for="(entry, i) in obs.debugLog" :key="i" class="px-3 py-2 text-xs">
          <div class="mb-1 flex items-center gap-2">
            <span class="font-medium text-primary">{{ nodeName(entry.nodeId, entry.nodeType) }}</span>
            <span class="font-mono text-muted-foreground">{{ entry.nodeId.slice(0, 8) }}</span>
            <span class="ml-auto text-muted-foreground">{{ fmtTime(entry.ts) }}</span>
          </div>
          <pre class="overflow-x-auto rounded bg-muted/50 p-2 text-[11px] leading-snug">{{ pretty(entry.debug) }}</pre>
        </div>
      </div>
    </div>
    <Dialog :open="!!obs.selectedRunId"   :title="m.activity.runDetails" @update:open="!$event && obs.closeRun()">
      <h2 class="text-lg font-semibold">{{ m.activity.runDetails }}</h2>
      <p class="mt-1 break-all font-mono text-xs text-muted-foreground">{{ obs.selectedRunId }}</p>
      <p v-if="selectedRun" class="mt-1 text-sm">{{ m.activity.startedBy }} <span :title="runActor(selectedRun.actor).title">{{ runActor(selectedRun.actor).label }}</span></p>
      <p class="mt-2 text-sm text-muted-foreground">{{ m.activity.runDetailsHint }}</p>
      <p v-if="obs.eventsLoading" role="status" class="my-4 text-sm">{{ m.activity.loadingEvents }}</p>
      <p v-else-if="obs.eventsError" role="alert" class="my-4 text-sm text-destructive">{{ obs.eventsError }} <Button size="sm" variant="outline" @click="obs.selectedRunId && obs.openRun(obs.selectedRunId)">{{ m.common.retry }}</Button></p>
      <p v-else-if="!obs.runEvents.length" class="my-4 text-sm text-muted-foreground">{{ m.activity.noEvents }}</p>
      <ol v-else class="my-4 max-h-[50dvh] space-y-3 overflow-auto">
        <li v-for="event in obs.runEvents" :key="event.id" class="rounded-lg border p-3 text-sm">
          <div class="flex flex-wrap items-center gap-2"><strong>{{ nodeName(event.nodeId, event.nodeType) }}</strong><Badge :variant="runBadgeVariant(event.phase)">{{ phase(event.phase) }}</Badge><span v-if="event.durationMs != null">{{ event.durationMs }} ms</span></div>
          <p class="mt-1 break-all text-xs text-muted-foreground">{{ event.nodeType }} · {{ event.nodeId }} · {{ formatDateTime(event.createdAt) }}</p>
          <pre v-if="event.error" class="mt-2 whitespace-pre-wrap break-words text-destructive">{{ event.error }}</pre>
        </li>
      </ol>
      <div class="mt-4 flex justify-end gap-2"><Button variant="outline" :disabled="obs.eventsLoading" @click="obs.selectedRunId && obs.openRun(obs.selectedRunId)">{{ m.activity.refreshDetails }}</Button><Button @click="obs.closeRun()">{{ m.common.done }}</Button></div>
    </Dialog>
  </div>
</template>
