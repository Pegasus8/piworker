<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Badge } from '@/components/ui'
import { ArrowLeft, Download, ExternalLink, Lock, ShieldAlert, ShieldCheck, Terminal } from 'lucide-vue-next'
import { PLATFORMS, TRUST_GUIDES, caFileName, detectPlatform, fingerprintRows, trustUrls, type PlatformId } from '@/lib/trust'
import type { TLSInfo } from '@/types'

const router = useRouter()
const auth = useAuthStore()
const api = useApi()

const caUrl = '/piworker-ca.crt'
const secure = window.location.protocol === 'https:'
const caAvailable = ref<boolean | null>(null)
const info = ref<TLSInfo | null>(null)
const infoError = ref('')
const platform = ref<PlatformId>(detectPlatform(navigator.userAgent, navigator.maxTouchPoints) ?? 'linux')
const guide = computed(() => TRUST_GUIDES[platform.value])
const rows = computed(() => fingerprintRows(info.value?.caFingerprint ?? ''))
const expires = computed(() => info.value?.notAfter ? new Date(info.value.notAfter).toLocaleDateString() : '')
// Without the status (signed out), a missing CA on an HTTP page means TLS is
// off; on an HTTPS page, another certificate (custom, or a proxy) is in use.
const httpsOff = computed(() => info.value ? info.value.mode === 'off' : caAvailable.value === false && !secure)
const customCert = computed(() => info.value ? info.value.mode === 'custom' : caAvailable.value === false && secure)
// Links for command-line tools use the ports PiWorker reports (signed in), or
// this page's own address.
const urls = computed(() => trustUrls(window.location, info.value))
const httpsBase = computed(() => urls.value.httpsBase)
const cliDownload = computed(() => urls.value.caDownload)

onMounted(async () => {
  try {
    const res = await fetch(caUrl, { method: 'HEAD', cache: 'no-store', credentials: 'omit' })
    // A development server answers unknown paths with the app itself.
    caAvailable.value = res.ok && /x509|pem/i.test(res.headers.get('content-type') ?? '')
  } catch {
    caAvailable.value = false
  }
  if (!auth.isAuthenticated) return
  try {
    info.value = await api.getTLSInfo()
  } catch (e: any) {
    infoError.value = e.response?.data?.error || 'Could not read the HTTPS status.'
  }
})

function back() {
  router.push(auth.isAuthenticated ? '/' : '/login')
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-3xl space-y-6">
      <Button variant="ghost" @click="back"><ArrowLeft class="mr-2 h-4 w-4" />Back</Button>

      <header class="space-y-2">
        <h1 class="text-2xl font-bold">Trust this device</h1>
        <p class="text-muted-foreground">
          PiWorker encrypts its connections with its own certificate authority (CA). Trust that CA once on each
          computer or phone you use, and your browser will recognise this PiWorker without warnings.
        </p>
      </header>

      <Card class="space-y-4 p-4 sm:p-6">
        <div class="flex flex-wrap items-center gap-2">
          <Badge v-if="secure" variant="secondary"><Lock class="mr-1 h-3 w-3" />This page uses HTTPS</Badge>
          <Badge v-else variant="destructive"><ShieldAlert class="mr-1 h-3 w-3" />This page uses unencrypted HTTP</Badge>
          <Badge v-if="info" variant="outline">TLS mode: {{ info.mode }}</Badge>
        </div>

        <template v-if="httpsOff">
          <h2 class="text-lg font-semibold">HTTPS is turned off</h2>
          <p class="text-sm text-muted-foreground">
            This PiWorker serves plain HTTP (TLS mode “off”), for example behind your own HTTPS proxy. To let PiWorker
            serve HTTPS with its own CA, run this on the device and open the address it prints:
          </p>
          <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>sudo piworkerctl tls on</code></pre>
          <p class="text-sm text-muted-foreground">Manual installations: set <code>mode = "auto"</code> in the <code>[tls]</code> section of piworker.toml and restart.</p>
        </template>

        <template v-else-if="customCert">
          <h2 class="text-lg font-semibold">Certificate provided by the administrator</h2>
          <p class="text-sm text-muted-foreground">
            This PiWorker uses a certificate configured by its administrator (TLS mode “custom”, or an HTTPS proxy in
            front of it). Trust its issuing CA as you would for any other server; there is no PiWorker CA to download.
          </p>
        </template>

        <template v-else>
          <h2 class="text-lg font-semibold">1. Download the CA certificate</h2>
          <div class="flex flex-wrap gap-2">
            <a :href="caUrl" :download="caFileName()" class="lab-button inline-flex h-10 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90">
              <Download class="mr-2 h-4 w-4" />Download CA certificate
            </a>
            <a :href="caUrl" target="_blank" rel="noopener" class="lab-button inline-flex h-10 items-center rounded-md border border-input px-4 text-sm font-medium hover:bg-accent">
              <ExternalLink class="mr-2 h-4 w-4" />Open in Firefox / Safari
            </a>
          </div>

          <h2 class="text-lg font-semibold">2. Check its fingerprint</h2>
          <div v-if="rows.length" class="space-y-2">
            <p class="text-sm text-muted-foreground">SHA-256 fingerprint of this PiWorker's CA:</p>
            <div class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed sm:text-sm" aria-label="CA fingerprint">
              <div v-for="row in rows" :key="row">{{ row }}</div>
            </div>
            <p v-if="!secure" class="text-sm text-amber-600 dark:text-amber-400">
              This page arrived over HTTP, so someone on the network could have changed it. Also compare with the
              fingerprint shown on the device (below).
            </p>
            <p v-else class="text-sm text-amber-600 dark:text-amber-400">
              Until this device trusts the CA, an impostor on the network could serve this page over HTTPS too. Verify
              the fingerprint against the one printed in the PiWorker console or journal, or by piworkerctl (below).
            </p>
          </div>
          <p v-else-if="infoError" role="alert" class="text-sm text-destructive">{{ infoError }}</p>
          <p v-else-if="!auth.isAuthenticated" class="text-sm text-muted-foreground">
            Sign in to see the fingerprint here. You can always read it on the device itself:
          </p>
          <p class="text-sm text-muted-foreground">On the PiWorker device, the fingerprint is printed at startup and by <code>piworkerctl install</code>:</p>
          <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>sudo journalctl -u piworker | grep "CA fingerprint"
openssl x509 -in /var/lib/piworker/tls/ca.crt -noout -fingerprint -sha256
docker logs piworker 2>&amp;1 | grep "CA fingerprint"</code></pre>
          <p class="text-sm text-muted-foreground">Each guide below shows how to read the fingerprint of the file you downloaded. Do not trust a file whose fingerprint differs.</p>

          <h2 class="text-lg font-semibold">3. Trust it</h2>
          <div class="flex flex-wrap gap-2" role="group" aria-label="Choose your system">
            <Button
              v-for="id in PLATFORMS"
              :key="id"
              size="sm"
              :variant="platform === id ? 'default' : 'outline'"
              :aria-pressed="platform === id"
              @click="platform = id"
            >{{ TRUST_GUIDES[id].label }}</Button>
          </div>
          <section class="space-y-4" :aria-label="guide.label + ' instructions'">
            <div>
              <h3 class="mb-2 font-semibold">Install on {{ guide.label }}</h3>
              <ol class="list-decimal space-y-2 pl-5 text-sm">
                <li v-for="(step, i) in guide.install" :key="i" class="min-w-0">
                  {{ step.text }}
                  <pre v-if="step.code" class="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs"><code>{{ step.code }}</code></pre>
                </li>
              </ol>
            </div>
            <div>
              <h3 class="mb-2 font-semibold">Check the fingerprint</h3>
              <ul class="list-disc space-y-2 pl-5 text-sm">
                <li v-for="(step, i) in guide.verify" :key="i" class="min-w-0">
                  {{ step.text }}
                  <pre v-if="step.code" class="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs"><code>{{ step.code }}</code></pre>
                </li>
              </ul>
            </div>
            <div>
              <h3 class="mb-2 font-semibold">Remove the trust later</h3>
              <ul class="list-disc space-y-2 pl-5 text-sm">
                <li v-for="(step, i) in guide.remove" :key="i" class="min-w-0">
                  {{ step.text }}
                  <pre v-if="step.code" class="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs"><code>{{ step.code }}</code></pre>
                </li>
              </ul>
            </div>
          </section>
        </template>
      </Card>

      <Card v-if="info && info.mode !== 'off'" class="space-y-2 p-4 sm:p-6">
        <h2 class="text-lg font-semibold"><ShieldCheck class="mr-1 inline h-5 w-5" />Server certificate</h2>
        <p v-if="info.warning" role="alert" class="text-sm text-amber-600 dark:text-amber-400">{{ info.warning }}</p>
        <p class="text-sm text-muted-foreground">Valid until {{ expires }} for these names and addresses:</p>
        <ul class="flex flex-wrap gap-2">
          <li v-for="name in info.sans" :key="name"><Badge variant="outline">{{ name }}</Badge></li>
        </ul>
        <p v-if="info.mode === 'auto'" class="text-sm text-muted-foreground">
          PiWorker renews this certificate by itself, 30 days before it expires, and within 10 minutes when the
          device gets a new address or name. You only trust the CA once; it is valid for 10 years. To add a name
          (for example one from your router's DNS), list it under <code>hosts</code> in the <code>[tls]</code> section.
        </p>
      </Card>

      <Card v-if="!httpsOff && !customCert" class="space-y-3 p-4 sm:p-6">
        <h2 class="text-lg font-semibold"><Terminal class="mr-1 inline h-5 w-5" />Command-line tools and Claude Code</h2>
        <p class="text-sm text-muted-foreground">
          Tools such as curl and Node.js (Claude Code) do not always use the system trust. Save the CA, check its
          fingerprint, and point them at it:
        </p>
        <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ cliDownload }}
openssl x509 -in ~/{{ caFileName() }} -noout -fingerprint -sha256
curl --cacert ~/{{ caFileName() }} {{ httpsBase }}/api/health
# Claude Code: add to your shell profile, then start claude
export NODE_EXTRA_CA_CERTS=~/{{ caFileName() }}</code></pre>
      </Card>

      <Card class="space-y-2 p-4 sm:p-6">
        <h2 class="text-lg font-semibold">Encryption and trust</h2>
        <p class="text-sm text-muted-foreground">
          HTTPS always encrypts the connection, even before you trust the CA. Until you do, your browser cannot tell
          this PiWorker from an impostor on the same network, so it shows a warning. After trusting the CA, a warning
          means something is wrong: do not continue.
        </p>
        <p class="text-sm text-muted-foreground">
          A trusted CA can vouch for any website on that device. PiWorker stores its CA key only in its data directory,
          but flows run as the PiWorker user: anyone who can create command flows in PiWorker, or read its data or
          backups, can copy the key and impersonate any website to devices that trust this CA. Protect PiWorker
          accounts, the device and its backups, and remove the trust when you stop using PiWorker. If the key may have
          been exposed, create a new CA with <code>piworker -tls-regenerate-ca</code> and trust the new one.
        </p>
      </Card>
    </div>
  </main>
</template>
