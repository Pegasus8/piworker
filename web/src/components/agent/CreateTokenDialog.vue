<script setup lang="ts">
// Creates an agent token after password step-up. Tool titles and descriptions
// come from flows (and may have been drafted by an agent): text only.
import { computed, ref, watch } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import { Badge, Button, Dialog, Input, Label, Select } from '@/components/ui'
import { useAgentStore, apiErrorMessage } from '@/stores/agent'
import { EXPIRY_OPTIONS, scopeOptions, toolKey, type AgentScope, type AgentTool, type CreatedAgentToken } from '@/lib/agentAccess'
import { messages as m } from '@/i18n'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; created: [CreatedAgentToken] }>()
const agent = useAgentStore()

const defaultDays = EXPIRY_OPTIONS.find(o => o.default)!.days
const name = ref('')
const scopes = ref<AgentScope[]>(['call'])
const privileged = ref(false)
const allTools = ref(false)
const selected = ref<string[]>([])
const days = ref(String(defaultDays))
const password = ref('')
const busy = ref(false)
const error = ref('')

const options = computed(() => scopeOptions(agent.settings))
const calls = computed(() => scopes.value.includes('call'))
const nameBytes = computed(() => new TextEncoder().encode(name.value.trim()).length)
const valid = computed(() => nameBytes.value > 0 && nameBytes.value <= 64 && scopes.value.length > 0 && !!password.value)
const expiryOptions = computed(() => EXPIRY_OPTIONS.map(o => ({ value: String(o.days), label: o.label + (o.default ? m.value.agents.create.defaultSuffix : '') })))
const key = (t: AgentTool) => toolKey(t.flowId, t.triggerNodeId)

function reset() {
  name.value = ''; scopes.value = ['call']; privileged.value = false; allTools.value = false
  selected.value = []; days.value = String(defaultDays); password.value = ''; error.value = ''
}

watch(() => props.open, open => {
  if (!open) { password.value = ''; return }
  reset()
  void agent.fetchTools()
  void agent.fetchSettings().catch(() => {})
})
// Unavailable scopes are never sent; a privileged tool needs the flag.
watch(options, opts => {
  scopes.value = scopes.value.filter(s => opts.find(o => o.scope === s)?.available)
})
watch(privileged, on => {
  if (!on) selected.value = selected.value.filter(k => agent.tools.find(t => key(t) === k)?.risk !== 'privileged')
})

function close() { emit('update:open', false) }

async function create() {
  if (!valid.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const created = await agent.createToken({
      name: name.value,
      scopes: scopes.value,
      privileged: privileged.value,
      allTools: allTools.value,
      tools: agent.tools.filter(t => selected.value.includes(key(t))),
      expiresInDays: Number(days.value),
      password: password.value
    })
    password.value = ''
    emit('created', created)
    close()
  } catch (e) {
    error.value = apiErrorMessage(e, m.value.agents.create.failed)
  } finally {
    busy.value = false
  }
}

const riskVariant = (risk: string) => risk === 'privileged' ? 'destructive' : risk === 'external' ? 'secondary' : 'outline'
</script>

<template>
  <Dialog :title="m.agents.create.title" class="max-w-2xl" :open="open" @update:open="v => emit('update:open', v)">
    <form class="space-y-5" @submit.prevent="create">
      <div>
        <h2 class="text-lg font-semibold">{{ m.agents.create.title }}</h2>
        <p class="text-sm text-muted-foreground">{{ m.agents.create.intro }}</p>
      </div>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>

      <div class="space-y-2">
        <Label for="token-name">{{ m.agents.create.name }}</Label>
        <Input id="token-name" v-model="name" :placeholder="m.agents.create.namePlaceholder" :disabled="busy" />
        <p v-if="nameBytes > 64" class="text-xs text-destructive">{{ m.agents.create.nameTooLong }}</p>
      </div>

      <fieldset class="space-y-3">
        <legend class="mb-2 text-sm font-medium">{{ m.agents.create.scopes }}</legend>
        <div v-for="o in options" :key="o.scope" class="flex items-start gap-3">
          <input :id="`scope-${o.scope}`" v-model="scopes" type="checkbox" :value="o.scope" class="mt-1 h-4 w-4 accent-primary"
            :disabled="busy || !o.available" :aria-describedby="`scope-${o.scope}-help`" />
          <div class="min-w-0">
            <label :for="`scope-${o.scope}`" class="text-sm font-medium" :class="!o.available && 'text-muted-foreground'">{{ o.label }}</label>
            <p :id="`scope-${o.scope}-help`" class="text-xs text-muted-foreground">{{ o.description }}</p>
            <p v-if="!o.available" class="text-xs text-amber-600 dark:text-amber-400">{{ o.reason }}</p>
          </div>
        </div>
      </fieldset>

      <div v-if="calls" class="space-y-3 rounded-lg border p-3">
        <div class="flex items-start gap-3">
          <input id="token-privileged" v-model="privileged" type="checkbox" class="mt-1 h-4 w-4 accent-primary" :disabled="busy" aria-describedby="token-privileged-help" />
          <div>
            <label for="token-privileged" class="text-sm font-medium">{{ m.agents.create.allowPrivileged }}</label>
            <p id="token-privileged-help" class="text-xs text-muted-foreground">{{ m.agents.create.allowPrivilegedHelp }}</p>
          </div>
        </div>
        <p v-if="privileged" class="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive">
          <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          {{ m.agents.create.privilegedWarning }}
        </p>

        <fieldset class="space-y-2">
          <legend class="mb-1 text-sm font-medium">{{ m.agents.create.toolsLegend }}</legend>
          <label class="flex items-center gap-2 text-sm"><input v-model="allTools" type="radio" :value="false" name="token-tools" class="accent-primary" :disabled="busy" />{{ m.agents.create.selected }}</label>
          <label class="flex items-center gap-2 text-sm"><input v-model="allTools" type="radio" :value="true" name="token-tools" class="accent-primary" :disabled="busy" />{{ m.agents.create.allPublished }}</label>
        </fieldset>
        <p v-if="allTools" class="text-xs text-muted-foreground">
          {{ m.agents.create.allNote }}
        </p>
        <template v-else>
          <p v-if="agent.toolsError" role="alert" class="text-sm text-destructive">{{ agent.toolsError }}</p>
          <p v-else-if="!agent.tools.length" class="rounded-md bg-muted p-3 text-sm text-muted-foreground">
            {{ m.agents.create.noTools }}
          </p>
          <ul v-else class="max-h-64 space-y-2 overflow-y-auto pr-1">
            <li v-for="(t, i) in agent.tools" :key="key(t)" class="flex items-start gap-3 rounded-md border p-2">
              <input :id="`tool-${i}`" v-model="selected" type="checkbox" :value="key(t)" class="mt-1 h-4 w-4 accent-primary"
                :disabled="busy || (t.risk === 'privileged' && !privileged)" :aria-describedby="`tool-${i}-help`" />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <label :for="`tool-${i}`" class="break-all font-mono text-sm font-medium">{{ t.toolName }}</label>
                  <Badge :variant="riskVariant(t.risk)">{{ m.agents.access.risk[t.risk] ?? t.risk }}</Badge>
                  <Badge v-if="t.confirm === 'admin'" variant="outline">{{ m.agents.create.asksFirst }}</Badge>
                </div>
                <div :id="`tool-${i}-help`" class="text-xs text-muted-foreground">
                  <p class="break-words">{{ m.agents.create.toolFlow(t.title, t.flowName) }}</p>
                  <p class="line-clamp-2 whitespace-pre-line break-words">{{ t.description }}</p>
                  <p v-if="t.risk === 'privileged' && !privileged" class="text-amber-600 dark:text-amber-400">{{ m.agents.create.needsPrivileged }}</p>
                </div>
              </div>
            </li>
          </ul>
        </template>
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div class="space-y-2">
          <Label for="token-expiry">{{ m.agents.create.expiresAfter }}</Label>
          <Select id="token-expiry" v-model="days" :options="expiryOptions" :disabled="busy" />
        </div>
        <div class="space-y-2">
          <Label for="token-password">{{ m.agents.yourPassword }}</Label>
          <Input id="token-password" v-model="password" type="password" autocomplete="current-password" :disabled="busy" />
          <p class="text-xs text-muted-foreground">{{ m.agents.create.passwordNeeded }}</p>
        </div>
      </div>

      <div class="flex flex-wrap justify-end gap-2">
        <Button variant="outline" :disabled="busy" @click="close">{{ m.common.cancel }}</Button>
        <Button type="submit" :disabled="busy || !valid">{{ busy ? m.agents.create.creating : m.agents.create.createToken }}</Button>
      </div>
    </form>
  </Dialog>
</template>
