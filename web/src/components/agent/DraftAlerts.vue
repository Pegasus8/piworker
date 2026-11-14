<script setup lang="ts">
// The alerts of a draft's diff (docs/design/mcp.md §10): each with an icon
// and its label in words, where it is, what it means, the server's message
// and, for settings agents may read, the values (H7: hidden settings carry
// none here). A hidden setting's values are pointed at only when the field
// changes of the same comparison list that setting; a setting that did not
// change (a node disabled or enabled) shows its value from the proposed
// flow. Every value and message is agent data, rendered as text.
import { computed } from 'vue'
import AgentText from '@/components/agent/AgentText.vue'
import { messages as m } from '@/i18n'
import { alertKind, alertLocation, alertValues, draftIcon, type DraftAlert } from '@/lib/drafts'

const props = defineProps<{
  alerts: DraftAlert[] | null
  names: ReadonlyMap<string, string>
  /** Node ID → the field paths of the same comparison's node diff (fieldPaths). */
  fields?: ReadonlyMap<string, ReadonlySet<string>>
  /** Node ID → its settings in the proposed flow (proposedSettings). */
  proposed?: ReadonlyMap<string, Readonly<Record<string, unknown>>>
  /** Default: no alerts at all (m.drafts.noAlerts). */
  emptyText?: string
}>()

const list = computed(() => (props.alerts ?? []).map(a => ({
  a,
  kind: alertKind(a.kind),
  values: alertValues(a, { fields: props.fields, proposed: props.proposed }),
  onSetting: !!a.nodeId && !!a.path?.startsWith('config.')
})))
</script>

<template>
  <p v-if="!list.length" class="flex items-center gap-2 rounded-md border p-3 text-sm text-muted-foreground">
    <component :is="draftIcon('ShieldCheck')" class="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
    {{ emptyText ?? m.drafts.noAlerts }}
  </p>
  <ul v-else class="space-y-2">
    <li
      v-for="({ a, kind, values, onSetting }, i) in list"
      :key="`${a.kind}-${a.nodeId ?? ''}-${a.path ?? ''}-${i}`"
      class="flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
      :data-alert-kind="a.kind"
    >
      <component :is="draftIcon(kind.icon)" class="mt-0.5 h-5 w-5 shrink-0 text-amber-700 dark:text-amber-400" aria-hidden="true" />
      <div class="min-w-0 flex-1 space-y-1">
        <p class="flex flex-wrap items-baseline gap-x-2">
          <span class="font-semibold">{{ kind.label }}</span>
          <span class="break-all text-xs text-muted-foreground"><AgentText :text="alertLocation(a, names)" /></span>
        </p>
        <p class="text-muted-foreground">{{ kind.description }}</p>
        <p v-if="a.message" class="whitespace-pre-wrap break-words"><AgentText :text="a.message" /></p>
        <dl v-if="values.before !== undefined || values.after !== undefined" class="grid gap-1 text-xs sm:grid-cols-2">
          <div v-if="values.before !== undefined">
            <dt class="text-muted-foreground">{{ m.drafts.before }}</dt>
            <dd dir="ltr" class="whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="values.before" /></dd>
          </div>
          <div v-if="values.after !== undefined">
            <dt class="text-muted-foreground">{{ m.drafts.after }}</dt>
            <dd dir="ltr" class="whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="values.after" /></dd>
          </div>
        </dl>
        <p v-else-if="values.inFieldDiff" class="text-xs text-muted-foreground">{{ m.drafts.valuesInDiff }}</p>
        <dl v-else-if="values.unchanged !== undefined" class="text-xs">
          <dt class="text-muted-foreground">{{ m.drafts.unchangedValue }}</dt>
          <dd dir="ltr" class="whitespace-pre-wrap break-all font-mono [unicode-bidi:bidi-override]"><AgentText :text="values.unchanged" /></dd>
        </dl>
        <p v-else-if="onSetting" class="text-xs text-muted-foreground">{{ m.drafts.valueNotShown }}</p>
      </div>
    </li>
  </ul>
</template>
