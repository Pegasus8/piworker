<script setup lang="ts">
// Shows a new token once, with set-up snippets for this PiWorker
// (docs/design/mcp.md §12). It closes only through "Done", so a stray click or
// Escape cannot lose the token; the parent then drops the plaintext.
import { computed, ref, watch } from 'vue'
import { Copy, KeyRound } from 'lucide-vue-next'
import { Button, Dialog } from '@/components/ui'
import {
  clientSnippets, DESKTOP_CONFIG_PATHS, TOKEN_FILE_WINDOWS_SHOWN,
  type CreatedAgentToken, type MCPStatus, type SnippetTLS
} from '@/lib/agentAccess'
import { caFileName } from '@/lib/trust'
import { messages as m } from '@/i18n'

const props = defineProps<{ created: CreatedAgentToken | null; tls: SnippetTLS | null; mcp: MCPStatus }>()
const emit = defineEmits<{ done: [] }>()
const copied = ref('')

const snippets = computed(() => props.created
  ? clientSnippets({ token: props.created.token, location: window.location, tls: props.tls })
  : null)
watch(() => props.created, () => { copied.value = '' })
// What openssl prints after "SHA256 Fingerprint=" for this PiWorker's CA.
const expectedFingerprint = computed(() => props.tls?.caFingerprint.replace(/^SHA256:/i, '') ?? '')

// The Claude Desktop set-up differs on Windows (PowerShell, icacls, paths);
// start with the system the editor runs on.
type DesktopOS = 'posix' | 'windows'
const desktopSystems: { value: DesktopOS; label: string }[] = [{ value: 'posix', label: 'macOS / Linux' }, { value: 'windows', label: 'Windows' }]
const desktopOS = ref<DesktopOS>(/Windows/i.test(navigator.userAgent) ? 'windows' : 'posix')

async function copy(text: string, what: string) {
  let ok = false
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      ok = true
    }
  } catch { /* fall back below */ }
  if (!ok) {
    // Plain HTTP pages have no clipboard API.
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    try { ok = document.execCommand('copy') } catch { ok = false }
    area.remove()
  }
  copied.value = ok ? m.value.agents.secret.copied(what) : m.value.agents.secret.copyBlocked
}
</script>

<template>
  <Dialog :title="m.agents.secret.dialogTitle" class="max-w-2xl" :open="!!created" @update:open="() => {}">
    <div v-if="created && snippets" class="space-y-5">
      <div class="flex items-start gap-2">
        <KeyRound class="mt-1 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
        <div>
          <h2 class="text-lg font-semibold">{{ m.agents.secret.title }}</h2>
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.onlyTime }}<span class="break-words">{{ created.info.name }}</span>{{ m.agents.secret.onlyTimeEnd }}
          </p>
        </div>
      </div>

      <div class="space-y-2">
        <div class="flex items-stretch gap-2">
          <code :aria-label="m.agents.secret.newToken" class="min-w-0 flex-1 select-all break-all rounded-md bg-muted p-3 font-mono text-xs sm:text-sm">{{ created.token }}</code>
          <Button variant="outline" :aria-label="m.agents.secret.copyToken" @click="copy(created.token, m.agents.secret.what.token)"><Copy class="h-4 w-4" /></Button>
        </div>
        <p v-if="copied" role="status" class="text-sm text-muted-foreground">{{ copied }}</p>
      </div>

      <section class="space-y-2">
        <h3 class="font-semibold">Claude Code</h3>
        <template v-if="snippets.needsCA">
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.saveCa(caFileName()) }}
            <a href="/trust" target="_blank" rel="noopener" class="text-primary underline-offset-2 hover:underline">{{ m.agents.secret.trustLink }}</a>{{ m.agents.secret.checkCa }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.checkCaLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.caCheck }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.caCheck, m.agents.secret.what.command)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyCommand }}</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            <template v-if="expectedFingerprint">{{ m.agents.secret.mustPrint }}
              <code :aria-label="m.agents.secret.expectedFingerprint" class="break-all font-mono text-xs">{{ expectedFingerprint }}</code>.</template>
            <template v-else>{{ m.agents.secret.compare }}</template>
            {{ m.agents.secret.differs }}
          </p>
          <p class="text-sm text-muted-foreground">{{ m.agents.secret.addToCode }}</p>
        </template>
        <div class="flex items-start gap-2">
          <pre :aria-label="m.agents.secret.codeCommands" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeCode }}</code></pre>
          <Button variant="ghost" size="sm" @click="copy(snippets.claudeCode, m.agents.secret.what.command)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyCommand }}</span></Button>
        </div>
        <template v-if="snippets.claudeStart">
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.startCode }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.startCodeLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeStart }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeStart, m.agents.secret.what.command)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyCommand }}</span></Button>
          </div>
        </template>
      </section>

      <section class="space-y-2">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 class="font-semibold">Claude Desktop</h3>
          <div role="group" :aria-label="m.agents.secret.operatingSystem" class="inline-flex rounded-md border p-0.5 text-xs">
            <button v-for="os in desktopSystems" :key="os.value" type="button" :aria-pressed="desktopOS === os.value"
              class="rounded px-2 py-1" :class="desktopOS === os.value ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'"
              @click="desktopOS = os.value">{{ os.label }}</button>
          </div>
        </div>
        <p class="text-sm text-muted-foreground">
          {{ m.agents.secret.bridge }}
        </p>
        <template v-if="desktopOS === 'windows'">
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.saveWindows(TOKEN_FILE_WINDOWS_SHOWN) }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.saveWindowsLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFileWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFileWindows, m.agents.secret.what.commands)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyCommands }}</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.addWindows(DESKTOP_CONFIG_PATHS.windows) }} {{ m.agents.access.desktopCommandNote }}
            {{ m.agents.secret.youNote }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.desktopWindowsLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktopWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktopWindows, m.agents.secret.what.configuration)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyConfiguration }}</span></Button>
          </div>
        </template>
        <template v-else>
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.savePosix }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.savePosixLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFile }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFile, m.agents.secret.what.command)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyCommand }}</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            {{ m.agents.secret.addPosix(DESKTOP_CONFIG_PATHS.macos) }} {{ m.agents.access.desktopCommandNote }}
          </p>
          <div class="flex items-start gap-2">
            <pre :aria-label="m.agents.secret.desktopLabel" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktop }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktop, m.agents.secret.what.configuration)"><Copy class="h-4 w-4" /><span class="sr-only">{{ m.agents.secret.copyConfiguration }}</span></Button>
          </div>
        </template>
        <p v-if="snippets.desktopPlainHttp" role="note" class="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {{ m.agents.access.desktopPlainHttpNote }}
        </p>
      </section>

      <p class="text-xs text-muted-foreground">
        {{ m.agents.secret.connectsTo(snippets.url) }}<template v-if="mcp.notice"> {{ mcp.label }}: {{ mcp.detail }}</template>
      </p>

      <div class="flex justify-end">
        <Button @click="emit('done')">{{ m.agents.secret.done }}</Button>
      </div>
    </div>
  </Dialog>
</template>
