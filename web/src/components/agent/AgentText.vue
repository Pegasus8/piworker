<script setup lang="ts">
// Text from an agent (or a flow's result), rendered as text nodes only. Every
// invisible or direction-changing character is replaced by a highlighted
// \uXXXX escape, so what is shown is what was sent (Trojan Source).
import { computed } from 'vue'
import { revealHidden } from '@/lib/agentAccess'
import { messages as m } from '@/i18n'

const props = defineProps<{ text: string }>()
const parts = computed(() => revealHidden(props.text))
</script>

<template>
  <template v-for="(p, i) in parts" :key="i"><mark v-if="p.hidden" class="rounded-sm bg-destructive/15 px-0.5 font-mono text-destructive" :title="m.agents.hiddenCharacter(p.code ?? '')">{{ p.text }}</mark><template v-else>{{ p.text }}</template></template>
</template>
