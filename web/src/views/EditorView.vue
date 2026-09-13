<script setup lang="ts">
import VariablesDialog from '@/components/VariablesDialog.vue'
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { useFlowsStore } from '@/stores/flows'
import { useObservabilityStore } from '@/stores/observability'
import { FlowCanvas, NodePalette, NodeConfigPanel, ActivityPanel } from '@/components/flow'
import { Button, Input, Badge, Dialog } from '@/components/ui'
import {
  ArrowLeft,
  Save,
  Play,
  Square,
  Settings,
  Workflow
} from 'lucide-vue-next'

import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import ManualRunDialog from '@/components/flow/ManualRunDialog.vue'
import ConnectionsDialog from '@/components/flow/ConnectionsDialog.vue'
import { useFeedback } from '@/composables/useFeedback'
const confirmation = ref<InstanceType<typeof ConfirmDialog>>()
const canvas = ref<InstanceType<typeof FlowCanvas>>()
const showManualRun = ref(false)
const showNodes = ref(false)
const showConnections = ref(false)
const { feedback, pending, run } = useFeedback()
async function addNode(type: string) {
  showNodes.value = false
  await nextTick()
  canvas.value?.addNodeType(type)
}
const route = useRoute()
const router = useRouter()
const flowsStore = useFlowsStore()
const obs = useObservabilityStore()

const showSettings = ref(false)
const showVariables = ref(false)
const isSaving = ref(false)

const flowId = computed(() => route.params.id as string | undefined)
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
    obs.setFlow(id || null)
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

async function handleSave(): Promise<boolean> {
  if (!flowsStore.currentFlow || isSaving.value) return false
  isSaving.value = true
  const saved = await run(async () => {
    if (!flowsStore.currentFlow!.id) {
      const flow = await flowsStore.createFlow(flowsStore.currentFlow!.name, flowsStore.currentFlow!.description)
      flowsStore.currentFlow!.id = flow.id
      await router.replace(`/editor/${flow.id}`)
    }
    await flowsStore.saveFlow()
  }, 'Flow saved.', 'Could not save the flow. Your changes are still here. Try again.')
  isSaving.value = false
  return saved
}

async function handleDeploy() {
  if (pending.value || isSaving.value || isDeploying.value || isStopping.value) return
  if (isRunning.value) {
    await run(() => flowsStore.stopFlow(), 'Flow stopped.', 'Could not stop the flow. Try again.')
  } else {
    if ((!flowsStore.currentFlow?.id || flowsStore.isDirty) && !await handleSave()) return
    await run(() => flowsStore.deployFlow(), 'Flow deployed.', 'Could not deploy the flow. Check its configuration and try again.')
  }
}
function handleBack() { router.push('/') }
onBeforeRouteLeave(async to => {
  // Session expiry must still redirect to authentication.
  if (to.name === 'login' || !flowsStore.isDirty) return true
  return !!await confirmation.value?.ask('Discard unsaved changes?', 'Your changes have not been saved to this flow.', 'Discard changes', 'Keep editing')
})
function beforeUnload(event: BeforeUnloadEvent) {
  if (flowsStore.isDirty) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onUnmounted(() => window.removeEventListener('beforeunload', beforeUnload))

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

</script>

<template>
  <div class="flex h-full flex-col bg-background">
    <!-- Header -->
    <header :inert="pending || flowsStore.loading" class="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b bg-card px-4 py-3">
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <Button variant="ghost" size="icon" aria-label="Back to flows" @click="handleBack">
          <ArrowLeft class="h-4 w-4" />
        </Button>

        <div class="flex items-center gap-3">
          <Workflow class="h-5 w-5 text-primary" />
          <Input
            v-if="flowsStore.currentFlow"
            aria-label="Flow name"
            :model-value="flowsStore.currentFlow.name"
            class="h-8 w-48 border-transparent bg-transparent px-2 text-base font-medium hover:border-input focus:border-input"
            @update:model-value="updateFlowName"
          />
        </div>

        <Badge v-if="flowsStore.isDirty" variant="secondary" class="text-xs">
          Unsaved
        </Badge>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <Badge v-if="flowsStore.currentFlow" :variant="flowsStore.currentFlow.state === 'failed' ? 'destructive' : 'secondary'">{{ isRunning ? 'Running' : flowsStore.currentFlow.state === 'failed' ? 'Failed' : 'Stopped' }}</Badge>

        <Button variant="outline" size="sm" @click="showSettings = true">
          <Settings class="mr-2 h-4 w-4" />
          Settings
        </Button>

        <Button
          variant="outline"
          size="sm"
          :disabled="!flowsStore.isDirty || pending || isSaving"
          @click="handleSave"
        >
          <Save class="mr-2 h-4 w-4" />
          {{ isSaving ? 'Saving...' : 'Save' }}
        </Button>

        <Button
          size="sm"
          :variant="isRunning ? 'destructive' : 'default'"
          :disabled="(!isRunning && flowsStore.invalidConnections.length > 0) || pending || isSaving || isDeploying || isStopping || (!flowId && !flowsStore.currentFlow?.name)"
          @click="handleDeploy"
        >
          <Play v-if="!isRunning" class="mr-2 h-4 w-4" />
          <Square v-else class="mr-2 h-4 w-4" />
          {{ isDeploying ? 'Deploying...' : isStopping ? 'Stopping...' : isRunning ? 'Stop' : 'Deploy' }}
        </Button>
      </div>
    </header>

    <p v-if="flowsStore.invalidConnections.length" role="alert" class="px-4 py-2 text-sm text-destructive">This flow has {{ flowsStore.invalidConnections.length }} invalid connection(s). Open Connections to choose valid ports before deploying.</p>
    <OperationFeedback :message="feedback" class="shrink-0 m-2" @dismiss="feedback = null" />
    <p v-if="flowsStore.loading && !pending" role="status" class="px-4 py-2 text-sm">Loading flow…</p>
    <div v-if="flowsStore.error && !feedback" role="alert" class="px-4 py-2 text-sm text-destructive">Could not load the flow. <Button variant="outline" size="sm" @click="flowId && flowsStore.fetchFlow(flowId)">Retry</Button></div>
    <nav :inert="pending || flowsStore.loading" aria-label="Editor tools" class="flex shrink-0 flex-wrap gap-2 border-b bg-card px-4 py-2">
      <Button class="md:hidden" size="sm" variant="outline" @click="showNodes = true">Add nodes</Button>
      <Button size="sm" variant="outline" @click="showVariables = true">Variables</Button>
      <Button size="sm" variant="outline" @click="showConnections = true">Connections</Button>
      <Button v-if="flowsStore.nodes.some(n => n.data.nodeType === 'trigger-manual' && n.data.enabled !== false)" size="sm" variant="outline" :disabled="!isRunning || flowsStore.isDirty" :title="!isRunning ? 'Deploy the flow first' : flowsStore.isDirty ? 'Stop and save changes before running manually' : 'Send a message through this flow'" @click="showManualRun = true">Run manually</Button>
      <span class="self-center text-xs text-muted-foreground">{{ flowsStore.nodes.length }} nodes · {{ flowsStore.edges.length }} connections</span>
    </nav>
    <!-- Main Editor Area -->
    <div :inert="pending || flowsStore.loading" class="flex min-h-0 flex-1 overflow-hidden">
      <!-- Node Palette -->
      <aside class="hidden w-64 flex-shrink-0 md:block">
        <NodePalette @add="addNode" />
      </aside>

      <!-- Canvas -->
      <main class="min-w-0 flex-1 relative">
        <FlowCanvas ref="canvas" />
      </main>

      <!-- Config Panel (slides in from right) -->
      <NodeConfigPanel />
    </div>

    <!-- Live execution activity (runs history + debug inspector) -->
    <ActivityPanel />

    <ConfirmDialog ref="confirmation" />
    <VariablesDialog v-model:open="showVariables" />
    <ManualRunDialog v-model:open="showManualRun" />
    <ConnectionsDialog v-model:open="showConnections" />
    <Dialog v-model:open="showNodes" title="Add nodes" class="p-0">
      <div class="flex h-[65dvh] flex-col"><div class="flex justify-end p-2"><Button variant="ghost" size="sm" @click="showNodes = false">Done</Button></div><div class="min-h-0 flex-1"><NodePalette @add="addNode" /></div></div>
    </Dialog>
    <!-- Settings Dialog -->
    <Dialog v-model:open="showSettings" title="Flow settings">
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
                aria-label="Flow name"
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
            <p class="text-sm text-muted-foreground">Deploy starts this flow and restores it when PiWorker restarts. Stop it before saving changes. Manual triggers wait for you to send a message.</p>
          </div>

          <div class="flex justify-end gap-2">
            <Button @click="close">Done</Button>
          </div>
        </div>
      </template>
    </Dialog>
  </div>
</template>
