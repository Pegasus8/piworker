<script setup lang="ts">
import { ref, computed } from 'vue'
import { useObservabilityStore } from '@/stores/observability'
import { Button, Badge } from '@/components/ui'
import { Activity, ChevronDown, ChevronUp, Trash2, RefreshCw } from 'lucide-vue-next'

const obs = useObservabilityStore()

const collapsed = ref(true)
const tab = ref<'runs' | 'debug'>('runs')

const statusColor = computed(() => (obs.connected ? 'bg-emerald-500' : 'bg-muted-foreground'))

function fmtTime(ts: number | string): string {
  const d = typeof ts === 'number' ? new Date(ts) : new Date(ts)
  return d.toLocaleTimeString()
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
  <div class="border-t bg-card">
    <!-- Header bar -->
    <div class="flex h-9 items-center gap-2 px-3 text-sm">
      <Activity class="h-4 w-4 text-primary" />
      <span class="font-medium">Activity</span>
      <span class="flex items-center gap-1 text-xs text-muted-foreground">
        <span :class="['h-2 w-2 rounded-full', statusColor]" />
        {{ obs.connected ? 'live' : 'offline' }}
      </span>

      <div class="ml-4 flex items-center gap-1">
        <button
          class="rounded px-2 py-0.5 text-xs"
          :class="tab === 'runs' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'"
          @click="tab = 'runs'; collapsed = false"
        >
          Runs <span class="opacity-60">({{ obs.runs.length }})</span>
        </button>
        <button
          class="rounded px-2 py-0.5 text-xs"
          :class="tab === 'debug' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'"
          @click="tab = 'debug'; collapsed = false"
        >
          Debug <span class="opacity-60">({{ obs.debugLog.length }})</span>
        </button>
      </div>

      <div class="ml-auto flex items-center gap-1">
        <Button variant="ghost" size="icon" class="h-7 w-7" title="Refresh runs" @click="obs.refreshRuns()">
          <RefreshCw class="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" class="h-7 w-7" title="Clear" @click="obs.clearActivity()">
          <Trash2 class="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" class="h-7 w-7" @click="collapsed = !collapsed">
          <ChevronDown v-if="!collapsed" class="h-4 w-4" />
          <ChevronUp v-else class="h-4 w-4" />
        </Button>
      </div>
    </div>

    <!-- Body -->
    <div v-if="!collapsed" class="h-48 overflow-auto border-t">
      <!-- Runs -->
      <div v-if="tab === 'runs'" class="divide-y">
        <p v-if="!obs.runs.length" class="p-4 text-center text-xs text-muted-foreground">
          No runs yet. Deploy the flow and trigger it to see executions here.
        </p>
        <div
          v-for="run in obs.runs"
          :key="run.id"
          class="flex items-center gap-3 px-3 py-2 text-xs"
        >
          <Badge :variant="runBadgeVariant(run.status)" class="w-16 justify-center">
            {{ run.status }}
          </Badge>
          <span class="font-mono text-muted-foreground">{{ run.id.slice(0, 8) }}</span>
          <span>{{ run.nodeCount }} nodes</span>
          <span v-if="run.errorCount > 0" class="text-destructive">{{ run.errorCount }} err</span>
          <span class="ml-auto text-muted-foreground">{{ fmtTime(run.startedAt) }}</span>
        </div>
      </div>

      <!-- Debug inspector -->
      <div v-else class="divide-y">
        <p v-if="!obs.debugLog.length" class="p-4 text-center text-xs text-muted-foreground">
          No debug output. Add a Debug node to a running flow to inspect messages.
        </p>
        <div v-for="(entry, i) in obs.debugLog" :key="i" class="px-3 py-2 text-xs">
          <div class="mb-1 flex items-center gap-2">
            <span class="font-mono text-primary">{{ entry.nodeId.slice(0, 8) }}</span>
            <span class="text-muted-foreground">{{ entry.nodeType }}</span>
            <span class="ml-auto text-muted-foreground">{{ fmtTime(entry.ts) }}</span>
          </div>
          <pre class="overflow-x-auto rounded bg-muted/50 p-2 text-[11px] leading-snug">{{ pretty(entry.debug) }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>
