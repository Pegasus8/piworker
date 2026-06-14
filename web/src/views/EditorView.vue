<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useFlowsStore } from '@/stores/flows'
import { useObservabilityStore } from '@/stores/observability'
import { FlowCanvas, NodePalette, NodeConfigPanel, ActivityPanel } from '@/components/flow'
import { Button, Input, Switch, Badge, Dialog } from '@/components/ui'
import {
  ArrowLeft,
  Save,
  Play,
  Square,
  Settings,
  Workflow
} from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const flowsStore = useFlowsStore()
const obs = useObservabilityStore()

const showSettings = ref(false)
const isSaving = ref(false)

const flowId = computed(() => route.params.id as string | undefined)
const isNewFlow = computed(() => !flowId.value)
const isDeploying = computed(() => flowsStore.isDeploying)
const isStopping = computed(() => flowsStore.isStopping)
const isRunning = computed(() => flowsStore.currentFlow?.running ?? false)

onMounted(async () => {
  if (flowId.value) {
    await flowsStore.fetchFlow(flowId.value)
  } else {
    flowsStore.initNewFlow()
  }
})

// Open the live event stream whenever the flow is running; close it otherwise.
watch(
  () => [flowsStore.currentFlow?.id, isRunning.value] as const,
  ([id, running]) => {
    if (running && id) {
      obs.connect(id)
    } else {
      obs.disconnect()
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  obs.reset()
  flowsStore.resetCurrentFlow()
})

async function handleSave() {
  if (!flowsStore.currentFlow) return

  isSaving.value = true
  try {
    if (isNewFlow.value) {
      // Create new flow first
      const flow = await flowsStore.createFlow(
        flowsStore.currentFlow.name,
        flowsStore.currentFlow.description
      )
      // Update current flow with new ID
      flowsStore.currentFlow.id = flow.id
      // Navigate to the new flow URL
      router.replace(`/editor/${flow.id}`)
    }
    await flowsStore.saveFlow()
  } catch (e) {
    console.error('Failed to save flow:', e)
  } finally {
    isSaving.value = false
  }
}

async function handleDeploy() {
  try {
    if (isRunning.value) {
      // Stopping: do NOT save first — the backend rejects updates to a running
      // flow (409), and stopping should not depend on persisting edits.
      await flowsStore.stopFlow()
    } else {
      // Deploying: persist any pending edits first so we run the latest version.
      if (!flowsStore.currentFlow?.id || flowsStore.isDirty) {
        await handleSave()
      }
      await flowsStore.deployFlow()
    }
  } catch (e) {
    console.error('Deploy/Stop failed:', e)
  }
}

function handleBack() {
  if (flowsStore.isDirty) {
    if (confirm('You have unsaved changes. Are you sure you want to leave?')) {
      router.push('/')
    }
  } else {
    router.push('/')
  }
}

function updateFlowName(value: string | number) {
  if (flowsStore.currentFlow) {
    flowsStore.currentFlow.name = String(value)
    flowsStore.isDirty = true
  }
}

function updateFlowDescription(value: string | number) {
  if (flowsStore.currentFlow) {
    flowsStore.currentFlow.description = String(value)
    flowsStore.isDirty = true
  }
}

function toggleFlowEnabled(enabled: boolean) {
  if (flowsStore.currentFlow) {
    flowsStore.currentFlow.enabled = enabled
    flowsStore.isDirty = true
  }
}
</script>

<template>
  <div class="flex h-screen flex-col bg-background">
    <!-- Header -->
    <header class="flex h-14 items-center justify-between border-b bg-card px-4">
      <div class="flex items-center gap-4">
        <Button variant="ghost" size="icon" @click="handleBack">
          <ArrowLeft class="h-4 w-4" />
        </Button>

        <div class="flex items-center gap-3">
          <Workflow class="h-5 w-5 text-primary" />
          <Input
            v-if="flowsStore.currentFlow"
            :model-value="flowsStore.currentFlow.name"
            class="h-8 w-48 border-transparent bg-transparent px-2 text-base font-medium hover:border-input focus:border-input"
            @update:model-value="updateFlowName"
          />
        </div>

        <Badge v-if="flowsStore.isDirty" variant="secondary" class="text-xs">
          Unsaved
        </Badge>
      </div>

      <div class="flex items-center gap-2">
        <!-- Flow Status Toggle -->
        <div v-if="flowsStore.currentFlow" class="flex items-center gap-2 mr-4">
          <span class="text-sm text-muted-foreground">
            {{ flowsStore.currentFlow.enabled ? 'Active' : 'Inactive' }}
          </span>
          <Switch
            :model-value="flowsStore.currentFlow.enabled"
            @update:model-value="toggleFlowEnabled"
          />
        </div>

        <Button variant="outline" size="sm" @click="showSettings = true">
          <Settings class="mr-2 h-4 w-4" />
          Settings
        </Button>

        <Button
          variant="outline"
          size="sm"
          :disabled="!flowsStore.isDirty || isSaving"
          @click="handleSave"
        >
          <Save class="mr-2 h-4 w-4" />
          {{ isSaving ? 'Saving...' : 'Save' }}
        </Button>

        <Button
          size="sm"
          :variant="isRunning ? 'destructive' : 'default'"
          :disabled="isDeploying || isStopping || (!flowId && !flowsStore.currentFlow?.name)"
          @click="handleDeploy"
        >
          <Play v-if="!isRunning" class="mr-2 h-4 w-4" />
          <Square v-else class="mr-2 h-4 w-4" />
          {{ isDeploying ? 'Deploying...' : isStopping ? 'Stopping...' : isRunning ? 'Stop' : 'Deploy' }}
        </Button>
      </div>
    </header>

    <!-- Main Editor Area -->
    <div class="flex flex-1 overflow-hidden">
      <!-- Node Palette -->
      <aside class="w-64 flex-shrink-0">
        <NodePalette />
      </aside>

      <!-- Canvas -->
      <main class="flex-1 relative">
        <FlowCanvas />
      </main>

      <!-- Config Panel (slides in from right) -->
      <NodeConfigPanel />
    </div>

    <!-- Live execution activity (runs history + debug inspector) -->
    <ActivityPanel />

    <!-- Settings Dialog -->
    <Dialog v-model:open="showSettings">
      <template #default="{ close }">
        <div class="space-y-4">
          <div>
            <h2 class="text-lg font-semibold">Flow Settings</h2>
            <p class="text-sm text-muted-foreground">
              Configure your flow settings.
            </p>
          </div>

          <div v-if="flowsStore.currentFlow" class="space-y-4">
            <div>
              <label class="text-sm font-medium">Name</label>
              <Input
                :model-value="flowsStore.currentFlow.name"
                class="mt-1"
                @update:model-value="updateFlowName"
              />
            </div>
            <div>
              <label class="text-sm font-medium">Description</label>
              <Input
                :model-value="flowsStore.currentFlow.description || ''"
                placeholder="Optional description..."
                class="mt-1"
                @update:model-value="updateFlowDescription"
              />
            </div>
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium">Enabled</label>
                <p class="text-xs text-muted-foreground">
                  When enabled, this flow will run automatically.
                </p>
              </div>
              <Switch
                :model-value="flowsStore.currentFlow.enabled"
                @update:model-value="toggleFlowEnabled"
              />
            </div>
          </div>

          <div class="flex justify-end gap-2">
            <Button @click="close">Done</Button>
          </div>
        </div>
      </template>
    </Dialog>
  </div>
</template>
