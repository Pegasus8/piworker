<script setup lang="ts">
// Shows a new token once, with set-up snippets for this PiWorker
// (docs/design/mcp.md §12). It closes only through "Done", so a stray click or
// Escape cannot lose the token; the parent then drops the plaintext.
import { computed, ref, watch } from 'vue'
import { Copy, KeyRound } from 'lucide-vue-next'
import { Button, Dialog } from '@/components/ui'
import {
  clientSnippets, DESKTOP_COMMAND_NOTE, DESKTOP_CONFIG_PATHS, DESKTOP_PLAIN_HTTP_NOTE, TOKEN_FILE_WINDOWS_SHOWN,
  type CreatedAgentToken, type MCPStatus, type SnippetTLS
} from '@/lib/agentAccess'
import { caFileName } from '@/lib/trust'

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
        <h3 class="font-semibold">Claude Code</h3>
        <template v-if="snippets.needsCA">
          <p class="text-sm text-muted-foreground">
            1. Save PiWorker's CA certificate as ~/{{ caFileName() }} (see
            <a href="/trust" target="_blank" rel="noopener" class="text-primary underline-offset-2 hover:underline">Trust this device</a>,
            opens in a new tab) and check it before trusting it:
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Check the CA certificate" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.caCheck }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.caCheck, 'Command')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the command</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            <template v-if="expectedFingerprint">It must print this fingerprint (after “Fingerprint=”):
              <code aria-label="Expected CA fingerprint" class="break-all font-mono text-xs">{{ expectedFingerprint }}</code>.</template>
            <template v-else>Compare it with the fingerprint shown on Trust this device.</template>
            If it differs, stop: the file is not this PiWorker's CA.
          </p>
          <p class="text-sm text-muted-foreground">2. Add PiWorker to Claude Code:</p>
        </template>
        <div class="flex items-start gap-2">
          <pre aria-label="Claude Code commands" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeCode }}</code></pre>
          <Button variant="ghost" size="sm" @click="copy(snippets.claudeCode, 'Command')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the command</span></Button>
        </div>
        <template v-if="snippets.claudeStart">
          <p class="text-sm text-muted-foreground">
            3. Start Claude Code trusting the CA for that command only. Do not export NODE_EXTRA_CA_CERTS in your shell
            profile: every Node.js program would then trust this CA for every website.
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Start Claude Code" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeStart }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeStart, 'Command')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the command</span></Button>
          </div>
        </template>
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
        <p class="text-sm text-muted-foreground">
          Claude Desktop connects through the piworker-mcp bridge, installed on the computer that runs Claude Desktop.
        </p>
        <template v-if="desktopOS === 'windows'">
          <p class="text-sm text-muted-foreground">
            In PowerShell, save the token in a file only you can read ({{ TOKEN_FILE_WINDOWS_SHOWN }}): run this and paste
            the token when asked (it is masked and stays out of the history).
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Save the token to a file (PowerShell)" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFileWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFileWindows, 'Commands')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the commands</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            Then add this to {{ DESKTOP_CONFIG_PATHS.windows }} and restart Claude Desktop. {{ DESKTOP_COMMAND_NOTE }}
            In the example, YOU is your Windows user name; backslashes are doubled in JSON.
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Claude Desktop configuration (Windows)" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktopWindows }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktopWindows, 'Configuration')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the configuration</span></Button>
          </div>
        </template>
        <template v-else>
          <p class="text-sm text-muted-foreground">
            Save the token in a file only you can read: run this and paste the token when asked (it is not shown and
            stays out of your shell history).
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Save the token to a file" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.tokenFile }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.tokenFile, 'Command')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the command</span></Button>
          </div>
          <p class="text-sm text-muted-foreground">
            Then add this to claude_desktop_config.json (on macOS: {{ DESKTOP_CONFIG_PATHS.macos }}) and restart Claude
            Desktop. {{ DESKTOP_COMMAND_NOTE }}
          </p>
          <div class="flex items-start gap-2">
            <pre aria-label="Claude Desktop configuration" class="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ snippets.claudeDesktop }}</code></pre>
            <Button variant="ghost" size="sm" @click="copy(snippets.claudeDesktop, 'Configuration')"><Copy class="h-4 w-4" /><span class="sr-only">Copy the configuration</span></Button>
          </div>
        </template>
        <p v-if="snippets.desktopPlainHttp" role="note" class="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {{ DESKTOP_PLAIN_HTTP_NOTE }}
        </p>
      </section>

      <p class="text-xs text-muted-foreground">
        Agents connect to {{ snippets.url }}.<template v-if="mcp.notice"> {{ mcp.label }}: {{ mcp.detail }}</template>
      </p>

      <div class="flex justify-end">
        <Button @click="emit('done')">Done, I saved it</Button>
      </div>
    </div>
  </Dialog>
</template>
