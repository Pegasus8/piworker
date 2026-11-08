<script setup lang="ts">
// Editor for an object-typed node setting (e.g. HTTP headers): name/value rows
// stored as an object, never as free text the node would refuse.
import { computed, ref, watch } from 'vue'
import { Button, Input } from '@/components/ui'
import { Plus, X } from 'lucide-vue-next'
import { isPlainObject, objectFromRows, objectRows, type ObjectRow } from '@/lib/configValues'

const props = defineProps<{ id: string; label: string; modelValue: unknown }>()
const emit = defineEmits<{ 'update:modelValue': [Record<string, string> | undefined] }>()

const rows = ref<ObjectRow[]>(objectRows(props.modelValue))
// Rows without a name are kept while editing; re-read only when the value
// changes from outside (another node, discarded changes).
watch(() => props.modelValue, value => {
  if (JSON.stringify(value ?? null) !== JSON.stringify(objectFromRows(rows.value) ?? null)) rows.value = objectRows(value)
})

// Free text saved by older editors: the node ignores it and deploys refuse it.
const unreadable = computed(() => props.modelValue != null && props.modelValue !== '' && !isPlainObject(props.modelValue))

function save() { emit('update:modelValue', objectFromRows(rows.value)) }
function setRow(index: number, part: keyof ObjectRow, value: string | number) {
  rows.value[index][part] = String(value)
  save()
}
function add() { rows.value.push({ key: '', value: '' }) }
function remove(index: number) {
  rows.value.splice(index, 1)
  save()
}
</script>

<template>
  <div :id="id" role="group" :aria-label="label" class="space-y-2">
    <div v-if="unreadable" role="alert" class="rounded-md border border-destructive/50 p-2 text-xs text-destructive">
      The saved value is text, not name/value pairs, so it is ignored and deploying fails:
      <code class="block break-all">{{ String(modelValue) }}</code>
      Add each entry below, or
      <button type="button" class="underline" @click="emit('update:modelValue', undefined)">remove the saved value</button>.
    </div>
    <div v-for="(row, index) in rows" :key="index" class="flex items-center gap-2">
      <Input :model-value="row.key" placeholder="Name" :aria-label="`${label} name ${index + 1}`" class="w-2/5" @update:model-value="v => setRow(index, 'key', v)" />
      <Input :model-value="row.value" placeholder="Value" :aria-label="`${label} value ${index + 1}`" class="flex-1" @update:model-value="v => setRow(index, 'value', v)" />
      <Button variant="ghost" size="icon" class="flex-shrink-0" :aria-label="`Remove ${label} entry ${index + 1}`" @click="remove(index)">
        <X class="h-4 w-4" />
      </Button>
    </div>
    <Button variant="outline" size="sm" @click="add">
      <Plus class="mr-2 h-4 w-4" />
      Add entry
    </Button>
  </div>
</template>
