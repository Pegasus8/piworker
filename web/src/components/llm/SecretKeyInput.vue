<script setup lang="ts">
// An API key field (write-only on the server): a typed key is masked unless
// shown on purpose; a key made only of {{secret.NAME}} references holds
// nothing secret, so it is shown as typed. The secrets defined so far are
// offered as references, as the node settings' secret fields offer them.
import { computed, ref, watch } from 'vue'
import { Eye, EyeOff } from 'lucide-vue-next'
import { Button, Input } from '@/components/ui'
import { messages as m } from '@/i18n'
import { referenceOptions } from '@/lib/references'
import { isSecretReference } from './connections'

const props = defineProps<{ id: string; modelValue: string; secrets: string[]; disabled?: boolean; placeholder?: string; describedby?: string }>()
const emit = defineEmits<{ 'update:modelValue': [string] }>()

const revealed = ref(false)
const reference = computed(() => isSecretReference(props.modelValue))
// Each secret as its {{secret.NAME}} reference.
const options = computed(() => referenceOptions('secret', [], props.secrets))
// Cleared (saved, or another connection edited): masked again.
watch(() => props.modelValue, value => { if (!value) revealed.value = false })

function useSecret(event: Event) {
  const select = event.target as HTMLSelectElement
  if (select.value) emit('update:modelValue', select.value)
  select.value = ''
}
</script>

<template>
  <div class="flex flex-wrap gap-2">
    <div class="flex min-w-0 flex-1 basis-56 gap-1">
      <Input :id="id" :model-value="modelValue" :type="reference || revealed ? 'text' : 'password'" :disabled="disabled" :placeholder="placeholder"
        autocomplete="new-password" spellcheck="false" :aria-describedby="describedby" class="min-w-0 font-mono"
        @update:model-value="v => emit('update:modelValue', String(v))" />
      <Button v-if="modelValue && !reference" variant="outline" size="icon" class="shrink-0" :disabled="disabled" :aria-pressed="revealed"
        :aria-label="revealed ? m.llm.connections.hideKey : m.llm.connections.showKey" :title="revealed ? m.llm.connections.hideKey : m.llm.connections.showKey"
        @click="revealed = !revealed">
        <EyeOff v-if="revealed" class="h-4 w-4" aria-hidden="true" />
        <Eye v-else class="h-4 w-4" aria-hidden="true" />
      </Button>
    </div>
    <select v-if="secrets.length" :aria-label="m.llm.connections.useSecret" :disabled="disabled" value=""
      class="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 sm:w-48"
      @change="useSecret">
      <option value="" disabled>{{ m.llm.connections.chooseSecret }}</option>
      <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
    </select>
  </div>
</template>
