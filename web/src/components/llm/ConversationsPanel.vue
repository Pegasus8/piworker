<script setup lang="ts">
// The conversations the LLM nodes remember (docs/design/llm.md §1.6, §8):
// listed by key, opened as plain text, cleared. Keys and turns come from
// messages that reached the flows: shown through AgentText (hidden
// characters escaped), and confirmations quote them with visibleText.
import { computed, nextTick, ref, watch } from 'vue'
import { Button, Input } from '@/components/ui'
import { ArrowLeft, Eraser, Loader2, MessagesSquare, RefreshCw } from 'lucide-vue-next'
import AgentText from '@/components/agent/AgentText.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useLLMStore } from '@/stores/llm'
import { errorMessage } from '@/lib/llm'
import { visibleText } from '@/lib/agentAccess'
import { messages as m, formatDateTime, formatNumber, formatRelative } from '@/i18n'
import { filterConversations } from './memory'
import type { LLMConversation, LLMTurn } from '@/types'

// open: the dialog is open (each opening reads the list again).
const props = defineProps<{ open: boolean }>()

const store = useLLMStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const t = computed(() => m.value.llm.memory.conversations)
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const query = ref('')
const shown = computed(() => filterConversations(store.conversations, query.value))
// The conversation shown, and the key being opened.
const selected = ref<LLMConversation | null>(null)
const opening = ref('')
const heading = ref<HTMLElement>()
const now = ref(Date.now())
// Kept in script so the literal braces don't confuse the template parser.
const exampleKey = 'telegram:{{payload.message.chat.id}}'

async function load() {
  loading.value = true
  error.value = ''
  now.value = Date.now()
  try {
    await store.fetchConversations()
    loaded.value = true
  } catch (e) {
    error.value = errorMessage(e, t.value.loadFailed)
  } finally {
    loading.value = false
  }
}

watch(() => props.open, open => {
  if (!open) return
  selected.value = null
  feedback.value = null
  query.value = ''
  void load()
}, { immediate: true })

async function openConversation(key: string) {
  if (opening.value) return
  opening.value = key
  feedback.value = null
  try {
    selected.value = await store.getConversation(key)
    now.value = Date.now()
    await nextTick()
    heading.value?.focus()
  } catch {
    feedback.value = { kind: 'error', text: t.value.openFailed }
  } finally {
    opening.value = ''
  }
}

function back() {
  selected.value = null
}

async function clear(key: string) {
  const text = t.value
  if (pending.value || !await confirmation.value?.ask(text.clearTitle, text.clearText(visibleText(key)), text.clearConfirm)) return
  const ok = await run(() => store.clearConversation(key), text.cleared, e => errorMessage(e, text.clearFailed))
  if (ok && selected.value?.key === key) selected.value = null
}

const roleLabel = (turn: LLMTurn) => t.value.roles[turn.role] ?? turn.role
</script>

<template>
  <div class="space-y-3">
    <OperationFeedback :message="feedback" @dismiss="feedback = null" />

    <!-- One conversation, oldest message first. -->
    <section v-if="selected" class="space-y-3" aria-labelledby="ai-memory-conversation">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <Button variant="ghost" size="sm" @click="back"><ArrowLeft class="mr-1 h-4 w-4" aria-hidden="true" />{{ t.back }}</Button>
        <Button variant="outline" size="sm" class="text-destructive" :disabled="pending" :aria-label="t.clearLabel(visibleText(selected.key))" @click="clear(selected.key)">
          <Eraser class="mr-1 h-4 w-4" aria-hidden="true" />{{ t.clear }}
        </Button>
      </div>
      <div>
        <h3 id="ai-memory-conversation" ref="heading" tabindex="-1" class="break-all font-mono text-sm font-semibold focus:outline-none"><AgentText :text="selected.key" /></h3>
        <p class="text-xs text-muted-foreground">{{ t.turns(selected.turns.length, formatNumber(selected.turns.length)) }}</p>
      </div>
      <ol class="max-h-[50vh] space-y-2 overflow-auto rounded border p-2" :aria-label="t.messagesLabel">
        <li v-for="(turn, i) in selected.turns" :key="i" class="rounded-md p-2 text-sm" :class="turn.role === 'assistant' ? 'bg-primary/5' : 'bg-muted/60'">
          <p class="mb-1 flex flex-wrap items-baseline justify-between gap-x-2 text-xs text-muted-foreground">
            <span class="font-semibold text-foreground">{{ roleLabel(turn) }}</span>
            <time :datetime="turn.at">{{ formatDateTime(turn.at) }}</time>
          </p>
          <p class="whitespace-pre-wrap break-words"><AgentText :text="turn.text" /></p>
        </li>
      </ol>
    </section>

    <!-- The conversations -->
    <template v-else>
      <div class="flex items-center gap-2">
        <Input v-if="store.conversations.length" v-model="query" type="search" :placeholder="t.filter" :aria-label="t.filter" class="h-9" autocomplete="off" spellcheck="false" />
        <Button variant="outline" size="icon" class="ml-auto h-9 w-9 shrink-0" :disabled="loading" :aria-label="m.llm.memory.refresh" :title="m.llm.memory.refresh" @click="load">
          <RefreshCw class="h-4 w-4" :class="loading ? 'animate-spin' : ''" aria-hidden="true" />
        </Button>
      </div>

      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button size="sm" variant="outline" @click="load">{{ m.common.retry }}</Button></p>
      <p v-if="loading && !store.conversations.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.common.loading }}</p>
      <div v-else-if="loaded && !store.conversations.length" class="flex items-start gap-2 rounded border border-dashed p-4 text-sm text-muted-foreground">
        <MessagesSquare class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
        <p>{{ t.empty[0] }}<code class="break-all">{{ exampleKey }}</code>{{ t.empty[1] }}</p>
      </div>
      <template v-else-if="store.conversations.length">
        <p v-if="!shown.items.length" role="status" class="p-3 text-center text-sm text-muted-foreground">{{ t.noMatch }}</p>
        <ul v-else class="max-h-80 divide-y overflow-auto rounded border" :aria-label="t.listLabel">
          <li v-for="c in shown.items" :key="c.key" class="flex items-start justify-between gap-2 px-3 py-2 text-sm">
            <div class="min-w-0">
              <p class="break-all font-mono"><AgentText :text="c.key" /></p>
              <p class="text-xs text-muted-foreground">
                {{ t.turns(c.turns, formatNumber(c.turns)) }} ·
                <time :datetime="c.updatedAt" :title="formatDateTime(c.updatedAt)">{{ m.llm.memory.updated(formatRelative(c.updatedAt, now)) }}</time>
              </p>
            </div>
            <div class="flex shrink-0 gap-1">
              <Button variant="outline" size="sm" :disabled="!!opening" :aria-label="t.openLabel(visibleText(c.key))" @click="openConversation(c.key)">
                <Loader2 v-if="opening === c.key" class="h-3.5 w-3.5 animate-spin sm:mr-1" aria-hidden="true" />
                <MessagesSquare v-else class="h-3.5 w-3.5 sm:mr-1" aria-hidden="true" />
                <span class="hidden sm:inline">{{ t.open }}</span>
              </Button>
              <Button variant="ghost" size="icon" class="h-9 w-9 text-destructive" :disabled="pending" :aria-label="t.clearLabel(visibleText(c.key))" :title="t.clear" @click="clear(c.key)">
                <Eraser class="h-3.5 w-3.5" aria-hidden="true" />
              </Button>
            </div>
          </li>
        </ul>
        <p v-if="shown.more" class="text-xs text-muted-foreground">{{ t.more(formatNumber(shown.more)) }}</p>
      </template>
    </template>
    <ConfirmDialog ref="confirmation" />
  </div>
</template>
