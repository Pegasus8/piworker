<script setup lang="ts">
// Editor for a list setting (ConfigProperty type "array", docs/design/llm.md
// §5), such as the classifier's categories or the tools an agent node may
// call: one row per item, added, removed and reordered, stored as a list of
// strings (trimmed, blank rows left out). With suggestions, every row
// offers them; the value stays free text.
import { computed, nextTick, ref, watch } from 'vue'
import { Button, Input } from '@/components/ui'
import { ArrowDown, ArrowUp, Plus, X } from 'lucide-vue-next'
import { messages as m } from '@/i18n'
import { listDuplicates, listFromRows, listRows, moveItem, unreadableList } from '@/lib/configValues'
import type { Suggestion } from '@/lib/suggestions'

const props = defineProps<{ id: string; label: string; modelValue: unknown; suggestions?: Suggestion[]; describedby?: string }>()
const emit = defineEmits<{ 'update:modelValue': [string[] | undefined] }>()

const rows = ref<string[]>(listRows(props.modelValue))
// Blank rows are kept while editing; re-read only when the value changes
// from outside (another node, undo, discarded changes).
watch(() => props.modelValue, value => {
  if (JSON.stringify(value ?? null) !== JSON.stringify(listFromRows(rows.value) ?? null)) rows.value = listRows(value)
})

// Free text saved by hand or by an agent: the node ignores it and deploys refuse it.
const unreadable = computed(() => unreadableList(props.modelValue))
const savedText = computed(() => typeof props.modelValue === 'string' ? props.modelValue : JSON.stringify(props.modelValue))
const duplicates = computed(() => listDuplicates(rows.value))
const listId = computed(() => props.suggestions?.length ? `${props.id}-suggestions` : undefined)
// Reordering and removing are announced: the rows move under the focus.
const announcement = ref('')

const rowId = (index: number) => `${props.id}-item-${index}`
const t = computed(() => m.value.flowTools.list)

function save() { emit('update:modelValue', listFromRows(rows.value)) }
function setRow(index: number, value: string | number) {
  rows.value[index] = String(value)
  save()
}
async function focus(id: string) {
  await nextTick()
  document.getElementById(id)?.focus()
}
function add() {
  rows.value.push('')
  void focus(rowId(rows.value.length - 1))
}
function remove(index: number) {
  const [item] = rows.value.splice(index, 1)
  save()
  announcement.value = t.value.removed(item.trim() || t.value.item(props.label, index + 1))
  void focus(rows.value.length ? rowId(Math.min(index, rows.value.length - 1)) : `${props.id}-add`)
}
function move(index: number, delta: -1 | 1) {
  const to = index + delta
  if (to < 0 || to >= rows.value.length) return
  rows.value = moveItem(rows.value, index, to)
  save()
  announcement.value = t.value.moved(rows.value[to].trim() || t.value.item(props.label, to + 1), to + 1, rows.value.length)
  // The focus stays on the same button of the moved row, or on its text
  // once it reaches an end (where that button is disabled).
  const atEnd = delta < 0 ? to === 0 : to === rows.value.length - 1
  void focus(atEnd ? rowId(to) : `${props.id}-${delta < 0 ? 'up' : 'down'}-${to}`)
}
// Enter moves to the next row, adding one after the last: items are typed one after another.
function next(index: number) {
  if (index === rows.value.length - 1) add()
  else void focus(rowId(index + 1))
}
</script>

<template>
  <div :id="id" role="group" :aria-label="label" :aria-describedby="describedby" class="space-y-2">
    <div v-if="unreadable" role="alert" class="rounded-md border border-destructive/50 p-2 text-xs text-destructive">
      {{ t.unreadable }}
      <code class="block break-all">{{ savedText }}</code>
      {{ t.addBelow }}
      <button type="button" class="underline" @click="emit('update:modelValue', undefined)">{{ t.removeSaved }}</button>.
    </div>
    <ol v-if="rows.length" class="space-y-2">
      <li v-for="(row, index) in rows" :key="index" class="flex items-center gap-1">
        <Input
          :id="rowId(index)" :model-value="row" :list="listId" autocomplete="off" spellcheck="false"
          :aria-label="t.item(label, index + 1)" class="min-w-0 flex-1"
          @update:model-value="v => setRow(index, v)" @keydown.enter.prevent="next(index)"
        />
        <Button :id="`${id}-up-${index}`" variant="ghost" size="icon" class="h-9 w-9 shrink-0" :disabled="index === 0" :aria-label="t.moveUp(label, index + 1)" :title="t.moveUp(label, index + 1)" @click="move(index, -1)">
          <ArrowUp class="h-4 w-4" aria-hidden="true" />
        </Button>
        <Button :id="`${id}-down-${index}`" variant="ghost" size="icon" class="h-9 w-9 shrink-0" :disabled="index === rows.length - 1" :aria-label="t.moveDown(label, index + 1)" :title="t.moveDown(label, index + 1)" @click="move(index, 1)">
          <ArrowDown class="h-4 w-4" aria-hidden="true" />
        </Button>
        <Button variant="ghost" size="icon" class="h-9 w-9 shrink-0" :aria-label="t.remove(label, index + 1)" :title="t.remove(label, index + 1)" @click="remove(index)">
          <X class="h-4 w-4" aria-hidden="true" />
        </Button>
      </li>
    </ol>
    <p v-else class="text-xs text-muted-foreground">{{ t.empty }}</p>
    <p v-if="duplicates.length" role="status" class="text-xs text-destructive">{{ t.duplicates(duplicates) }}</p>
    <Button :id="`${id}-add`" variant="outline" size="sm" @click="add">
      <Plus class="mr-2 h-4 w-4" aria-hidden="true" />
      {{ t.add }}
    </Button>
    <datalist v-if="listId" :id="listId">
      <option v-for="s in suggestions" :key="s.value" :value="s.value" :label="s.label" />
    </datalist>
    <p class="sr-only" aria-live="polite">{{ announcement }}</p>
  </div>
</template>
