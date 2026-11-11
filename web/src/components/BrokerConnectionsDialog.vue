<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Button, Input, Label, Dialog, Badge } from '@/components/ui'
import { Cable, Pencil, Trash2, Plus, CheckCircle2, AlertCircle, Loader2 } from 'lucide-vue-next'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'
import { useZigbeeStore, type ConnectionTestResult } from '@/stores/zigbee'
import { apiErrorMessage } from '@/stores/agent'
import { DEFAULT_BASE_TOPIC, connectionFormError, type BrokerConnection, type ConnectionForm } from '@/lib/zigbee'

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
    error.value = apiErrorMessage(e, 'Could not load the connections. Check your connection and try again.')
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
  const ok = await run(() => store.saveConnection(form.value), `Connection “${name}” saved.`, e => apiErrorMessage(e, 'Could not save the connection. Try again.'))
  if (ok) {
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
    testResult.value = { ok: false, error: apiErrorMessage(e, 'Could not test the connection.') }
  } finally {
    testing.value = false
  }
}

async function remove(name: string) {
  if (pending.value || !await confirmation.value?.ask('Delete connection?', `Delete “${name}”? Flows whose Zigbee nodes use it will not deploy until you define it again. This cannot be undone.`, 'Delete connection')) return
  const ok = await run(() => store.deleteConnection(name), 'Connection deleted.', e => apiErrorMessage(e, 'Could not delete the connection. Try again.'))
  if (ok) {
    emit('changed')
    if (editing.value?.name === name) reset()
  }
}
</script>

<template>
  <Dialog title="Broker connections" class="max-w-2xl" :open="open" @update:open="(v) => emit('update:open', v)">
    <template #default="{ close }">
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <Cable class="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <div>
            <h2 class="text-lg font-semibold">Broker connections</h2>
            <p class="text-sm text-muted-foreground">
              Zigbee nodes reach Zigbee2MQTT through an MQTT broker named here. Passwords are write-only.
            </p>
          </div>
        </div>

        <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
        <OperationFeedback :message="feedback" @dismiss="feedback = null" />

        <!-- Existing connections -->
        <ul class="max-h-60 divide-y overflow-auto rounded border" aria-label="Connections">
          <li v-if="loading && !store.connections.length" class="p-3 text-center text-sm text-muted-foreground">Loading…</li>
          <li v-else-if="!store.connections.length" class="p-3 text-center text-sm text-muted-foreground">No connections yet.</li>
          <li v-for="item in store.connections" :key="item.name" class="flex items-start justify-between gap-2 px-3 py-2 text-sm">
            <div class="min-w-0 space-y-0.5">
              <p class="break-all font-mono font-medium">{{ item.name }}</p>
              <p class="break-all text-xs text-muted-foreground">
                {{ item.broker }}<template v-if="item.username"> · user {{ item.username }}</template> · topic {{ item.baseTopic || DEFAULT_BASE_TOPIC }}
              </p>
              <Badge v-if="item.hasPassword" variant="secondary" class="text-[10px]">password set</Badge>
            </div>
            <div class="flex shrink-0 gap-1">
              <Button variant="ghost" size="icon" :disabled="pending" :aria-label="`Edit connection ${item.name}`" title="Edit" @click="edit(item)">
                <Pencil class="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon" class="text-destructive" :disabled="pending" :aria-label="`Delete connection ${item.name}`" title="Delete" @click="remove(item.name)">
                <Trash2 class="h-3.5 w-3.5" />
              </Button>
            </div>
          </li>
        </ul>

        <!-- Add or edit -->
        <form class="space-y-3 border-t pt-3" novalidate @submit.prevent="save">
          <h3 class="text-sm font-semibold">{{ editing ? `Edit “${editing.name}”` : 'New connection' }}</h3>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <Label for="broker-conn-name" class="text-xs">Name</Label>
              <Input id="broker-conn-name" v-model="form.name" :disabled="pending || !!editing" placeholder="home" class="mt-1" autocomplete="off"
                :aria-describedby="editing ? 'broker-conn-name-help' : undefined" />
              <p v-if="editing" id="broker-conn-name-help" class="mt-1 text-xs text-muted-foreground">Nodes refer to the name, so it cannot change.</p>
            </div>
            <div>
              <Label for="broker-conn-broker" class="text-xs">Broker</Label>
              <Input id="broker-conn-broker" v-model="form.broker" :disabled="pending" :placeholder="brokerExample" class="mt-1" autocomplete="off" />
            </div>
            <div>
              <Label for="broker-conn-user" class="text-xs">Username</Label>
              <Input id="broker-conn-user" v-model="form.username" :disabled="pending" placeholder="optional" class="mt-1" autocomplete="off" />
            </div>
            <div>
              <Label for="broker-conn-password" class="text-xs">Password</Label>
              <Input id="broker-conn-password" v-model="form.password" type="password" :disabled="pending || form.clearPassword" class="mt-1" autocomplete="new-password"
                :placeholder="editing?.hasPassword ? 'Empty keeps the stored password' : 'optional'" />
              <label v-if="editing?.hasPassword" class="mt-1 flex items-center gap-2 text-xs">
                <input v-model="form.clearPassword" type="checkbox" :disabled="pending" class="h-3.5 w-3.5" />
                Clear the stored password
              </label>
            </div>
            <div class="sm:col-span-2">
              <Label for="broker-conn-topic" class="text-xs">Base topic</Label>
              <Input id="broker-conn-topic" v-model="form.baseTopic" :disabled="pending" :placeholder="DEFAULT_BASE_TOPIC" class="mt-1" autocomplete="off"
                aria-describedby="broker-conn-topic-help" />
              <p id="broker-conn-topic-help" class="mt-1 text-xs text-muted-foreground">Zigbee2MQTT's <code>mqtt.base_topic</code>; empty uses {{ DEFAULT_BASE_TOPIC }}. Username and password may hold <code>{{ secretSyntax }}</code>.</p>
            </div>
          </div>

          <p v-if="shownError" role="alert" class="text-sm text-destructive">{{ shownError }}</p>

          <!-- The test's result: announced when it arrives. -->
          <div aria-live="polite">
            <p v-if="testing" class="flex items-center gap-1.5 text-sm text-muted-foreground"><Loader2 class="h-4 w-4 animate-spin" aria-hidden="true" />Connecting to the broker…</p>
            <p v-else-if="testResult?.ok" class="flex items-center gap-1.5 text-sm text-primary"><CheckCircle2 class="h-4 w-4" aria-hidden="true" />Connected: the broker accepted these settings.</p>
            <p v-else-if="testResult" class="flex items-start gap-1.5 break-words text-sm text-destructive"><AlertCircle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><span>Could not connect: {{ testResult.error }}</span></p>
          </div>

          <div class="flex flex-wrap justify-end gap-2">
            <Button v-if="editing" type="button" variant="ghost" size="sm" :disabled="pending" @click="reset">Cancel edit</Button>
            <Button type="button" variant="outline" size="sm" :disabled="pending || testing" @click="test">Test connection</Button>
            <Button type="submit" size="sm" :disabled="pending">
              <Plus v-if="!editing" class="mr-2 h-4 w-4" aria-hidden="true" />
              {{ editing ? 'Save connection' : 'Add connection' }}
            </Button>
          </div>
        </form>

        <div class="flex justify-end border-t pt-3">
          <Button variant="outline" @click="close">Done</Button>
        </div>
      </div>
      <ConfirmDialog ref="confirmation" />
    </template>
  </Dialog>
</template>
