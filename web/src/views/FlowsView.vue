<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useFlowsStore } from '@/stores/flows'
import { useAuthStore } from '@/stores/auth'
import { Button, Card, Input, Dialog, Label, Switch, Badge } from '@/components/ui'
import {
  Plus,
  Trash2,
  Edit3,
  Search,
  Workflow,
  Calendar,
  LogOut
} from 'lucide-vue-next'

const router = useRouter()
const flowsStore = useFlowsStore()
const authStore = useAuthStore()

const searchQuery = ref('')
const showCreateDialog = ref(false)
const newFlowName = ref('')
const newFlowDescription = ref('')

onMounted(() => {
  flowsStore.fetchFlows()
})

import { computed } from 'vue'

const displayedFlows = computed(() => {
  const query = searchQuery.value.toLowerCase()
  if (!query) return flowsStore.flows
  return flowsStore.flows.filter(
    flow =>
      flow.name.toLowerCase().includes(query) ||
      flow.description?.toLowerCase().includes(query)
  )
})

async function createFlow() {
  if (!newFlowName.value.trim()) return

  try {
    const flow = await flowsStore.createFlow(newFlowName.value, newFlowDescription.value)
    showCreateDialog.value = false
    newFlowName.value = ''
    newFlowDescription.value = ''
    router.push(`/editor/${flow.id}`)
  } catch (e) {
    console.error('Failed to create flow:', e)
  }
}

function editFlow(id: string) {
  router.push(`/editor/${id}`)
}

async function toggleFlow(id: string, enabled: boolean) {
  try {
    await flowsStore.toggleFlow(id, enabled)
  } catch (e) {
    console.error('Failed to toggle flow:', e)
  }
}

async function deleteFlow(id: string) {
  if (confirm('Are you sure you want to delete this flow?')) {
    try {
      await flowsStore.deleteFlow(id)
    } catch (e) {
      console.error('Failed to delete flow:', e)
    }
  }
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

function logout() {
  authStore.logout()
  router.push('/login')
}
</script>

<template>
  <div class="min-h-screen bg-background">
    <!-- Header -->
    <header class="sticky top-0 z-50 border-b bg-card/95 backdrop-blur supports-[backdrop-filter]:bg-card/60">
      <div class="container mx-auto flex h-16 items-center justify-between px-4">
        <div class="flex items-center gap-3">
          <Workflow class="h-8 w-8 text-primary" />
          <h1 class="text-xl font-bold">PiWorker</h1>
        </div>
        <div class="flex items-center gap-2">
          <Button @click="createNewFlow">
            <Plus class="mr-2 h-4 w-4" />
            New Flow
          </Button>
          <Button v-if="authStore.authEnabled" variant="ghost" size="icon" @click="logout" title="Logout">
            <LogOut class="h-4 w-4" />
          </Button>
        </div>
      </div>
    </header>

    <!-- Main Content -->
    <main class="container mx-auto px-4 py-8">
      <!-- Search -->
      <div class="mb-6 flex items-center gap-4">
        <div class="relative flex-1 max-w-md">
          <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="searchQuery"
            placeholder="Search flows..."
            class="pl-9"
          />
        </div>
        <div class="text-sm text-muted-foreground">
          {{ displayedFlows.length }} flow{{ displayedFlows.length !== 1 ? 's' : '' }}
        </div>
      </div>

      <!-- Loading State -->
      <div v-if="flowsStore.loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>

      <!-- Error State -->
      <div v-else-if="flowsStore.error" class="rounded-lg border border-destructive bg-destructive/10 p-4 text-center">
        <p class="text-destructive">{{ flowsStore.error }}</p>
        <Button variant="outline" class="mt-2" @click="flowsStore.fetchFlows">
          Retry
        </Button>
      </div>

      <!-- Empty State -->
      <div v-else-if="displayedFlows.length === 0" class="text-center py-12">
        <Workflow class="mx-auto h-16 w-16 text-muted-foreground/50" />
        <h3 class="mt-4 text-lg font-medium">No flows yet</h3>
        <p class="mt-2 text-muted-foreground">
          Create your first automation flow to get started.
        </p>
        <Button class="mt-4" @click="createNewFlow">
          <Plus class="mr-2 h-4 w-4" />
          Create Flow
        </Button>
      </div>

      <!-- Flows Grid -->
      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card
          v-for="flow in displayedFlows"
          :key="flow.id"
          class="group relative overflow-hidden transition-all duration-200 hover:shadow-md"
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
                :model-value="flow.enabled"
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
              class="absolute top-2 right-2 bg-green-500"
            >
              Running
            </Badge>
            <Badge
              v-else-if="flow.enabled"
              variant="secondary"
              class="absolute top-2 right-2"
            >
              Enabled
            </Badge>

            <!-- Actions -->
            <div class="mt-4 flex gap-2 opacity-0 transition-opacity group-hover:opacity-100">
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
                class="text-destructive hover:bg-destructive/10"
                @click="deleteFlow(flow.id)"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
          </div>
        </Card>
      </div>
    </main>

    <!-- Create Flow Dialog -->
    <Dialog v-model:open="showCreateDialog">
      <template #default="{ close }">
        <div class="space-y-4">
          <div>
            <h2 class="text-lg font-semibold">Create New Flow</h2>
            <p class="text-sm text-muted-foreground">
              Give your automation flow a name and description.
            </p>
          </div>

          <div class="space-y-4">
            <div>
              <Label for="name">Name</Label>
              <Input
                id="name"
                v-model="newFlowName"
                placeholder="My Automation"
                class="mt-1"
              />
            </div>
            <div>
              <Label for="description">Description (optional)</Label>
              <Input
                id="description"
                v-model="newFlowDescription"
                placeholder="Describe what this flow does..."
                class="mt-1"
              />
            </div>
          </div>

          <div class="flex justify-end gap-2">
            <Button variant="outline" @click="close">Cancel</Button>
            <Button :disabled="!newFlowName.trim()" @click="createFlow">
              Create Flow
            </Button>
          </div>
        </div>
      </template>
    </Dialog>
  </div>
</template>
