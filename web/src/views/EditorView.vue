<script setup lang="ts">
import VariablesDialog from '@/components/VariablesDialog.vue'
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { useFlowsStore, FlowVersionConflictError } from '@/stores/flows'
import { useObservabilityStore } from '@/stores/observability'
import { FlowCanvas, NodePalette, NodeConfigPanel, ActivityPanel } from '@/components/flow'
import { Button, Input, Badge, Dialog } from '@/components/ui'
import {
  ArrowLeft,
  Save,
  Play,
  Square,
  Settings,
  Workflow,
  History,
  Undo2,
  Redo2
} from 'lucide-vue-next'
import { messages as m } from '@/i18n'

import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { deployWarningsSummary } from '@/lib/deployWarnings'
import type { DeployWarning } from '@/lib/deployWarnings'
import OperationFeedback from '@/components/ui/OperationFeedback.vue'
import ManualRunDialog from '@/components/flow/ManualRunDialog.vue'
import ConnectionsDialog from '@/components/flow/ConnectionsDialog.vue'
import VersionHistoryDialog from '@/components/flow/VersionHistoryDialog.vue'
import FlowMarks from '@/components/agent/FlowMarks.vue'
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
// Version history and rollback to the version you last approved; a rollback
// reloads the flow (the dialog warns that unsaved changes are discarded).
const showHistory = ref(false)
const isSaving = ref(false)

const flowId = computed(() => route.params.id as string | undefined)
const isDeploying = computed(() => flowsStore.isDeploying)
const isStopping = computed(() => flowsStore.isStopping)
const isRunning = computed(() => flowsStore.currentFlow?.running ?? false)

onMounted(async () => {
  if (flowId.value) {
    await flowsStore.fetchFlow(flowId.value)
  } else {
    flowsStore.initNewFlow(typeof route.query.name === 'string' ? route.query.name : undefined)
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
  let conflict: FlowVersionConflictError | null = null
  const saved = await run(async () => {
    if (!flowsStore.currentFlow!.id) {
      const flow = await flowsStore.createFlow(flowsStore.currentFlow!.name, flowsStore.currentFlow!.description)
      flowsStore.currentFlow!.id = flow.id
      flowsStore.currentFlow!.version = flow.version
      await router.replace(`/editor/${flow.id}`)
    }
    try {
      await flowsStore.saveFlow()
    } catch (e) {
      if (e instanceof FlowVersionConflictError) conflict = e
      throw e
    }
  }, m.value.editor.saved, m.value.editor.saveFailed)
  isSaving.value = false
  if (conflict) await offerReload(conflict, 'saved')
  return saved
}

// A save never silently overwrites, and a deploy never runs, a version saved
// since this editor loaded the flow (an approved agent draft, another tab):
// offer to reload it, which discards the changes made here, or to keep
// editing.
async function offerReload(conflict: FlowVersionConflictError, action: 'saved' | 'deployed') {
  const t = m.value.editor
  feedback.value = { kind: 'error', text: action === 'saved' ? t.conflictNotSaved(conflict.currentVersion) : t.conflictNotDeployed(conflict.currentVersion) }
  const reload = await confirmation.value?.ask(
    t.conflictTitle,
    action === 'saved' ? t.conflictSaveBody(conflict.currentVersion) : t.conflictDeployBody(conflict.currentVersion),
    t.reload, m.value.common.cancel)
  if (reload && flowId.value) {
    flowsStore.isDirty = false
    await flowsStore.fetchFlow(flowId.value)
    feedback.value = { kind: 'success', text: m.value.editor.reloaded(flowsStore.currentFlow?.version ?? conflict.currentVersion) }
  }
}

async function handleDeploy() {
  if (pending.value || isSaving.value || isDeploying.value || isStopping.value) return
  if (isRunning.value) {
    await run(() => flowsStore.stopFlow(), m.value.editor.stopped, m.value.editor.stopFailed)
  } else {
    if ((!flowsStore.currentFlow?.id || flowsStore.isDirty) && !await handleSave()) return
    let conflict: FlowVersionConflictError | null = null
    let warnings: DeployWarning[] = []
    const deployed = await run(async () => {
      try {
        warnings = await flowsStore.deployFlow()
      } catch (e) {
        if (e instanceof FlowVersionConflictError) conflict = e
        throw e
      }
    }, m.value.editor.deployed, m.value.editor.deployFailed)
    // Deployed, with something to know (an undefined secret an optional
    // credential uses): the warning stays until dismissed.
    if (deployed && warnings.length) feedback.value = { kind: 'warning', text: deployWarningsSummary(warnings, nodeName) }
    if (conflict) await offerReload(conflict, 'deployed')
  }
}
// A node's name in messages: its type's name and its ID.
function nodeName(id: string) {
  const n = flowsStore.nodes.find(node => node.id === id)
  return n ? `${n.data.label} (${id})` : id
}
function handleBack() { router.push('/') }
onBeforeRouteLeave(async to => {
  // Session expiry must still redirect to authentication.
  if (to.name === 'login' || !flowsStore.isDirty) return true
  return !!await confirmation.value?.ask(m.value.editor.discardTitle, m.value.editor.discardBody, m.value.editor.discard, m.value.editor.keepEditing)
})
// Ctrl/Cmd+S saves; Ctrl/Cmd+Z and Ctrl/Cmd+Shift+Z (or Ctrl+Y) undo and
// redo canvas changes, except while typing in a field (the field's own undo).
function typing(target: EventTarget | null) {
  const el = target as HTMLElement | null
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}
function onKeydown(event: KeyboardEvent) {
  if (!(event.ctrlKey || event.metaKey) || event.altKey) return
  if (document.querySelector('dialog[open]')) return
  const key = event.key.toLowerCase()
  if (key === 's') {
    event.preventDefault()
    if (flowsStore.isDirty) void handleSave()
  } else if (!typing(event.target) && key === 'z') {
    event.preventDefault()
    if (event.shiftKey) flowsStore.redo(); else flowsStore.undo()
  } else if (!typing(event.target) && key === 'y') {
    event.preventDefault()
    flowsStore.redo()
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
const mod = isMac ? '⌘' : 'Ctrl+'
// An empty flow has nothing to run.
const deployBlocked = computed(() => !isRunning.value && (flowsStore.invalidConnections.length > 0 || flowsStore.nodes.length === 0))
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
        <Button variant="ghost" size="icon" :aria-label="m.editor.back" :title="m.editor.back" @click="handleBack">
          <ArrowLeft class="h-4 w-4" />
        </Button>

        <div class="flex items-center gap-3">
          <Workflow class="h-5 w-5 text-primary" aria-hidden="true" />
          <Input
            v-if="flowsStore.currentFlow"
            :aria-label="m.editor.flowName"
            :model-value="flowsStore.currentFlow.name"
            class="h-8 w-48 border-transparent bg-transparent px-2 text-base font-medium hover:border-input focus:border-input"
            @update:model-value="updateFlowName"
          />
        </div>

        <Badge v-if="flowsStore.isDirty" variant="secondary" class="text-xs">
          {{ m.editor.unsaved }}
        </Badge>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <Badge v-if="flowsStore.currentFlow" :variant="flowsStore.currentFlow.state === 'failed' ? 'destructive' : isRunning ? 'default' : 'secondary'">{{ isRunning ? m.editor.running : flowsStore.currentFlow.state === 'failed' ? m.editor.failed : m.editor.stoppedState }}</Badge>
        <FlowMarks v-if="flowsStore.currentFlow?.id" :flow="flowsStore.currentFlow" />
        <span v-if="flowsStore.currentFlow?.version" class="text-xs text-muted-foreground">{{ m.editor.version(flowsStore.currentFlow.version) }}</span>

        <Button variant="outline" size="sm" @click="showSettings = true">
          <Settings class="mr-2 h-4 w-4" />
          {{ m.editor.settings }}
        </Button>

        <Button
          variant="outline"
          size="sm"
          :title="`${m.editor.save} (${mod}S)`"
          :disabled="!flowsStore.isDirty || pending || isSaving"
          @click="handleSave"
        >
          <Save class="mr-2 h-4 w-4" />
          {{ isSaving ? m.editor.saving : m.editor.save }}
        </Button>

        <Button
          size="sm"
          :variant="isRunning ? 'destructive' : 'default'"
          :title="!isRunning && flowsStore.nodes.length === 0 ? m.editor.deployEmpty : undefined"
          :disabled="deployBlocked || pending || isSaving || isDeploying || isStopping || (!flowId && !flowsStore.currentFlow?.name)"
          @click="handleDeploy"
        >
          <Play v-if="!isRunning" class="mr-2 h-4 w-4" />
          <Square v-else class="mr-2 h-4 w-4" />
          {{ isDeploying ? m.editor.deploying : isStopping ? m.editor.stopping : isRunning ? m.editor.stop : m.editor.deploy }}
        </Button>
      </div>
    </header>

    <p v-if="flowsStore.invalidConnections.length" role="alert" class="px-4 py-2 text-sm text-destructive">{{ m.editor.invalidConnections(flowsStore.invalidConnections.length) }}</p>
    <OperationFeedback floating :message="feedback" @dismiss="feedback = null" />
    <p v-if="flowsStore.loading && !pending" role="status" class="px-4 py-2 text-sm">{{ m.editor.loading }}</p>
    <div v-if="flowsStore.error && !feedback" role="alert" class="px-4 py-2 text-sm text-destructive">{{ m.editor.loadFailed }} <Button variant="outline" size="sm" @click="flowId && flowsStore.fetchFlow(flowId)">{{ m.common.retry }}</Button></div>
    <nav :inert="pending || flowsStore.loading" :aria-label="m.editor.tools" class="flex shrink-0 flex-wrap items-center gap-2 border-b bg-card px-4 py-2">
      <Button class="md:hidden" size="sm" variant="outline" @click="showNodes = true">{{ m.editor.addNodes }}</Button>
      <div class="flex items-center">
        <Button size="icon" variant="ghost" class="h-8 w-8" :disabled="!flowsStore.canUndo" :aria-label="m.editor.undo" :title="`${m.editor.undo} (${mod}Z)`" @click="flowsStore.undo()"><Undo2 class="h-4 w-4" /></Button>
        <Button size="icon" variant="ghost" class="h-8 w-8" :disabled="!flowsStore.canRedo" :aria-label="m.editor.redo" :title="`${m.editor.redo} (${mod}${isMac ? '⇧Z' : 'Y'})`" @click="flowsStore.redo()"><Redo2 class="h-4 w-4" /></Button>
      </div>
      <Button size="sm" variant="outline" :title="m.editor.flowVariablesHint" @click="showVariables = true">{{ m.editor.flowVariables }}</Button>
      <Button size="sm" variant="outline" :title="m.editor.connectionsHint" @click="showConnections = true">{{ m.editor.connections }}</Button>
      <Button v-if="flowId" size="sm" variant="outline" @click="showHistory = true"><History class="mr-2 h-4 w-4" aria-hidden="true" />{{ m.editor.history }}</Button>
      <Button v-if="flowsStore.nodes.some(n => n.data.nodeType === 'trigger-manual' && n.data.enabled !== false)" size="sm" variant="outline" :disabled="!isRunning || flowsStore.isDirty" :title="!isRunning ? m.editor.runManualNotDeployed : flowsStore.isDirty ? m.editor.runManualDirty : m.editor.runManualHint" @click="showManualRun = true">{{ m.editor.runManually }}</Button>
      <span class="self-center text-xs text-muted-foreground">{{ m.editor.counts(flowsStore.nodes.length, flowsStore.edges.length) }}</span>
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

      <!-- Settings of the selected node, beside the canvas -->
      <NodeConfigPanel />
    </div>

    <!-- Live execution activity (runs history + debug inspector) -->
    <ActivityPanel />

    <ConfirmDialog ref="confirmation" />
    <VariablesDialog v-model:open="showVariables" :flow-id="flowsStore.currentFlow?.id || undefined" />
    <ManualRunDialog v-model:open="showManualRun" />
    <ConnectionsDialog v-model:open="showConnections" />
    <VersionHistoryDialog
      v-if="flowId && flowsStore.currentFlow"
      v-model:open="showHistory"
      :flow-id="flowId"
      :flow-name="flowsStore.currentFlow.name"
      :running="isRunning"
      :dirty="flowsStore.isDirty"
    />
    <Dialog v-model:open="showNodes" :title="m.editor.addNodes" class="p-0">
      <div class="flex h-[65dvh] flex-col"><div class="flex justify-end p-2"><Button variant="ghost" size="sm" @click="showNodes = false">{{ m.common.done }}</Button></div><div class="min-h-0 flex-1"><NodePalette @add="addNode" /></div></div>
    </Dialog>
    <!-- Settings Dialog -->
    <Dialog v-model:open="showSettings" :title="m.editor.flowSettings">
      <template #default="{ close }">
        <div class="space-y-4">
          <div>
            <h2 class="text-lg font-semibold">{{ m.editor.flowSettings }}</h2>
          </div>

          <div v-if="flowsStore.currentFlow" class="space-y-4">
            <div>
              <label for="flow-settings-name" class="text-sm font-medium">{{ m.editor.name }}</label>
              <Input
                id="flow-settings-name"
                :aria-label="m.editor.flowName"
                :model-value="flowsStore.currentFlow.name"
                class="mt-1"
                @update:model-value="updateFlowName"
              />
            </div>
            <div>
              <label for="flow-settings-description" class="text-sm font-medium">{{ m.editor.description }}</label>
              <Input
                id="flow-settings-description"
                :model-value="flowsStore.currentFlow.description || ''"
                :placeholder="m.editor.descriptionPlaceholder"
                class="mt-1"
                @update:model-value="updateFlowDescription"
              />
            </div>
            <p class="text-sm text-muted-foreground">{{ m.editor.settingsHint }}</p>
          </div>

          <div class="flex justify-end gap-2">
            <Button @click="close">{{ m.common.done }}</Button>
          </div>
        </div>
      </template>
    </Dialog>
  </div>
</template>
