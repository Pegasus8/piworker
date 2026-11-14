<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { durationUnits, joinDuration, splitDuration, type DurationUnit } from '@/lib/duration'
import { messages as m } from '@/i18n'

// A time setting typed in a friendly unit and stored in the node's unit.
const props = defineProps<{ id: string; modelValue: unknown; baseUnit: string; required?: boolean; placeholder?: string; integer?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: number | undefined] }>()
const units = computed(() => durationUnits(props.baseUnit))
const split = () => splitDuration(typeof props.modelValue === 'number' ? props.modelValue : undefined, props.baseUnit)
const amount = ref<number | undefined>(split().amount)
const unit = ref<DurationUnit>(split().unit)
// Follow outside changes (undo, another node) without fighting the typing.
watch(() => props.modelValue, value => {
  if (value === joinDuration(amount.value, unit.value, props.baseUnit)) return
  const next = split()
  amount.value = next.amount
  unit.value = next.unit
})
function emitValue() {
  const value = joinDuration(amount.value, unit.value, props.baseUnit)
  emit('update:modelValue', value === undefined ? undefined : props.integer ? Math.round(value) : value)
}
function onAmount(event: Event) {
  const raw = (event.target as HTMLInputElement).value
  amount.value = raw.trim() === '' || !Number.isFinite(Number(raw)) ? undefined : Number(raw)
  emitValue()
}
function onUnit(event: Event) {
  unit.value = (event.target as HTMLSelectElement).value as DurationUnit
  emitValue()
}
</script>

<template>
  <div class="flex gap-2">
    <input :id="id" type="number" inputmode="decimal" :aria-required="required" :value="amount ?? ''" :placeholder="placeholder"
      class="flex h-10 min-w-0 flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      @input="onAmount" />
    <select :value="unit" :aria-label="m.editor.durationUnit" class="h-10 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" @change="onUnit">
      <option v-for="u in units" :key="u" :value="u">{{ m.editor.units[u] }}</option>
    </select>
  </div>
</template>
