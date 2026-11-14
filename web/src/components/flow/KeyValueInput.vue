<script setup lang="ts">
// Editor for an object-typed node setting (e.g. HTTP headers): name/value rows
// stored as an object, never as free text the node would refuse.
import { computed, ref, watch } from 'vue'
import { Button, Input } from '@/components/ui'
import { Plus, X } from 'lucide-vue-next'
import { messages as m } from '@/i18n'
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
      {{ m.flowTools.keyValue.unreadable }}
      <code class="block break-all">{{ String(modelValue) }}</code>
      {{ m.flowTools.keyValue.addBelow }}
      <button type="button" class="underline" @click="emit('update:modelValue', undefined)">{{ m.flowTools.keyValue.removeSaved }}</button>.
    </div>
    <div v-for="(row, index) in rows" :key="index" class="flex items-center gap-2">
      <Input :model-value="row.key" :placeholder="m.flowTools.keyValue.name" :aria-label="m.flowTools.keyValue.nameLabel(label, index + 1)" class="w-2/5" @update:model-value="v => setRow(index, 'key', v)" />
      <Input :model-value="row.value" :placeholder="m.flowTools.keyValue.value" :aria-label="m.flowTools.keyValue.valueLabel(label, index + 1)" class="flex-1" @update:model-value="v => setRow(index, 'value', v)" />
      <Button variant="ghost" size="icon" class="flex-shrink-0" :aria-label="m.flowTools.keyValue.removeLabel(label, index + 1)" @click="remove(index)">
        <X class="h-4 w-4" />
      </Button>
    </div>
    <Button variant="outline" size="sm" @click="add">
      <Plus class="mr-2 h-4 w-4" />
      {{ m.flowTools.keyValue.add }}
    </Button>
  </div>
</template>
