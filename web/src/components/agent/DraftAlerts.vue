<script setup lang="ts">
// The alerts of a draft's diff (docs/design/mcp.md §10): each with an icon
// and its label in words, where it is, what it means, the server's message
// and, for settings agents may read, the values (H7: hidden settings carry
// none here; the field changes show them). Every value and message is agent
// data, rendered as text.
import AgentText from '@/components/agent/AgentText.vue'
import { alertKind, alertLocation, alertValues, draftIcon, type DraftAlert } from '@/lib/drafts'

const props = withDefaults(defineProps<{
  alerts: DraftAlert[] | null
  names: ReadonlyMap<string, string>
  emptyText?: string
}>(), { emptyText: 'No alerts: no destination, secret, credential or risk changes.' })

const list = () => props.alerts ?? []
</script>

<template>
  <p v-if="!list().length" class="flex items-center gap-2 rounded-md border p-3 text-sm text-muted-foreground">
    <component :is="draftIcon('ShieldCheck')" class="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
    {{ emptyText }}
  </p>
  <ul v-else class="space-y-2">
    <li
      v-for="(a, i) in list()"
      :key="`${a.kind}-${a.nodeId ?? ''}-${a.path ?? ''}-${i}`"
      class="flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
      :data-alert-kind="a.kind"
    >
      <component :is="draftIcon(alertKind(a.kind).icon)" class="mt-0.5 h-5 w-5 shrink-0 text-amber-700 dark:text-amber-400" aria-hidden="true" />
      <div class="min-w-0 flex-1 space-y-1">
        <p class="flex flex-wrap items-baseline gap-x-2">
          <span class="font-semibold">{{ alertKind(a.kind).label }}</span>
          <span class="break-all text-xs text-muted-foreground"><AgentText :text="alertLocation(a, names)" /></span>
        </p>
        <p class="text-muted-foreground">{{ alertKind(a.kind).description }}</p>
        <p v-if="a.message" class="whitespace-pre-wrap break-words"><AgentText :text="a.message" /></p>
        <dl v-if="alertValues(a).before !== undefined || alertValues(a).after !== undefined" class="grid gap-1 text-xs sm:grid-cols-2">
          <div v-if="alertValues(a).before !== undefined">
            <dt class="text-muted-foreground">Before</dt>
            <dd dir="ltr" class="whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="alertValues(a).before!" /></dd>
          </div>
          <div v-if="alertValues(a).after !== undefined">
            <dt class="text-muted-foreground">After</dt>
            <dd dir="ltr" class="whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="alertValues(a).after!" /></dd>
          </div>
        </dl>
        <p v-else-if="alertValues(a).inFieldDiff" class="text-xs text-muted-foreground">The values are in the node's changes below.</p>
      </div>
    </li>
  </ul>
</template>
