<script setup lang="ts">
// Editor for a JSON-object node setting (e.g. a tool's input schema): JSON text
// checked as it is typed and stored as the parsed object, never as text the
// node would refuse. Invalid text is not applied; the panel blocks "Apply"
// while any JSON field is invalid (update:invalid).
import { ref, watch } from 'vue'
import { Button, Textarea } from '@/components/ui'
import { jsonText, parseJsonObject } from '@/lib/configValues'

const props = defineProps<{ id: string; label: string; modelValue: unknown; required?: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [Record<string, unknown> | undefined]
  'update:invalid': [boolean]
}>()

const text = ref(jsonText(props.modelValue))
const error = ref('')

function setError(message: string) {
  if (message === error.value) return
  error.value = message
  emit('update:invalid', message !== '')
}

// Re-read only when the value changes from outside (another node, discarded
// changes), so the admin's own formatting is kept while typing.
watch(() => props.modelValue, value => {
  const current = parseJsonObject(text.value)
  if (!current.error && JSON.stringify(current.value ?? null) === JSON.stringify(value ?? null)) return
  text.value = jsonText(value)
  setError('')
})

function onInput(value: string) {
  text.value = value
  const parsed = parseJsonObject(value)
  setError(parsed.error ?? '')
  if (!parsed.error) emit('update:modelValue', parsed.value)
}

function format() {
  const parsed = parseJsonObject(text.value)
  if (!parsed.error && parsed.value) text.value = jsonText(parsed.value)
}
</script>

<template>
  <div class="space-y-2">
    <Textarea
      :id="id"
      :model-value="text"
      :rows="10"
      placeholder='{"type": "object", "properties": {}}'
      spellcheck="false"
      autocapitalize="off"
      :aria-required="required"
      :aria-invalid="error !== ''"
      :aria-describedby="error ? `${id}-error` : undefined"
      class="text-xs"
      @update:model-value="onInput"
    />
    <p v-if="error" :id="`${id}-error`" role="alert" class="break-words text-xs text-destructive">
      {{ error }}. Not applied until it is fixed.
    </p>
    <Button variant="outline" size="sm" :aria-label="`Format ${label}`" :disabled="error !== '' || text.trim() === ''" @click="format">
      Format JSON
    </Button>
  </div>
</template>
