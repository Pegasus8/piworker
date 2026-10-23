<script setup lang="ts">
import VariablesDialog from '@/components/VariablesDialog.vue'
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useFlowsStore } from '@/stores/flows'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Input, Switch, Badge } from '@/components/ui'
import SecretsDialog from '@/components/SecretsDialog.vue'
import {
  Plus,
  Trash2,
  Edit3,
  Search,
  Workflow,
  Calendar,
  LogOut,
  Copy,
  Download,
  Upload,
  KeyRound
} from 'lucide-vue-next'

import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import { useFeedback } from '@/composables/useFeedback'

const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const { feedback, pending, run } = useFeedback()
const router = useRouter()
const flowsStore = useFlowsStore()
const authStore = useAuthStore()
const api = useApi()

const searchQuery = ref('')
const showSecrets = ref(false)
const showVariables = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

async function handleDuplicate(id: string) {
  await run(async () => { await api.duplicateFlow(id); await flowsStore.fetchFlows() }, 'Flow duplicated.', 'Could not duplicate the flow. Try again.')
}
async function handleExport(id: string, name: string) {
  await run(async () => {
    const flow = await api.exportFlow(id)
    const blob = new Blob([JSON.stringify(flow, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name.replace(/[^a-z0-9-_]+/gi, '_') || 'flow'}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, 'Export prepared. Check your downloads.', 'Could not export the flow. Try again.')
}

function triggerImport() {
  fileInput.value?.click()
}

async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await run(async () => {
    const flow = JSON.parse(await file.text())
    const imported = await api.importFlow(flow)
    await router.push(`/editor/${imported.id}`)
  }, 'Flow imported.', 'Could not import the flow. Check the JSON file and your connection, then try again.')
  input.value = ''

}

onMounted(() => {
  flowsStore.fetchFlows()
})


const displayedFlows = computed(() => {
  const query = searchQuery.value.toLowerCase()
  if (!query) return flowsStore.flows
  return flowsStore.flows.filter(
    flow =>
      flow.name.toLowerCase().includes(query) ||
      flow.description?.toLowerCase().includes(query)
  )
})

function editFlow(id: string) {
  router.push(`/editor/${id}`)
}

async function toggleFlow(id: string, enabled: boolean) {
  await run(() => flowsStore.toggleFlow(id, enabled), enabled ? 'Flow deployed.' : 'Flow stopped.', 'Could not update the flow. Try again.')
  flowsStore.error = null
}

async function deleteFlow(id: string) {
  if (pending.value) return
  const name = flowsStore.flows.find(flow => flow.id === id)?.name || 'this flow'
  if (!await confirmation.value?.ask('Delete flow?', `Delete “${name}” and its configuration? This cannot be undone.`, 'Delete flow')) return
  await run(() => flowsStore.deleteFlow(id), 'Flow deleted.', 'Could not delete the flow. Try again.')
  flowsStore.error = null
}

function formatDate(dateString: string) {
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric'
  })
}

function createNewFlow() {
  flowsStore.initNewFlow()
  router.push('/editor')
}

const logoutError = ref('')
async function logout() {
  logoutError.value = ''
  try { await authStore.logout(); await router.push('/login') }
  catch (e: any) {
    if (e.response?.status === 401) { authStore.clearAuth(); await router.push('/login') }
    else logoutError.value = 'Could not sign out. Check your connection and try again.'
  }
}
</script>

<template>
  <div class="min-h-full bg-background">
    <!-- Header -->
    <header class="sticky top-0 z-50 border-b bg-card/95 backdrop-blur supports-[backdrop-filter]:bg-card/60">
      <div class="container mx-auto flex min-h-16 flex-wrap items-center justify-between gap-3 px-4 py-3">
        <div class="flex items-center gap-3">
          <Workflow class="h-8 w-8 text-primary" />
          <h1 class="text-xl font-bold">PiWorker</h1>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <Button variant="outline" @click="showVariables = true">Variables</Button>
          <Button variant="outline" @click="showSecrets = true" title="Manage secrets">
            <KeyRound class="mr-2 h-4 w-4" />
            Secrets
          </Button>
          <Button variant="outline" :disabled="pending" @click="triggerImport" title="Import a flow from JSON">
            <Upload class="mr-2 h-4 w-4" />
            Import
          </Button>
          <Button @click="createNewFlow">
            <Plus class="mr-2 h-4 w-4" />
            New Flow
          </Button>
          <Button v-if="authStore.authEnabled" variant="ghost" @click="router.push('/account')">Account</Button>
          <Button v-if="authStore.authEnabled" variant="ghost" size="icon" @click="logout" title="Logout" aria-label="Logout">
            <LogOut class="h-4 w-4" />
          </Button>
        </div>
      </div>
    </header>

    <!-- Main Content -->
    <main class="lab-page container mx-auto max-w-7xl px-4 py-8 sm:px-8">
      <p v-if="logoutError" role="alert" class="mb-4 text-sm text-destructive">{{ logoutError }}</p>
      <section class="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="mb-2 text-xs font-semibold uppercase tracking-widest text-primary">Your automation lab</p>
          <h2 class="text-3xl font-semibold sm:text-4xl">Small flows. Big possibilities.</h2>
          <p class="mt-3 max-w-xl text-sm leading-relaxed text-muted-foreground">Connect ideas, automate the everyday, and let your Pi take it from here.</p>
        </div>
        <Badge variant="secondary">{{ flowsStore.flows.filter(flow => flow.running).length }} running</Badge>
      </section>
      <OperationFeedback :message="feedback" class="mb-4" @dismiss="feedback = null" />
      <!-- Search -->
      <div class="mb-6 flex items-center gap-4">
        <div class="relative flex-1 max-w-md">
          <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="searchQuery"
            aria-label="Search flows"
            placeholder="Search flows..."
            class="pl-9"
          />
        </div>
        <div class="text-sm text-muted-foreground">
          {{ displayedFlows.length }} flow{{ displayedFlows.length !== 1 ? 's' : '' }}
        </div>
      </div>

      <!-- Loading State -->
      <div v-if="flowsStore.loading && !flowsStore.flows.length" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>

      <!-- Error State -->
      <div v-else-if="flowsStore.error && !flowsStore.flows.length" class="rounded-lg border border-destructive bg-destructive/10 p-4 text-center">
        <p class="text-destructive">{{ flowsStore.error }}</p>
        <Button variant="outline" class="mt-2" @click="flowsStore.fetchFlows">
          Retry
        </Button>
      </div>

      <!-- Empty State -->
      <div v-else-if="displayedFlows.length === 0" class="lab-empty rounded-xl border border-dashed px-6 py-16 text-center">
        <Workflow class="mx-auto h-16 w-16 text-primary/70" />
        <h3 class="mt-4 text-lg font-medium">{{ searchQuery ? 'No matching flows' : 'No flows yet' }}</h3>
        <p class="mt-2 text-muted-foreground">
          {{ searchQuery ? 'Try a different name or clear your search.' : 'Every useful automation starts with a first connection.' }}
        </p>
        <Button v-if="searchQuery" class="mt-4" variant="outline" @click="searchQuery = ''">Clear search</Button>
        <Button v-else class="mt-4" @click="createNewFlow">
          <Plus class="mr-2 h-4 w-4" />
          Create Flow
        </Button>
      </div>

      <!-- Flows Grid -->
      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card
          v-for="flow in displayedFlows"
          :key="flow.id"
          class="group relative overflow-hidden transition-colors hover:border-primary/50 hover:shadow-md"
        >
          <div class="p-4">
            <!-- Header -->
            <div class="flex items-start justify-between gap-3">
              <div class="flex-1 min-w-0">
                <h3 class="font-semibold truncate">{{ flow.name }}</h3>
                <p
                  v-if="flow.description"
                  class="mt-1 text-sm text-muted-foreground line-clamp-2"
                >
                  {{ flow.description }}
                </p>
              </div>
              <Switch
                :disabled="pending" :aria-label="`${flow.running ? 'Stop' : 'Deploy'} ${flow.name}`"
                :model-value="flow.running"
                @update:model-value="(v) => toggleFlow(flow.id, v)"
              />
            </div>

            <!-- Meta -->
            <div class="mt-4 flex items-center gap-4 text-xs text-muted-foreground">
              <div class="flex items-center gap-1">
                <Workflow class="h-3 w-3" />
                {{ flow.nodeCount }} nodes
              </div>
              <div class="flex items-center gap-1">
                <Calendar class="h-3 w-3" />
                {{ formatDate(flow.updatedAt) }}
              </div>
            </div>

            <!-- Status Badge -->
            <Badge
              v-if="flow.running"
              variant="default"
              class="mt-3 bg-primary text-primary-foreground"
            >
              Running
            </Badge>
            <Badge
              v-else-if="flow.state === 'failed'"
              variant="secondary"
              class="mt-3"
            >
              Failed
            </Badge>

            <!-- Actions -->
            <div class="mt-4 flex gap-2 border-t pt-4">
              <Button
                variant="outline"
                size="sm"
                class="flex-1"
                @click="editFlow(flow.id)"
              >
                <Edit3 class="mr-2 h-4 w-4" />
                Edit
              </Button>
              <Button
                variant="ghost"
                size="icon"
                :disabled="pending" title="Duplicate"
                @click="handleDuplicate(flow.id)"
              >
                <Copy class="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                :disabled="pending" title="Export"
                @click="handleExport(flow.id, flow.name)"
              >
                <Download class="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                class="text-destructive hover:bg-destructive/10"
                :disabled="pending" title="Delete"
                @click="deleteFlow(flow.id)"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
          </div>
        </Card>
      </div>
    </main>

    <ConfirmDialog ref="confirmation" />
    <!-- Secrets manager -->
    <SecretsDialog v-model:open="showSecrets" />
    <VariablesDialog v-model:open="showVariables" />

    <!-- Hidden file input for flow import -->
    <input
      ref="fileInput"
      type="file"
      accept="application/json,.json"
      class="hidden"
      @change="onImportFile"
    />
  </div>
</template>
