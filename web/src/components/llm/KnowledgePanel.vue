<script setup lang="ts">
// The knowledge base (docs/design/llm.md §1.7, §8): collections with their
// embedding model; a collection's documents a page at a time; deleting a
// document or a whole collection. Document IDs, texts and metadata come
// from messages that reached the flows: shown through AgentText (hidden
// characters escaped), and confirmations quote them with visibleText.
import { computed, nextTick, ref, watch } from 'vue'
import { Button } from '@/components/ui'
import { ArrowLeft, BookOpen, ChevronLeft, ChevronRight, Library, RefreshCw, Trash2 } from 'lucide-vue-next'
import AgentText from '@/components/agent/AgentText.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useLLMStore } from '@/stores/llm'
import { errorMessage } from '@/lib/llm'
import { visibleText } from '@/lib/agentAccess'
import { messages as m, formatDateTime, formatNumber, formatRelative } from '@/i18n'
import { DOCUMENT_PAGE, metadataText, offsetAfterDelete, pageInfo, textPreview } from './memory'
import type { KnowledgeCollection, KnowledgeDocumentPage } from '@/types'

// open: the dialog is open (each opening reads the collections again).
const props = defineProps<{ open: boolean }>()

const store = useLLMStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const t = computed(() => m.value.llm.memory.knowledge)
const loading = ref(false)
const error = ref('')
const now = ref(Date.now())

// The collection being browsed, and the page of its documents shown.
const browsing = ref<string | null>(null)
const collection = computed(() => store.collections.find(c => c.name === browsing.value))
const page = ref<KnowledgeDocumentPage>({ documents: [], total: 0 })
const offset = ref(0)
const info = computed(() => pageInfo(offset.value, page.value.documents.length, page.value.total))
const docsLoading = ref(false)
const docsError = ref('')
const heading = ref<HTMLElement>()
let docsRequest = 0

async function load() {
  loading.value = true
  error.value = ''
  now.value = Date.now()
  try {
    await store.fetchCollections()
  } catch (e) {
    error.value = errorMessage(e, t.value.loadFailed)
  } finally {
    loading.value = false
  }
}

watch(() => props.open, open => {
  if (!open) return
  back()
  feedback.value = null
  void load()
}, { immediate: true })

// Only the latest read lands (paging quickly, or going back meanwhile).
async function loadDocuments() {
  const name = browsing.value
  if (!name) return
  const request = ++docsRequest
  docsLoading.value = true
  docsError.value = ''
  try {
    const result = await store.fetchDocuments(name, offset.value, DOCUMENT_PAGE)
    if (request === docsRequest) page.value = result
  } catch (e) {
    if (request === docsRequest) docsError.value = errorMessage(e, t.value.documentsFailed)
  } finally {
    if (request === docsRequest) docsLoading.value = false
  }
}

async function browse(name: string) {
  browsing.value = name
  offset.value = 0
  page.value = { documents: [], total: 0 }
  feedback.value = null
  now.value = Date.now()
  void loadDocuments()
  await nextTick()
  heading.value?.focus()
}

function back() {
  docsRequest++
  browsing.value = null
  docsLoading.value = false
  docsError.value = ''
}

function go(pages: number) {
  offset.value = Math.max(0, offset.value + pages * DOCUMENT_PAGE)
  void loadDocuments()
}

async function deleteDocument(id: string) {
  const name = browsing.value
  const text = t.value
  if (!name || pending.value || !await confirmation.value?.ask(text.deleteDocumentTitle, text.deleteDocumentText(visibleText(id)), text.deleteDocumentConfirm)) return
  const ok = await run(() => store.deleteDocument(name, id), text.documentDeleted, e => errorMessage(e, text.deleteDocumentFailed))
  if (!ok || browsing.value !== name) return
  // The last document took the collection with it.
  if (!store.collections.some(c => c.name === name)) {
    back()
    return
  }
  offset.value = offsetAfterDelete(offset.value, page.value.total - 1)
  await loadDocuments()
}

async function deleteCollection(c: KnowledgeCollection) {
  const text = t.value
  if (pending.value || !await confirmation.value?.ask(text.deleteCollectionTitle, text.deleteCollectionText(c.name, c.documents, formatNumber(c.documents)), text.deleteCollectionConfirm)) return
  const ok = await run(() => store.deleteCollection(c.name), text.collectionDeleted, e => errorMessage(e, text.deleteCollectionFailed))
  if (ok && browsing.value === c.name) back()
}

const summary = (c: KnowledgeCollection) => `${t.value.documents(c.documents, formatNumber(c.documents))} · ${t.value.embedding(c.model, formatNumber(c.dimensions))}`
</script>

<template>
  <div class="space-y-3">
    <OperationFeedback :message="feedback" @dismiss="feedback = null" />

    <!-- One collection's documents -->
    <section v-if="browsing" class="space-y-3" aria-labelledby="ai-memory-collection">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <Button variant="ghost" size="sm" @click="back"><ArrowLeft class="mr-1 h-4 w-4" aria-hidden="true" />{{ t.back }}</Button>
        <Button v-if="collection" variant="outline" size="sm" class="text-destructive" :disabled="pending" :aria-label="t.deleteCollectionLabel(collection.name)" @click="deleteCollection(collection)">
          <Trash2 class="mr-1 h-4 w-4" aria-hidden="true" />{{ t.deleteCollection }}
        </Button>
      </div>
      <div>
        <h3 id="ai-memory-collection" ref="heading" tabindex="-1" class="break-all font-mono text-sm font-semibold focus:outline-none">{{ browsing }}</h3>
        <p v-if="collection" class="break-words text-xs text-muted-foreground">{{ summary(collection) }}</p>
      </div>

      <p v-if="docsError" role="alert" class="text-sm text-destructive">{{ docsError }} <Button size="sm" variant="outline" @click="loadDocuments">{{ m.common.retry }}</Button></p>
      <p v-else-if="docsLoading && !page.documents.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.common.loading }}</p>
      <p v-else-if="!page.documents.length" class="p-3 text-center text-sm text-muted-foreground">{{ t.noDocuments }}</p>
      <ul v-else class="max-h-[50vh] divide-y overflow-auto rounded border" :aria-label="t.documentsLabel" :aria-busy="docsLoading">
        <li v-for="d in page.documents" :key="d.id" class="flex items-start gap-2 px-3 py-2 text-sm">
          <!-- A preview line; the whole text and metadata on demand. -->
          <details class="group min-w-0 flex-1">
            <summary class="cursor-pointer">
              <span class="break-all font-mono text-xs font-medium"><AgentText :text="d.id" /></span>
              <span class="ml-2 text-xs text-muted-foreground"><time :datetime="d.createdAt" :title="formatDateTime(d.createdAt)">{{ t.added(formatRelative(d.createdAt, now)) }}</time></span>
              <span class="mt-0.5 block break-words text-muted-foreground group-open:hidden"><AgentText :text="textPreview(d.text)" /></span>
            </summary>
            <div class="mt-2 space-y-2">
              <p class="whitespace-pre-wrap break-words rounded bg-muted/60 p-2"><AgentText :text="d.text" /></p>
              <p v-if="metadataText(d.metadata)" class="break-all text-xs">
                <span class="font-semibold">{{ t.metadata }}:</span> <code><AgentText :text="metadataText(d.metadata)" /></code>
              </p>
            </div>
          </details>
          <Button variant="ghost" size="icon" class="h-9 w-9 shrink-0 text-destructive" :disabled="pending" :aria-label="t.deleteDocumentLabel(visibleText(d.id))" :title="t.deleteDocumentConfirm" @click="deleteDocument(d.id)">
            <Trash2 class="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
        </li>
      </ul>

      <div v-if="info.hasPrev || info.hasNext" class="flex items-center justify-between gap-2">
        <Button variant="outline" size="sm" :disabled="!info.hasPrev || docsLoading" :aria-label="t.previous" :title="t.previous" @click="go(-1)">
          <ChevronLeft class="h-4 w-4" aria-hidden="true" />
        </Button>
        <p class="text-xs text-muted-foreground" aria-live="polite">{{ t.range(formatNumber(info.from), formatNumber(info.to), formatNumber(info.total)) }}</p>
        <Button variant="outline" size="sm" :disabled="!info.hasNext || docsLoading" :aria-label="t.next" :title="t.next" @click="go(1)">
          <ChevronRight class="h-4 w-4" aria-hidden="true" />
        </Button>
      </div>
    </section>

    <!-- The collections -->
    <template v-else>
      <div class="flex justify-end">
        <Button variant="outline" size="icon" class="h-9 w-9" :disabled="loading" :aria-label="m.llm.memory.refresh" :title="m.llm.memory.refresh" @click="load">
          <RefreshCw class="h-4 w-4" :class="loading ? 'animate-spin' : ''" aria-hidden="true" />
        </Button>
      </div>

      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button size="sm" variant="outline" @click="load">{{ m.common.retry }}</Button></p>
      <p v-if="loading && !store.collections.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.common.loading }}</p>
      <div v-else-if="store.collectionsLoaded && !store.collections.length" class="flex items-start gap-2 rounded border border-dashed p-4 text-sm text-muted-foreground">
        <Library class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
        <p>{{ t.empty }}</p>
      </div>
      <ul v-else-if="store.collections.length" class="max-h-80 divide-y overflow-auto rounded border" :aria-label="t.listLabel">
        <li v-for="c in store.collections" :key="c.name" class="flex items-start justify-between gap-2 px-3 py-2 text-sm">
          <div class="min-w-0">
            <p class="break-all font-mono font-medium">{{ c.name }}</p>
            <p class="break-words text-xs text-muted-foreground">
              {{ summary(c) }} ·
              <time :datetime="c.updatedAt" :title="formatDateTime(c.updatedAt)">{{ m.llm.memory.updated(formatRelative(c.updatedAt, now)) }}</time>
            </p>
          </div>
          <div class="flex shrink-0 gap-1">
            <Button variant="outline" size="sm" :aria-label="t.browseLabel(c.name)" @click="browse(c.name)">
              <BookOpen class="h-3.5 w-3.5 sm:mr-1" aria-hidden="true" /><span class="hidden sm:inline">{{ t.browse }}</span>
            </Button>
            <Button variant="ghost" size="icon" class="h-9 w-9 text-destructive" :disabled="pending" :aria-label="t.deleteCollectionLabel(c.name)" :title="t.deleteCollection" @click="deleteCollection(c)">
              <Trash2 class="h-3.5 w-3.5" aria-hidden="true" />
            </Button>
          </div>
        </li>
      </ul>
    </template>
    <ConfirmDialog ref="confirmation" />
  </div>
</template>
