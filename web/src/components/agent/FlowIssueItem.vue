<script setup lang="ts">
// One Lint issue (flow.Issue) as a list item, as the draft review and the
// flow assistant show them: its severity (icon and word, never colour
// alone), where it is and its code, and its message. The location and the
// message can come from an agent or a model: shown through AgentText.
// bordered: the item's border takes the severity's colour.
import AgentText from './AgentText.vue'
import { draftIcon, severityInfo, type FlowIssue } from '@/lib/drafts'

defineProps<{ issue: FlowIssue; where: string; bordered?: boolean }>()
</script>

<template>
  <li class="flex items-start gap-2 rounded-md border p-2" :class="bordered ? (issue.severity === 'error' ? 'border-destructive/40' : 'border-amber-500/40') : ''">
    <component :is="draftIcon(severityInfo(issue.severity).icon)" class="mt-0.5 h-4 w-4 shrink-0" :class="issue.severity === 'error' ? 'text-destructive' : 'text-amber-700 dark:text-amber-400'" aria-hidden="true" />
    <div class="min-w-0">
      <p><span class="font-semibold">{{ severityInfo(issue.severity).label }}</span> <span class="break-all font-mono text-xs text-muted-foreground"><AgentText :text="where" /> · {{ issue.code }}</span></p>
      <p class="whitespace-pre-wrap break-words"><AgentText :text="issue.message" /></p>
    </div>
  </li>
</template>
