<script setup lang="ts">
// Shows a new token once, with set-up snippets for this PiWorker
// (docs/design/mcp.md §12). It closes only through "Done", so a stray click or
// Escape cannot lose the token; the parent then drops the plaintext.
import { computed, ref, watch } from 'vue'
import { Copy, KeyRound } from 'lucide-vue-next'
import { Button, Dialog } from '@/components/ui'
import {
  clientSnippets, DESKTOP_CONFIG_PATHS, TOKEN_FILE, TOKEN_FILE_PLACEHOLDER, TOKEN_FILE_PLACEHOLDER_WINDOWS, TOKEN_FILE_WINDOWS,
  type CreatedAgentToken, type MCPStatus, type SnippetTLS
} from '@/lib/agentAccess'

const props = defineProps<{ created: CreatedAgentToken | null; tls: SnippetTLS | null; mcp: MCPStatus }>()
const emit = defineEmits<{ done: [] }>()
const copied = ref('')

const snippets = computed(() => props.created
  ? clientSnippets({ token: props.created.token, location: window.location, tls: props.tls })
  : null)
watch(() => props.created, () => { copied.value = '' })

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
  copied.value = ok ? `${what} copied.` : 'Copying is blocked here: select the text and copy it.'
}
</script>

<template>
  <Dialog title="Copy your new token" class="max-w-2xl" :open="!!created" @update:open="() => {}">
    <div v-if="created && snippets" class="space-y-5">
      <div class="flex items-start gap-2">
        <KeyRound class="mt-1 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
        <div>
          <h2 class="text-lg font-semibold">Copy your token now</h2>
          <p class="text-sm text-muted-foreground">
            This is the only time PiWorker shows it. Store it like a password: anyone who has it can use the access of
            “<span class="break-words">{{ created.info.name }}</span>”.
          </p>
        </div>
      </div>

      <div class="space-y-2">
        <div class="flex items-stretch gap-2">
          <code aria-label="New token" class="min-w-0 flex-1 select-all break-all rounded-md bg-muted p-3 font-mono text-xs sm:text-sm">{{ created.token }}</code>
          <Button variant="outline" aria-label="Copy token" @click="copy(created.token, 'Token')"><Copy class="h-4 w-4" /></Button>
        </div>
        <p v-if="copied" role="status" class="text-sm text-muted-foreground">{{ copied }}</p>
      </div>

      <section class="space-y-2">
        <div class="flex items-center justify-between gap-2">
          <h3 class="font-semibold">Claude Code</h3>
          <Button variant="ghost" size="sm" @click="copy(snippets.claudeCode, 'Commands')"><Copy class="mr-2 h-4 w-4" />Copy</Button>
        </div>
        <p v-if="snippets.needsCA" class="text-sm text-muted-foreground">
          Save PiWorker's CA certificate as ~/piworker-ca.crt first (see
          <a href="/trust" target="_blank" rel="noopener" class="text-primary underline-offset-2 hover:underline">Trust this device</a>,
          opens in a new tab), then run:
        </p>
        <pre aria-label="Claude Code commands" class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeCode }}</code></pre>
      </section>

      <section class="space-y-2">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 class="font-semibold">Claude Desktop</h3>
          <div role="group" aria-label="Operating system" class="inline-flex rounded-md border p-0.5 text-xs">
            <button v-for="os in desktopSystems" :key="os.value" type="button" :aria-pressed="desktopOS === os.value"
              class="rounded px-2 py-1" :class="desktopOS === os.value ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'"
              @click="desktopOS = os.value">{{ os.label }}</button>
          </div>
        </div>
        <template v-if="desktopOS === 'windows'">
          <p class="text-sm text-muted-foreground">
            Claude Desktop connects through the piworker-mcp bridge. In PowerShell, save the token in a file only you
            can read ({{ TOKEN_FILE_WINDOWS }}):
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Save the token to a file (PowerShell)" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFileWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFileWindows, 'Commands')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the commands</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            Then add this to {{ DESKTOP_CONFIG_PATHS.windows }}, replacing {{ TOKEN_FILE_PLACEHOLDER_WINDOWS }} with the
            full path of the file (backslashes doubled in JSON). Put piworker-mcp.exe in your PATH or write its full path
            as the command:
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Claude Desktop configuration (Windows)" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktopWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktopWindows, 'Configuration')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the configuration</span></Button>
          </div>
        </template>
        <template v-else>
          <p class="text-sm text-muted-foreground">
            Claude Desktop connects through the piworker-mcp bridge. Save the token in a file only you can read:
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Save the token to a file" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFile }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFile, 'Command')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the command</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            Then add this to claude_desktop_config.json (on macOS: {{ DESKTOP_CONFIG_PATHS.macos }}), replacing
            {{ TOKEN_FILE_PLACEHOLDER }} with the full path of {{ TOKEN_FILE }} (Claude Desktop does not expand ~):
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Claude Desktop configuration" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktop }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktop, 'Configuration')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the configuration</span></Button>
          </div>
        </template>
      </section>

      <p class="text-xs text-muted-foreground">
        Agents connect to {{ snippets.url }}.<template v-if="mcp.state !== 'on'"> {{ mcp.label }}: {{ mcp.detail }}</template>
      </p>

      <div class="flex justify-end">
        <Button @click="emit('done')">Done, I saved it</Button>
      </div>
    </div>
  </Dialog>
</template>
