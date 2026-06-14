<script setup lang="ts">
import { ref, watch } from 'vue'
import { useApi } from '@/composables/useApi'
import { Button, Input, Label, Dialog } from '@/components/ui'
import { KeyRound, Trash2, Plus } from 'lucide-vue-next'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const api = useApi()
const names = ref<string[]>([])
const loading = ref(false)
const error = ref('')

const newName = ref('')
const newValue = ref('')

// Kept in script so the literal braces don't confuse the template parser.
const secretSyntax = '{{secret.NAME}}'

async function load() {
  loading.value = true
  error.value = ''
  try {
    names.value = await api.getSecrets()
  } catch (e: any) {
    error.value = e?.response?.data?.error || 'Failed to load secrets'
  } finally {
    loading.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) load()
  }
)

async function add() {
  const name = newName.value.trim()
  if (!name || !newValue.value) return
  try {
    await api.setSecret(name, newValue.value)
    newName.value = ''
    newValue.value = ''
    await load()
  } catch (e: any) {
    error.value = e?.response?.data?.error || 'Failed to save secret'
  }
}

async function remove(name: string) {
  if (!confirm(`Delete secret "${name}"?`)) return
  try {
    await api.deleteSecret(name)
    await load()
  } catch (e: any) {
    error.value = e?.response?.data?.error || 'Failed to delete secret'
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <KeyRound class="h-5 w-5 text-primary" />
          <div>
            <h2 class="text-lg font-semibold">Secrets</h2>
            <p class="text-sm text-muted-foreground">
              Reference these in node fields as <code>{{ secretSyntax }}</code>. Values are write-only.
            </p>
          </div>
        </div>

        <p v-if="error" class="text-sm text-destructive">{{ error }}</p>

        <!-- Existing secrets -->
        <div class="max-h-48 space-y-1 overflow-auto rounded border">
          <p v-if="!loading && !names.length" class="p-3 text-center text-sm text-muted-foreground">
            No secrets yet.
          </p>
          <div
            v-for="name in names"
            :key="name"
            class="flex items-center justify-between px-3 py-2 text-sm"
          >
            <span class="font-mono">{{ name }}</span>
            <Button variant="ghost" size="icon" class="h-7 w-7 text-destructive" @click="remove(name)">
              <Trash2 class="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>

        <!-- Add new -->
        <div class="space-y-2 border-t pt-3">
          <div class="grid grid-cols-2 gap-2">
            <div>
              <Label class="text-xs">Name</Label>
              <Input v-model="newName" placeholder="telegram_token" class="mt-1" />
            </div>
            <div>
              <Label class="text-xs">Value</Label>
              <Input v-model="newValue" type="password" placeholder="secret value" class="mt-1" />
            </div>
          </div>
          <Button size="sm" class="w-full" :disabled="!newName.trim() || !newValue" @click="add">
            <Plus class="mr-2 h-4 w-4" /> Add secret
          </Button>
        </div>

        <div class="flex justify-end">
          <Button variant="outline" @click="close">Done</Button>
        </div>
      </div>
    </template>
  </Dialog>
</template>
