<script setup lang="ts">
// Agent tokens and their grants, and the agent deploy level
// (docs/design/mcp.md §7, §10). Token names and tool texts are shown as text.
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { AlertTriangle, KeyRound, Plug, Plus, ShieldAlert } from 'lucide-vue-next'
import { Badge, Button, Card, Input, Label } from '@/components/ui'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import SecretsDialog from '@/components/SecretsDialog.vue'
import AgentNav from '@/components/agent/AgentNav.vue'
import CreateTokenDialog from '@/components/agent/CreateTokenDialog.vue'
import PasswordPrompt from '@/components/agent/PasswordPrompt.vue'
import TokenSecretDialog from '@/components/agent/TokenSecretDialog.vue'
import type { Feedback } from '@/composables/useFeedback'
import { useAgentStore, apiErrorMessage, type GrantChange } from '@/stores/agent'
import { useAuthStore } from '@/stores/auth'
import {
  DEPLOY_LEVELS, GUESSING_AT_EVERY_LEVEL, clientSnippets, formatDateTime, formatRelative, grantStatus, mcpStatus, toolKey,
  type AgentDeployLevel, type AgentToken, type CreatedAgentToken, type TokenGrant
} from '@/lib/agentAccess'

const agent = useAgentStore()
const auth = useAuthStore()
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const passwordPrompt = ref<InstanceType<typeof PasswordPrompt>>()
const feedback = ref<Feedback | null>(null)
const pending = ref(false)
const loadError = ref('')
const loading = ref(false)
const showCreate = ref(false)
const created = ref<CreatedAgentToken | null>(null)
const expanded = ref<Record<string, boolean>>({})
const addChoice = ref<Record<string, string>>({})
// Null until the server's level is known: no level is shown as chosen then.
const level = ref<AgentDeployLevel | null>(null)
const levelPassword = ref('')
const settingsError = ref('')
// Short secrets, the ones an agent could confirm a guess of at any level
// (ADR-0021): read once with the page and again after the admin edits the
// secrets.
const secretsChecked = ref(false)
const showSecrets = ref(false)

const endpoint = computed(() => clientSnippets({ token: '', location: window.location, tls: agent.tls }).url)
const mcp = computed(() => mcpStatus(agent.settings, agent.tls))
const levelChanged = computed(() => !!agent.settings && !!level.value && level.value !== agent.settings.agentDeploy)
watch(() => agent.settings?.agentDeploy, v => { level.value = v ?? null }, { immediate: true })
const authorScope = computed(() => !agent.settings ? 'unknown' : agent.settings.authorEnabled ? 'enabled' : 'disabled')

async function loadSettings() {
  settingsError.value = ''
  try {
    await agent.fetchSettings()
  } catch (e) {
    settingsError.value = apiErrorMessage(e, 'Could not read the agent deploy level.')
  }
}

async function loadSecrets() {
  try {
    await agent.fetchSecretList()
  } catch {
    // Shown as unknown (agent.shortSecrets).
  } finally {
    secretsChecked.value = true
  }
}

async function load() {
  if (!auth.authEnabled) return
  loading.value = true
  loadError.value = ''
  try {
    await agent.fetchTokens()
  } catch (e) {
    loadError.value = apiErrorMessage(e, 'Could not load the agent tokens.')
  } finally {
    loading.value = false
  }
  await Promise.allSettled([agent.fetchTools(), loadSettings(), agent.fetchTLS(), agent.fetchFlowNames(), loadSecrets()])
}
onMounted(load)

// Runs a change and reports the server's message when it fails.
async function act(action: () => Promise<unknown>, success: string, failure: string) {
  if (pending.value) return false
  pending.value = true
  feedback.value = { kind: 'pending', text: 'Working…' }
  try {
    await action()
    feedback.value = { kind: 'success', text: success }
    return true
  } catch (e) {
    feedback.value = { kind: 'error', text: apiErrorMessage(e, failure) }
    return false
  } finally {
    pending.value = false
  }
}

function onCreated(token: CreatedAgentToken) {
  created.value = token
  feedback.value = { kind: 'success', text: `Token “${token.info.name}” created.` }
}

const statusLabel = (t: AgentToken) => ({ active: 'Active', expired: 'Expired', revoked: 'Revoked' })[t.status] ?? t.status
const statusVariant = (t: AgentToken) => t.status === 'active' ? 'secondary' : 'outline'
const riskVariant = (risk: string) => risk === 'privileged' ? 'destructive' : risk === 'external' ? 'secondary' : 'outline'
const suspendedCount = (t: AgentToken) => t.grants.filter(g => g.suspended).length
const toolOf = (g: TokenGrant) => agent.toolFor(g.flowId, g.triggerNodeId)
const toolLabel = (g: TokenGrant) => toolOf(g)?.toolName ?? `node ${g.triggerNodeId}`
const flowLabel = (flowId: string) => agent.tools.find(t => t.flowId === flowId)?.flowName ?? agent.flowNames[flowId] ?? flowId
const stateLabel = { active: 'Active', changed: 'Changed', suspended: 'Suspended', missing: 'Not published' } as const
const ungranted = (t: AgentToken) => agent.tools.filter(tool => !t.grants.some(g => g.flowId === tool.flowId && g.triggerNodeId === tool.triggerNodeId))

async function revoke(t: AgentToken) {
  if (pending.value || !await confirmation.value?.ask('Revoke token?', `“${t.name}” stops working immediately, and its requests waiting for approval are rejected. This cannot be undone.`, 'Revoke token')) return
  await act(() => agent.revokeToken(t.id), `Token “${t.name}” revoked.`, 'Could not revoke the token. Try again.')
}

async function revokeAll() {
  const n = agent.activeTokens.length
  if (pending.value || !n || !await confirmation.value?.ask('Revoke all tokens?', `All ${n} active token${n === 1 ? '' : 's'} stop working immediately, and their requests waiting for approval are rejected. Agents need new tokens to connect again.`, 'Revoke all tokens')) return
  // One request revokes every token in one transaction: all of them or none.
  let revoked = 0
  if (await act(async () => { revoked = await agent.revokeAllTokens() }, '', 'Could not revoke the tokens. Try again.')) {
    feedback.value = { kind: 'success', text: `Revoked ${revoked} token${revoked === 1 ? '' : 's'}.` }
  }
}

async function changeGrant(t: AgentToken, change: GrantChange, success: string) {
  await act(() => agent.updateGrant(t.id, change), success, 'Could not update the grant. Try again.')
}

async function reconfirm(t: AgentToken, g: TokenGrant) {
  const status = grantStatus(g, toolOf(g))
  const risk = toolOf(g)?.risk
  const password = await passwordPrompt.value?.ask('Re-confirm grant', `Let “${t.name}” call “${toolLabel(g)}” as it is now${risk ? ` (risk: ${risk})` : ''}.`, 'Re-confirm')
  if (!password) return
  await changeGrant(t, { action: 'confirm', flowId: g.flowId, triggerNodeId: g.triggerNodeId, fingerprint: status.confirmFingerprint, password }, 'Grant confirmed.')
}

async function suspend(t: AgentToken, g: TokenGrant) {
  await changeGrant(t, { action: 'suspend', flowId: g.flowId, triggerNodeId: g.triggerNodeId }, 'Grant suspended.')
}

async function remove(t: AgentToken, g: TokenGrant) {
  if (pending.value || !await confirmation.value?.ask('Remove grant?', `“${t.name}” can no longer call “${toolLabel(g)}”. Adding it back needs your password.`, 'Remove grant')) return
  await changeGrant(t, { action: 'remove', flowId: g.flowId, triggerNodeId: g.triggerNodeId }, 'Grant removed.')
}

async function addGrant(t: AgentToken) {
  const tool = agent.tools.find(x => toolKey(x.flowId, x.triggerNodeId) === addChoice.value[t.id])
  if (!tool) return
  const password = await passwordPrompt.value?.ask('Add grant', `Let “${t.name}” call “${tool.toolName}” (risk: ${tool.risk}).`, 'Add grant')
  if (!password) return
  const ok = await act(() => agent.updateGrant(t.id, { action: 'add', flowId: tool.flowId, triggerNodeId: tool.triggerNodeId, fingerprint: tool.fingerprint, password }), 'Grant added.', 'Could not add the grant. Try again.')
  if (ok) addChoice.value[t.id] = ''
}

async function saveLevel() {
  const chosen = level.value
  if (!levelChanged.value || !chosen || !levelPassword.value) return
  const ok = await act(() => agent.saveSettings(chosen, levelPassword.value), 'Agent deploy level changed.', 'Could not change the agent deploy level. Try again.')
  levelPassword.value = ''
  if (!ok) level.value = agent.settings?.agentDeploy ?? null
}
function cancelLevel() {
  level.value = agent.settings?.agentDeploy ?? null
  levelPassword.value = ''
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-4xl space-y-6">
      <AgentNav />
      <OperationFeedback :message="feedback" @dismiss="feedback = null" />

      <Card v-if="auth.authEnabled" class="space-y-4 p-4 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-lg font-semibold">Tokens</h2>
            <p class="text-sm text-muted-foreground">Tokens work only on the MCP endpoint, never in this editor. Revoking needs no password.</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <Button variant="outline" class="text-destructive" :disabled="pending || !agent.activeTokens.length" @click="revokeAll">Revoke all</Button>
            <Button :disabled="pending" @click="showCreate = true"><Plus class="mr-2 h-4 w-4" />New token</Button>
          </div>
        </div>

        <p v-if="loadError" role="alert" class="text-sm text-destructive">{{ loadError }} <Button variant="link" class="h-auto p-0" @click="load">Retry</Button></p>
        <p v-else-if="!loading && !agent.tokens.length" class="rounded-md bg-muted p-4 text-center text-sm text-muted-foreground">No agent tokens yet.</p>

        <ul class="space-y-3">
          <li v-for="t in agent.tokens" :key="t.id" class="space-y-3 rounded-lg border p-4" :class="t.status !== 'active' && 'opacity-75'">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0 space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="break-words font-medium">{{ t.name }}</span>
                  <Badge :variant="statusVariant(t)">{{ statusLabel(t) }}</Badge>
                  <Badge v-if="t.privileged" variant="destructive">privileged</Badge>
                  <Badge v-if="t.allTools" variant="outline">all tools</Badge>
                  <Badge v-if="suspendedCount(t)" variant="destructive">{{ suspendedCount(t) }} suspended</Badge>
                </div>
                <p class="font-mono text-xs text-muted-foreground">{{ t.prefix }}_…</p>
                <dl class="grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
                  <div><dt class="text-muted-foreground">Scopes</dt><dd>{{ t.scopes.join(', ') }}</dd></div>
                  <div><dt class="text-muted-foreground">{{ t.status === 'expired' ? 'Expired' : 'Expires' }}</dt><dd :title="formatDateTime(t.expiresAt)">{{ formatRelative(t.expiresAt) }}</dd></div>
                  <div><dt class="text-muted-foreground">Last used</dt><dd :title="formatDateTime(t.lastUsedAt)">{{ t.lastUsedAt ? formatRelative(t.lastUsedAt) : 'Never' }}</dd></div>
                  <div v-if="t.revokedAt"><dt class="text-muted-foreground">Revoked</dt><dd :title="formatDateTime(t.revokedAt)">{{ formatRelative(t.revokedAt) }}</dd></div>
                  <div v-else><dt class="text-muted-foreground">Created</dt><dd :title="formatDateTime(t.createdAt)">{{ formatRelative(t.createdAt) }}</dd></div>
                </dl>
              </div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" :aria-expanded="!!expanded[t.id]" @click="expanded[t.id] = !expanded[t.id]">
                  {{ expanded[t.id] ? 'Hide grants' : `Show grants (${t.grants.length})` }}
                </Button>
                <Button v-if="t.status === 'active'" size="sm" variant="outline" class="text-destructive" :disabled="pending" @click="revoke(t)">Revoke</Button>
              </div>
            </div>

            <div v-if="expanded[t.id]" class="space-y-3 border-t pt-3">
              <p v-if="!t.scopes.includes('call')" class="text-sm text-muted-foreground">Grants only matter with the call scope, which this token does not have.</p>
              <p v-if="t.allTools" class="text-sm text-muted-foreground">This token may also call every published tool that is not privileged.</p>
              <p v-if="!t.grants.length" class="text-sm text-muted-foreground">No individual tools granted.</p>
              <ul v-else class="space-y-2">
                <li v-for="g in t.grants" :key="toolKey(g.flowId, g.triggerNodeId)" class="space-y-2 rounded-md border p-3">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="break-all font-mono text-sm font-medium">{{ toolLabel(g) }}</span>
                    <span class="text-xs text-muted-foreground">flow {{ flowLabel(g.flowId) }}</span>
                    <Badge v-if="toolOf(g)" :variant="riskVariant(toolOf(g)!.risk)">{{ toolOf(g)!.risk }}</Badge>
                    <Badge :variant="grantStatus(g, toolOf(g)).state === 'active' ? 'secondary' : 'destructive'">{{ stateLabel[grantStatus(g, toolOf(g)).state] }}</Badge>
                  </div>
                  <p v-if="grantStatus(g, toolOf(g)).reason" class="text-xs text-muted-foreground">{{ grantStatus(g, toolOf(g)).reason }}</p>
                  <div v-if="t.status === 'active'" class="flex flex-wrap gap-2">
                    <Button v-if="grantStatus(g, toolOf(g)).needsConfirm" size="sm" :disabled="pending" @click="reconfirm(t, g)">Re-confirm</Button>
                    <Button v-if="!g.suspended" size="sm" variant="outline" :disabled="pending" @click="suspend(t, g)">Suspend</Button>
                    <Button size="sm" variant="ghost" class="text-destructive" :disabled="pending" @click="remove(t, g)">Remove</Button>
                  </div>
                </li>
              </ul>
              <form v-if="t.status === 'active' && t.scopes.includes('call')" class="flex flex-wrap items-end gap-2" @submit.prevent="addGrant(t)">
                <div class="min-w-0 flex-1 space-y-1">
                  <Label :for="`add-tool-${t.id}`" class="text-xs">Add a tool</Label>
                  <select :id="`add-tool-${t.id}`" :value="addChoice[t.id] ?? ''" :disabled="pending || !ungranted(t).length"
                    @change="addChoice[t.id] = ($event.target as HTMLSelectElement).value"
                    class="flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50">
                    <option value="">{{ agent.tools.length ? (ungranted(t).length ? 'Choose a published tool' : 'Every published tool is granted') : 'No tools published yet' }}</option>
                    <option v-for="tool in ungranted(t)" :key="toolKey(tool.flowId, tool.triggerNodeId)" :value="toolKey(tool.flowId, tool.triggerNodeId)"
                      :disabled="tool.risk === 'privileged' && !t.privileged">
                      {{ tool.toolName }} ({{ tool.flowName }}){{ tool.risk === 'privileged' && !t.privileged ? ' — needs a privileged token' : '' }}
                    </option>
                  </select>
                </div>
                <Button type="submit" size="sm" :disabled="pending || !addChoice[t.id]">Add</Button>
              </form>
            </div>
          </li>
        </ul>
      </Card>

      <Card v-if="auth.authEnabled" class="space-y-4 p-4 sm:p-6">
        <div>
          <h2 class="flex items-center gap-2 text-lg font-semibold"><ShieldAlert class="h-5 w-5 text-primary" aria-hidden="true" />Agent deploy</h2>
          <p class="text-sm text-muted-foreground">
            Whether agents may deploy, start and stop flows without you. Tokens also need the deploy scope, which can only
            be granted while this is not off. Changing it needs your password.
          </p>
        </div>
        <p v-if="settingsError" role="alert" class="text-sm text-destructive">
          The current level is unknown ({{ settingsError }}).
          <Button variant="link" class="h-auto p-0" :disabled="pending" @click="loadSettings">Retry</Button>
        </p>
        <p id="deploy-guessing" class="flex items-start gap-1.5 text-xs text-muted-foreground">
          <KeyRound class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
          <span><strong class="font-medium text-foreground">Guessable secrets, at every level:</strong> {{ GUESSING_AT_EVERY_LEVEL }}</span>
        </p>
        <div role="radiogroup" aria-label="Agent deploy level" aria-describedby="deploy-guessing" class="space-y-2">
          <div v-for="l in DEPLOY_LEVELS" :key="l.value" class="flex items-start gap-3 rounded-md border p-3" :class="level === l.value && 'border-primary'">
            <input :id="`deploy-${l.value}`" v-model="level" type="radio" name="agent-deploy" :value="l.value" class="mt-1 accent-primary"
              :disabled="pending || !agent.settings"
              :aria-describedby="l.residual ? `deploy-${l.value}-risk deploy-${l.value}-residual` : `deploy-${l.value}-risk`" />
            <div class="space-y-1">
              <label :for="`deploy-${l.value}`" class="text-sm font-medium">{{ l.label }}</label>
              <p :id="`deploy-${l.value}-risk`" class="text-xs text-muted-foreground">{{ l.risk }}</p>
              <p v-if="l.residual" :id="`deploy-${l.value}-residual`" class="flex items-start gap-1.5 text-xs text-muted-foreground">
                <KeyRound class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                <span><strong class="font-medium text-foreground">Guessable secrets, also:</strong> {{ l.residual }}</span>
              </p>
            </div>
          </div>
        </div>
        <div v-if="secretsChecked" class="flex flex-wrap items-start gap-2 rounded-md border p-3 text-sm"
          :class="agent.shortSecrets.short ? 'border-amber-500/40 bg-amber-500/10' : ''" role="status">
          <AlertTriangle v-if="agent.shortSecrets.short !== 0" class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
          <KeyRound v-else class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <p class="min-w-0 flex-1">
            <span class="font-medium">Short secrets:</span> {{ agent.shortSecrets.text }}
            <Button variant="link" class="h-auto p-0" @click="showSecrets = true">Review secrets</Button>
          </p>
        </div>
        <form v-if="levelChanged" class="space-y-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3" @submit.prevent="saveLevel">
          <p v-if="level !== 'off'" class="flex items-start gap-2 text-sm">
            <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
            You accept this risk for every token with the deploy scope. Every agent deploy is recorded in the audit log.
          </p>
          <p v-else class="text-sm">Agents with the deploy scope lose the deploy tools; their tokens keep working otherwise.</p>
          <div class="space-y-1">
            <Label for="deploy-password">Password to change the level</Label>
            <Input id="deploy-password" v-model="levelPassword" type="password" autocomplete="current-password" :disabled="pending" />
          </div>
          <div class="flex flex-wrap gap-2">
            <Button type="submit" :disabled="pending || !levelPassword">Change agent deploy</Button>
            <Button variant="outline" :disabled="pending" @click="cancelLevel">Cancel</Button>
          </div>
        </form>
        <p class="text-xs text-muted-foreground">
          <template v-if="authorScope === 'unknown'">Author scope: unknown (the server's settings could not be read).</template>
          <template v-else>Author scope: {{ authorScope }} in piworker.toml (author_enabled in the [mcp] section).</template>
        </p>
      </Card>

      <Card class="space-y-2 p-4 sm:p-6">
        <h2 class="flex items-center gap-2 text-lg font-semibold"><Plug class="h-5 w-5 text-primary" aria-hidden="true" />Connect an agent</h2>
        <p class="text-sm text-muted-foreground">
          Agents connect to <code class="break-all">{{ endpoint }}</code> and send their token as a Bearer header. Each new
          token comes with ready-to-paste set-up for Claude Code and Claude Desktop.
        </p>
        <p v-if="mcp.state !== 'on'" class="text-sm text-muted-foreground">{{ mcp.label }}: {{ mcp.detail }}</p>
        <p class="text-sm text-muted-foreground">
          Clients must trust this PiWorker's certificate:
          <RouterLink to="/trust" class="text-primary underline-offset-2 hover:underline">Trust this device</RouterLink>.
        </p>
      </Card>
    </div>
    <CreateTokenDialog v-model:open="showCreate" @created="onCreated" />
    <SecretsDialog v-model:open="showSecrets" @changed="loadSecrets" />
    <TokenSecretDialog :created="created" :tls="agent.tls" :mcp="mcp" @done="created = null" />
    <PasswordPrompt ref="passwordPrompt" />
    <ConfirmDialog ref="confirmation" />
  </main>
</template>
