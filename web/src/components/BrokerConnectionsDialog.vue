<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Button, Input, Label, Dialog, Badge } from '@/components/ui'
import { Cable, Pencil, Trash2, Plus, CheckCircle2, AlertCircle, Loader2 } from 'lucide-vue-next'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useZigbeeStore, type ConnectionTestResult } from '@/stores/zigbee'
import { apiErrorMessage } from '@/stores/agent'
import { messages as m } from '@/i18n'
import { DEFAULT_BASE_TOPIC, connectionFormError, connectionSavedText, type BrokerConnection, type ConnectionForm, type ConnectionRestarts } from '@/lib/zigbee'

const props = defineProps<{ open: boolean }>()
// changed: a connection was saved or deleted.
const emit = defineEmits<{ 'update:open': [boolean]; changed: [] }>()

const store = useZigbeeStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const loading = ref(false)
const error = ref('')

const blank = (): ConnectionForm => ({ name: '', broker: '', username: '', password: '', clearPassword: false, baseTopic: '' })
const form = ref<ConnectionForm>(blank())
// The connection being edited (its name cannot change), or null for a new one.
const editing = ref<BrokerConnection | null>(null)
const attempted = ref(false)
const formError = computed(() => connectionFormError(form.value))
const shownError = computed(() => attempted.value ? formError.value : '')
const testing = ref(false)
const testResult = ref<ConnectionTestResult | null>(null)
// Kept in script so the literal braces don't confuse the template parser.
const secretSyntax = '{{secret.NAME}}'
const brokerExample = 'tcp://192.168.1.10:1883'

async function load() {
  loading.value = true
  error.value = ''
  try {
    await store.fetchConnections()
  } catch (e) {
    error.value = apiErrorMessage(e, m.value.zigbee.dialog.loadFailed)
  } finally {
    loading.value = false
  }
}

function reset() {
  form.value = blank()
  editing.value = null
  attempted.value = false
  testResult.value = null
}

watch(() => props.open, open => {
  if (open) {
    reset()
    feedback.value = null
    void load()
  }
}, { immediate: true })

// Editing never shows the stored password: empty keeps it.
function edit(item: BrokerConnection) {
  editing.value = item
  form.value = { name: item.name, broker: item.broker, username: item.username, password: '', clearPassword: false, baseTopic: item.baseTopic }
  attempted.value = false
  testResult.value = null
  feedback.value = null
}

// Settings changed since the last test: its result no longer applies.
watch(form, () => { testResult.value = null }, { deep: true })

async function save() {
  attempted.value = true
  if (formError.value) return
  const name = form.value.name.trim()
  let restarts: ConnectionRestarts = { restarted: [], failed: [] }
  const ok = await run(async () => { restarts = await store.saveConnection(form.value) }, m.value.zigbee.dialog.saved(name), e => apiErrorMessage(e, m.value.zigbee.dialog.saveFailed))
  if (ok) {
    // Running flows that use the connection restart with it (ADR-0029).
    feedback.value = connectionSavedText(name, restarts)
    emit('changed')
    reset()
  }
}

async function test() {
  attempted.value = true
  if (formError.value || testing.value) return
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await store.testConnection(form.value)
  } catch (e) {
    testResult.value = { ok: false, error: apiErrorMessage(e, m.value.zigbee.dialog.testFailed) }
  } finally {
    testing.value = false
  }
}

async function remove(name: string) {
  if (pending.value || !await confirmation.value?.ask(m.value.zigbee.dialog.deleteTitle, m.value.zigbee.dialog.deleteText(name), m.value.zigbee.dialog.deleteConfirm)) return
  const ok = await run(() => store.deleteConnection(name), m.value.zigbee.dialog.deleted, e => apiErrorMessage(e, m.value.zigbee.dialog.deleteFailed))
  if (ok) {
    emit('changed')
    if (editing.value?.name === name) reset()
  }
}
</script>

<template>
  <Dialog :title="m.zigbee.dialog.title" class="max-w-2xl" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <Cable class="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div>
            <h2 class="text-lg font-semibold">{{ m.zigbee.dialog.title }}</h2>
            <p class="text-sm text-muted-foreground">{{ m.zigbee.dialog.intro }}</p>
          </div>
        </div>

        <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
        <OperationFeedback :message="feedback" @dismiss="feedback = null" />

        <!-- Existing connections -->
        <ul class="max-h-60 divide-y overflow-auto rounded border" :aria-label="m.zigbee.dialog.listLabel">
          <li v-if="loading && !store.connections.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.common.loading }}</li>
          <li v-else-if="!store.connections.length" class="p-3 text-center text-sm text-muted-foreground">{{ m.zigbee.dialog.empty }}</li>
          <li v-for="item in store.connections" :key="item.name" class="flex items-start justify-between gap-2 px-3 py-2 text-sm">
            <div class="min-w-0 space-y-0.5">
              <p class="break-all font-mono font-medium">{{ item.name }}</p>
              <p class="break-all text-xs text-muted-foreground">
                {{ item.broker }}<template v-if="item.username"> · {{ m.zigbee.dialog.user(item.username) }}</template> · {{ m.zigbee.dialog.topic(item.baseTopic || DEFAULT_BASE_TOPIC) }}
              </p>
              <Badge v-if="item.hasPassword" variant="secondary" class="text-[10px]">{{ m.zigbee.dialog.passwordSet }}</Badge>
            </div>
            <div class="flex shrink-0 gap-1">
              <Button variant="ghost" size="icon" :disabled="pending" :aria-label="m.zigbee.dialog.editLabel(item.name)" :title="m.zigbee.dialog.edit" @click="edit(item)">
                <Pencil class="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon" class="text-destructive" :disabled="pending" :aria-label="m.zigbee.dialog.deleteLabel(item.name)" :title="m.common.delete" @click="remove(item.name)">
                <Trash2 class="h-3.5 w-3.5" />
              </Button>
            </div>
          </li>
        </ul>

        <!-- Add or edit -->
        <form class="space-y-3 border-t pt-3" novalidate @submit.prevent="save">
          <h3 class="text-sm font-semibold">{{ editing ? m.zigbee.dialog.editTitle(editing.name) : m.zigbee.dialog.newTitle }}</h3>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <Label for="broker-conn-name" class="text-xs">{{ m.zigbee.dialog.name }}</Label>
              <Input id="broker-conn-name" v-model="form.name" :disabled="pending || !!editing" placeholder="home" class="mt-1" autocomplete="off"
                :aria-describedby="editing ? 'broker-conn-name-help' : undefined" />
              <p v-if="editing" id="broker-conn-name-help" class="mt-1 text-xs text-muted-foreground">{{ m.zigbee.dialog.nameFixed }}</p>
            </div>
            <div>
              <Label for="broker-conn-broker" class="text-xs">{{ m.zigbee.dialog.broker }}</Label>
              <Input id="broker-conn-broker" v-model="form.broker" :disabled="pending" :placeholder="brokerExample" class="mt-1" autocomplete="off" />
            </div>
            <div>
              <Label for="broker-conn-user" class="text-xs">{{ m.zigbee.dialog.username }}</Label>
              <Input id="broker-conn-user" v-model="form.username" :disabled="pending" :placeholder="m.zigbee.dialog.optional" class="mt-1" autocomplete="off" />
            </div>
            <div>
              <Label for="broker-conn-password" class="text-xs">{{ m.zigbee.dialog.password }}</Label>
              <Input id="broker-conn-password" v-model="form.password" type="password" :disabled="pending || form.clearPassword" class="mt-1" autocomplete="new-password"
                :placeholder="editing?.hasPassword ? m.zigbee.dialog.keepPassword : m.zigbee.dialog.optional" />
              <label v-if="editing?.hasPassword" class="mt-1 flex items-center gap-2 text-xs">
                <input v-model="form.clearPassword" type="checkbox" :disabled="pending" class="h-3.5 w-3.5" />
                {{ m.zigbee.dialog.clearPassword }}
              </label>
            </div>
            <div class="sm:col-span-2">
              <Label for="broker-conn-topic" class="text-xs">{{ m.zigbee.dialog.baseTopic }}</Label>
              <Input id="broker-conn-topic" v-model="form.baseTopic" :disabled="pending" :placeholder="DEFAULT_BASE_TOPIC" class="mt-1" autocomplete="off"
                aria-describedby="broker-conn-topic-help" />
              <p id="broker-conn-topic-help" class="mt-1 text-xs text-muted-foreground">{{ m.zigbee.dialog.topicHelp(DEFAULT_BASE_TOPIC)[0] }}<code>mqtt.base_topic</code>{{ m.zigbee.dialog.topicHelp(DEFAULT_BASE_TOPIC)[1] }}<code>{{ secretSyntax }}</code>{{ m.zigbee.dialog.topicHelp(DEFAULT_BASE_TOPIC)[2] }}</p>
            </div>
          </div>

          <p v-if="shownError" role="alert" class="text-sm text-destructive">{{ shownError }}</p>

          <!-- The test's result: announced when it arrives. -->
          <div aria-live="polite">
            <p v-if="testing" class="flex items-center gap-1.5 text-sm text-muted-foreground"><Loader2 class="h-4 w-4 animate-spin" aria-hidden="true" />{{ m.zigbee.dialog.connecting }}</p>
            <p v-else-if="testResult?.ok" class="flex items-center gap-1.5 text-sm text-primary"><CheckCircle2 class="h-4 w-4" aria-hidden="true" />{{ m.zigbee.dialog.connected }}</p>
            <p v-else-if="testResult" class="flex items-start gap-1.5 break-words text-sm text-destructive"><AlertCircle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><span>{{ m.zigbee.dialog.couldNotConnect(testResult.error ?? '') }}</span></p>
          </div>

          <div class="flex flex-wrap justify-end gap-2">
            <Button v-if="editing" type="button" variant="ghost" size="sm" :disabled="pending" @click="reset">{{ m.zigbee.dialog.cancelEdit }}</Button>
            <Button type="button" variant="outline" size="sm" :disabled="pending || testing" @click="test">{{ m.zigbee.dialog.test }}</Button>
            <Button type="submit" size="sm" :disabled="pending">
              <Plus v-if="!editing" class="mr-2 h-4 w-4" aria-hidden="true" />
              {{ editing ? m.zigbee.dialog.save : m.zigbee.dialog.add }}
            </Button>
          </div>
        </form>

        <div class="flex justify-end border-t pt-3">
          <Button variant="outline" @click="close">{{ m.common.done }}</Button>
        </div>
      </div>
      <ConfirmDialog ref="confirmation" />
    </template>
  </Dialog>
</template>
