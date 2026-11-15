<script setup lang="ts">
// What the LLM nodes keep in PiWorker's database (docs/design/llm.md §1.6,
// §1.7, §8): the conversations of their memory and the knowledge base, in
// two tabs. The panels read their lists each time the dialog opens.
import { computed, nextTick, ref } from 'vue'
import { Button, Dialog } from '@/components/ui'
import { Brain, Library, MessagesSquare } from 'lucide-vue-next'
import ConversationsPanel from '@/components/llm/ConversationsPanel.vue'
import KnowledgePanel from '@/components/llm/KnowledgePanel.vue'
import { messages as m } from '@/i18n'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

type Tab = 'conversations' | 'knowledge'
const tab = ref<Tab>('conversations')
const tabs = computed(() => [
  { id: 'conversations' as const, label: m.value.llm.memory.conversations.tab, icon: MessagesSquare },
  { id: 'knowledge' as const, label: m.value.llm.memory.knowledge.tab, icon: Library }
])

// Tabs move with the arrow keys, Home and End (one tab stop: the selected one).
async function onKey(event: KeyboardEvent) {
  const ids = tabs.value.map(x => x.id)
  const at = ids.indexOf(tab.value)
  const next = { ArrowRight: at + 1, ArrowLeft: at - 1, Home: 0, End: ids.length - 1 }[event.key]
  if (next === undefined) return
  event.preventDefault()
  tab.value = ids[(next + ids.length) % ids.length]
  await nextTick()
  document.getElementById(`ai-memory-tab-${tab.value}`)?.focus()
}
</script>

<template>
  <Dialog :title="m.llm.memory.title" class="max-w-3xl" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-start gap-2">
          <Brain class="mt-1 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div>
            <h2 class="text-lg font-semibold">{{ m.llm.memory.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.llm.memory.intro }}</p>
          </div>
        </div>

        <div role="tablist" :aria-label="m.llm.memory.sections" class="flex gap-1 overflow-x-auto border-b" @keydown="onKey">
          <button v-for="x in tabs" :id="`ai-memory-tab-${x.id}`" :key="x.id" type="button" role="tab" :aria-selected="tab === x.id" :aria-controls="`ai-memory-panel-${x.id}`"
            :tabindex="tab === x.id ? 0 : -1"
            class="-mb-px flex shrink-0 items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :class="tab === x.id ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground'"
            @click="tab = x.id">
            <component :is="x.icon" class="h-4 w-4" aria-hidden="true" />{{ x.label }}
          </button>
        </div>

        <!-- Both stay mounted: switching tabs keeps what each one shows. -->
        <div v-show="tab === 'conversations'" id="ai-memory-panel-conversations" role="tabpanel" aria-labelledby="ai-memory-tab-conversations">
          <ConversationsPanel :open="open" />
        </div>
        <div v-show="tab === 'knowledge'" id="ai-memory-panel-knowledge" role="tabpanel" aria-labelledby="ai-memory-tab-knowledge">
          <KnowledgePanel :open="open" />
        </div>

        <div class="flex justify-end border-t pt-3">
          <Button variant="outline" @click="close">{{ m.common.done }}</Button>
        </div>
      </div>
    </template>
  </Dialog>
</template>
