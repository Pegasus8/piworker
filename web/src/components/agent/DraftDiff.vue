<script setup lang="ts">
// A draft's per-node and connection diff (docs/design/mcp.md §10): added,
// removed and changed nodes with field-level before/after, Sensitive values
// as "hidden" (the server never sends them), destination fields highlighted
// with an icon and a label, the node's alerts named, and the connections
// added and removed. Node IDs, ports and values are agent data: text only.
import { computed } from 'vue'
import { Lock, MapPin, Minus, Move, Pencil, Plus } from 'lucide-vue-next'
import { Badge } from '@/components/ui'
import { visibleText } from '@/lib/agentAccess'
import AgentText from '@/components/agent/AgentText.vue'
import {
  alertKind, alertsByNode, connectionText, diffSummary, draftIcon, fieldRows, groupNodes,
  type Change, type DiffReport, type NodeDiff
} from '@/lib/drafts'

const props = defineProps<{ diff: DiffReport; names: ReadonlyMap<string, string>; label: string }>()

const groups = computed(() => groupNodes(props.diff))
const byNode = computed(() => alertsByNode(props.diff.alerts))
const added = computed(() => props.diff.connections?.added ?? [])
const removed = computed(() => props.diff.connections?.removed ?? [])
const changeIcon: Record<Change, typeof Plus> = { added: Plus, removed: Minus, changed: Pencil }
const changeLabel: Record<Change, string> = { added: 'Added', removed: 'Removed', changed: 'Changed' }
const nodeAlertKinds = (n: NodeDiff) => [...new Set((byNode.value.get(n.nodeId) ?? []).map(a => a.kind))]
</script>

<template>
  <div class="space-y-4">
    <p class="text-sm text-muted-foreground">{{ diffSummary(diff) }}</p>

    <section v-for="g in groups" :key="g.change" :aria-label="`${label}: ${g.label}`" class="space-y-2">
      <h4 class="flex items-center gap-2 text-sm font-semibold">
        <component :is="changeIcon[g.change]" class="h-4 w-4" aria-hidden="true" />{{ g.label }} ({{ g.nodes.length }})
      </h4>
      <article
        v-for="n in g.nodes"
        :key="`${g.change}-${n.nodeId}`"
        class="space-y-2 rounded-md border p-3"
        :class="g.change === 'added' ? 'border-l-4 border-l-primary' : g.change === 'removed' ? 'border-l-4 border-l-destructive' : 'border-l-4 border-l-blue-500'"
        :aria-label="visibleText(`${changeLabel[n.change]} node ${n.typeName || n.type} ${n.nodeId}`)"
      >
        <div class="flex flex-wrap items-center gap-2 text-sm">
          <Badge variant="outline" class="gap-1"><component :is="changeIcon[n.change]" class="h-3 w-3" aria-hidden="true" />{{ changeLabel[n.change] }}</Badge>
          <span class="font-medium"><AgentText :text="n.typeName || n.type" /></span>
          <span class="break-all font-mono text-xs text-muted-foreground"><AgentText :text="n.nodeId" /></span>
          <span v-if="n.typeName && n.type" class="break-all font-mono text-xs text-muted-foreground">(<AgentText :text="n.type" />)</span>
          <Badge
            v-for="kind in nodeAlertKinds(n)"
            :key="kind"
            variant="outline"
            class="gap-1 border-amber-500/60 text-amber-800 dark:text-amber-300"
          >
            <component :is="draftIcon(alertKind(kind).icon)" class="h-3 w-3" aria-hidden="true" />{{ alertKind(kind).label }}
          </Badge>
        </div>
        <p v-if="n.moved" class="flex items-center gap-1 text-xs text-muted-foreground"><Move class="h-3 w-3" aria-hidden="true" />Moved on the canvas.</p>
        <div v-if="fieldRows(n).length" class="overflow-x-auto">
          <table class="w-full min-w-[18rem] table-fixed text-left text-xs">
            <caption class="sr-only">Changed fields of node {{ visibleText(n.nodeId) }}</caption>
            <thead class="text-muted-foreground">
              <tr><th scope="col" class="w-1/4 py-1 pr-2 font-medium">Field</th><th scope="col" class="py-1 pr-2 font-medium">Before</th><th scope="col" class="py-1 font-medium">After</th></tr>
            </thead>
            <tbody>
              <tr
                v-for="r in fieldRows(n)"
                :key="r.path"
                class="border-t align-top"
                :class="r.destination && 'bg-amber-500/10'"
                :data-destination="r.destination ? 'true' : undefined"
              >
                <th scope="row" class="py-1.5 pr-2 font-normal">
                  <span class="break-all font-mono"><AgentText :text="r.label" /></span>
                  <span v-if="r.destination" class="mt-0.5 flex items-center gap-1 font-semibold text-amber-800 dark:text-amber-300">
                    <MapPin class="h-3 w-3 shrink-0" aria-hidden="true" />Destination
                  </span>
                  <span v-if="r.sensitive" class="mt-0.5 flex items-center gap-1 text-muted-foreground"><Lock class="h-3 w-3 shrink-0" aria-hidden="true" />Credential</span>
                </th>
                <td v-for="(v, side) in [r.before, r.after]" :key="side" class="py-1.5 pr-2">
                  <span v-if="v === null" class="text-muted-foreground">{{ side === 0 ? '(not set)' : '(removed)' }}</span>
                  <span v-else-if="v.hidden" class="inline-flex items-center gap-1 italic text-muted-foreground" title="Sensitive value: never shown">
                    <Lock class="h-3 w-3" aria-hidden="true" />hidden
                  </span>
                  <span v-else dir="ltr" class="block max-h-48 overflow-auto whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="v.text" /></span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>
    </section>

    <section v-if="added.length || removed.length" :aria-label="`${label}: connections`" class="space-y-2">
      <h4 class="text-sm font-semibold">Connections</h4>
      <ul class="space-y-1 text-xs">
        <li v-for="c in added" :key="`a-${c.id}-${c.sourceNode}-${c.sourcePort}-${c.targetNode}-${c.targetPort}`" class="flex items-start gap-2">
          <Badge variant="outline" class="shrink-0 gap-1"><Plus class="h-3 w-3" aria-hidden="true" />Added</Badge>
          <span class="break-all font-mono"><AgentText :text="connectionText(c, names)" /></span>
        </li>
        <li v-for="c in removed" :key="`r-${c.id}-${c.sourceNode}-${c.sourcePort}-${c.targetNode}-${c.targetPort}`" class="flex items-start gap-2">
          <Badge variant="outline" class="shrink-0 gap-1"><Minus class="h-3 w-3" aria-hidden="true" />Removed</Badge>
          <span class="break-all font-mono"><AgentText :text="connectionText(c, names)" /></span>
        </li>
      </ul>
    </section>
  </div>
</template>
