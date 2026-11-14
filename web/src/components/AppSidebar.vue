<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Workflow, Radio, Bot, Braces, KeyRound, Cable, ArchiveRestore, DatabaseBackup, ShieldCheck, UserRound, LogOut, Menu, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useAgentStore } from '@/stores/agent'
import { useShellStore } from '@/stores/shell'
import { BACKUPS_POLL_MS, useBackupsStore } from '@/stores/backups'
import { createVisibilityPoller, type Poller } from '@/lib/poller'
import { messages as m } from '@/i18n'
import LanguageSelect from '@/components/LanguageSelect.vue'
import VariablesDialog from '@/components/VariablesDialog.vue'
import SecretsDialog from '@/components/SecretsDialog.vue'
import BrokerConnectionsDialog from '@/components/BrokerConnectionsDialog.vue'
import StoresBackupDialog from '@/components/StoresBackupDialog.vue'

// The navigation of every page but the editor and sign-in: sections are
// links (open in a new tab, show where you are); global settings open as
// dialogs over the current page. On small screens it is a drawer.
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const agent = useAgentStore()
const shell = useShellStore()
const backups = useBackupsStore()

const agentWaiting = computed(() => agent.pendingCount + agent.pendingDrafts)
const agentsLabel = computed(() => m.value.nav.agentsWaiting(agent.pendingCount, agent.pendingDrafts))
const agentsTarget = computed(() => agent.pendingCount ? '/agents/approvals' : agent.pendingDrafts ? '/agents/drafts' : '/agents')

const sections = computed(() => [
  { to: '/', label: m.value.nav.flows, icon: Workflow, active: route.path === '/' },
  { to: '/zigbee', label: m.value.nav.devices, icon: Radio, active: route.path.startsWith('/zigbee') },
  ...(auth.authEnabled ? [{ to: agentsTarget.value, label: m.value.nav.agents, icon: Bot, active: route.path.startsWith('/agents'), badge: agentWaiting.value, aria: agentsLabel.value }] : [])
])
const settings = computed(() => [
  { label: m.value.nav.variables, hint: m.value.nav.variablesHint, icon: Braces, open: () => { shell.showVariables = true } },
  { label: m.value.nav.secrets, hint: m.value.nav.secretsHint, icon: KeyRound, open: () => { shell.showSecrets = true } },
  { label: m.value.nav.brokers, hint: m.value.nav.brokersHint, icon: Cable, open: () => { shell.showBrokers = true } },
  { label: m.value.nav.backup, hint: m.value.nav.backupHint, icon: ArchiveRestore, open: () => { shell.showBackup = true } }
])
const pages = computed(() => [
  { to: '/backups', label: m.value.nav.backups, icon: DatabaseBackup, active: route.path === '/backups', alert: backups.alert ? m.value.backups.navAlert : '' },
  { to: '/trust', label: m.value.nav.https, icon: ShieldCheck, active: route.path === '/trust' },
  ...(auth.authEnabled ? [{ to: '/account', label: m.value.nav.account, icon: UserRound, active: route.path === '/account' }] : [])
])

watch(() => route.fullPath, () => { shell.drawerOpen = false })

// The backup status marks the Backups link when it needs attention (a
// failure, a stale backup). A background read: it never extends the
// session (ADR-0017).
let backupPoller: Poller | null = null
onMounted(() => {
  backupPoller = createVisibilityPoller(() => backups.fetchOverview({ background: true }).catch(() => {}), { intervalMs: BACKUPS_POLL_MS })
  backupPoller.start()
})
onUnmounted(() => backupPoller?.stop())

const logoutError = ref('')
async function logout() {
  logoutError.value = ''
  try { await auth.logout(); await router.push('/login') }
  catch (e: any) {
    if (e.response?.status === 401) { auth.clearAuth(); await router.push('/login') }
    else logoutError.value = m.value.nav.logoutFailed
  }
}
const item = 'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
</script>

<template>
  <!-- Small screens: a top bar that opens the drawer -->
  <div class="flex shrink-0 items-center gap-2 border-b bg-card px-3 py-2 md:hidden">
    <button class="rounded-md p-2 hover:bg-accent" :aria-label="m.nav.openMenu" :aria-expanded="shell.drawerOpen" aria-controls="app-navigation" @click="shell.drawerOpen = true">
      <Menu class="h-5 w-5" />
    </button>
    <RouterLink to="/" class="flex items-center gap-2 font-bold"><Workflow class="h-6 w-6 text-primary" aria-hidden="true" />PiWorker</RouterLink>
    <span v-if="agentWaiting" class="ml-auto rounded-full bg-destructive px-2 text-xs font-semibold text-destructive-foreground" :aria-label="agentsLabel">{{ agentWaiting }}</span>
  </div>
  <div v-if="shell.drawerOpen" class="fixed inset-0 z-40 bg-black/50 md:hidden" aria-hidden="true" @click="shell.drawerOpen = false" />

  <nav id="app-navigation" :aria-label="m.nav.main"
    class="fixed inset-y-0 left-0 z-50 flex w-64 flex-col border-r bg-card transition-transform md:static md:z-auto md:w-56 md:translate-x-0 lg:w-60"
    :class="shell.drawerOpen ? 'translate-x-0' : '-translate-x-full'">
    <div class="flex items-center gap-2 px-4 py-4">
      <RouterLink to="/" class="flex items-center gap-2 text-lg font-bold"><Workflow class="h-7 w-7 text-primary" aria-hidden="true" />PiWorker</RouterLink>
      <button class="ml-auto rounded-md p-2 hover:bg-accent md:hidden" :aria-label="m.nav.closeMenu" @click="shell.drawerOpen = false"><X class="h-4 w-4" /></button>
    </div>

    <div class="flex-1 space-y-6 overflow-y-auto px-3 pb-4">
      <ul class="space-y-1">
        <li v-for="s in sections" :key="s.label">
          <RouterLink :to="s.to" :class="[item, s.active ? 'bg-primary/10 text-primary' : 'text-foreground']" :aria-current="s.active ? 'page' : undefined" :aria-label="s.aria">
            <component :is="s.icon" class="h-4 w-4 shrink-0" aria-hidden="true" />
            {{ s.label }}
            <span v-if="s.badge" class="ml-auto rounded-full bg-destructive px-1.5 text-xs font-semibold text-destructive-foreground" aria-hidden="true">{{ s.badge }}</span>
          </RouterLink>
        </li>
      </ul>

      <div>
        <p class="px-3 pb-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">{{ m.nav.settings }}</p>
        <ul class="space-y-1">
          <li v-for="s in settings" :key="s.label">
            <button type="button" :class="[item, 'text-foreground']" :title="s.hint" @click="s.open(); shell.drawerOpen = false">
              <component :is="s.icon" class="h-4 w-4 shrink-0" aria-hidden="true" />{{ s.label }}
            </button>
          </li>
          <li v-for="p in pages" :key="p.to">
            <RouterLink :to="p.to" :class="[item, p.active ? 'bg-primary/10 text-primary' : 'text-foreground']" :aria-current="p.active ? 'page' : undefined"
              :aria-label="'alert' in p && p.alert ? `${p.label}: ${p.alert}` : undefined">
              <component :is="p.icon" class="h-4 w-4 shrink-0" aria-hidden="true" />{{ p.label }}
              <span v-if="'alert' in p && p.alert" class="ml-auto h-2 w-2 rounded-full bg-amber-500" :title="p.alert" aria-hidden="true" />
            </RouterLink>
          </li>
        </ul>
      </div>
    </div>

    <div class="space-y-2 border-t p-3">
      <LanguageSelect />
      <p v-if="logoutError" role="alert" class="text-xs text-destructive">{{ logoutError }}</p>
      <button v-if="auth.authEnabled" type="button" :class="[item, 'text-muted-foreground']" @click="logout">
        <LogOut class="h-4 w-4" aria-hidden="true" />{{ m.nav.logout }}
      </button>
    </div>
  </nav>

  <!-- The one copy of each global dialog (pages open them through the shell
       store, so element ids stay unique). -->
  <VariablesDialog v-model:open="shell.showVariables" />
  <SecretsDialog v-model:open="shell.showSecrets" @changed="shell.secretsChanged++" />
  <BrokerConnectionsDialog v-model:open="shell.showBrokers" @changed="shell.brokersChanged++" />
  <StoresBackupDialog v-model:open="shell.showBackup" @changed="shell.secretsChanged++; shell.brokersChanged++" />
</template>
