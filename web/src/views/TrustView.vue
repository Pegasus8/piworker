<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useApi } from '@/composables/useApi'
import { Button, Card, Badge } from '@/components/ui'
import { ArrowLeft, Download, ExternalLink, Lock, ShieldAlert, ShieldCheck, Terminal } from 'lucide-vue-next'
import { PLATFORMS, TRUST_GUIDES, caFileName, detectPlatform, fingerprintRows, trustUrls, type PlatformId } from '@/lib/trust'
import type { TLSInfo } from '@/types'
import { messages as m, formatDate } from '@/i18n'

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
const expires = computed(() => info.value?.notAfter ? formatDate(info.value.notAfter) : '')
// Without the status (signed out), a missing CA on an HTTP page means TLS is
// off; on an HTTPS page, another certificate (custom, or a proxy) is in use.
const httpsOff = computed(() => info.value ? info.value.mode === 'off' : caAvailable.value === false && !secure)
const customCert = computed(() => info.value ? info.value.mode === 'custom' : caAvailable.value === false && secure)
// Links for command-line tools use the ports PiWorker reports (signed in), or
// this page's own address and the default ports.
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
    infoError.value = e.response?.data?.error || m.value.trust.statusError
  }
})

function back() {
  router.push(auth.isAuthenticated ? '/' : '/login')
}
</script>

<template>
  <main class="lab-page min-h-full bg-background p-4 sm:p-6">
    <div class="mx-auto max-w-3xl space-y-6">
      <Button variant="ghost" @click="back"><ArrowLeft class="mr-2 h-4 w-4" />{{ m.trust.back }}</Button>

      <header class="space-y-2">
        <h1 class="text-2xl font-bold">{{ m.trust.title }}</h1>
        <p class="text-muted-foreground">{{ m.trust.intro }}</p>
      </header>

      <Card class="space-y-4 p-4 sm:p-6">
        <div class="flex flex-wrap items-center gap-2">
          <Badge v-if="secure" variant="secondary"><Lock class="mr-1 h-3 w-3" />{{ m.trust.pageHttps }}</Badge>
          <Badge v-else variant="destructive"><ShieldAlert class="mr-1 h-3 w-3" />{{ m.trust.pageHttp }}</Badge>
          <Badge v-if="info" variant="outline">{{ m.trust.tlsMode(info.mode) }}</Badge>
        </div>

        <template v-if="httpsOff">
          <h2 class="text-lg font-semibold">{{ m.trust.offTitle }}</h2>
          <p class="text-sm text-muted-foreground">{{ m.trust.offText }}</p>
          <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>sudo piworkerctl tls on</code></pre>
          <p class="text-sm text-muted-foreground">{{ m.trust.offManual[0] }}<code>mode = "auto"</code>{{ m.trust.offManual[1] }}<code>[tls]</code>{{ m.trust.offManual[2] }}</p>
        </template>

        <template v-else-if="customCert">
          <h2 class="text-lg font-semibold">{{ m.trust.customTitle }}</h2>
          <p class="text-sm text-muted-foreground">{{ m.trust.customText }}</p>
        </template>

        <template v-else>
          <h2 class="text-lg font-semibold">{{ m.trust.step1 }}</h2>
          <div class="flex flex-wrap gap-2">
            <a :href="caUrl" :download="caFileName()" class="lab-button inline-flex h-10 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90">
              <Download class="mr-2 h-4 w-4" />{{ m.trust.download }}
            </a>
            <a :href="caUrl" target="_blank" rel="noopener" class="lab-button inline-flex h-10 items-center rounded-md border border-input px-4 text-sm font-medium hover:bg-accent">
              <ExternalLink class="mr-2 h-4 w-4" />{{ m.trust.openIn }}
            </a>
          </div>

          <h2 class="text-lg font-semibold">{{ m.trust.step2 }}</h2>
          <div v-if="rows.length" class="space-y-2">
            <p class="text-sm text-muted-foreground">{{ m.trust.fingerprintOf }}</p>
            <div class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed sm:text-sm" :aria-label="m.trust.fingerprintLabel">
              <div v-for="row in rows" :key="row">{{ row }}</div>
            </div>
            <p v-if="!secure" class="text-sm text-amber-600 dark:text-amber-400">{{ m.trust.overHttp }}</p>
            <p v-else class="text-sm text-amber-600 dark:text-amber-400">{{ m.trust.overHttps }}</p>
          </div>
          <p v-else-if="infoError" role="alert" class="text-sm text-destructive">{{ infoError }}</p>
          <p v-else-if="!auth.isAuthenticated" class="text-sm text-muted-foreground">{{ m.trust.signIn }}</p>
          <p class="text-sm text-muted-foreground">{{ m.trust.onDevice }}<code>piworkerctl install</code>:</p>
          <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>sudo journalctl -u piworker | grep "CA fingerprint"
openssl x509 -in /var/lib/piworker/tls/ca.crt -noout -fingerprint -sha256
docker logs piworker 2>&amp;1 | grep "CA fingerprint"</code></pre>
          <p class="text-sm text-muted-foreground">{{ m.trust.eachGuide }}</p>

          <h2 class="text-lg font-semibold">{{ m.trust.step3 }}</h2>
          <div class="flex flex-wrap gap-2" role="group" :aria-label="m.trust.chooseSystem">
            <Button
              v-for="id in PLATFORMS"
              :key="id"
              size="sm"
              :variant="platform === id ? 'default' : 'outline'"
              :aria-pressed="platform === id"
              @click="platform = id"
            >{{ TRUST_GUIDES[id].label }}</Button>
          </div>
          <section class="space-y-4" :aria-label="m.trust.instructions(guide.label)">
            <div>
              <h3 class="mb-2 font-semibold">{{ m.trust.installOn(guide.label) }}</h3>
              <ol class="list-decimal space-y-2 pl-5 text-sm">
                <li v-for="(step, i) in guide.install" :key="i" class="min-w-0">
                  {{ step.text }}
                  <pre v-if="step.code" class="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs"><code>{{ step.code }}</code></pre>
                </li>
              </ol>
            </div>
            <div>
              <h3 class="mb-2 font-semibold">{{ m.trust.checkFingerprint }}</h3>
              <ul class="list-disc space-y-2 pl-5 text-sm">
                <li v-for="(step, i) in guide.verify" :key="i" class="min-w-0">
                  {{ step.text }}
                  <pre v-if="step.code" class="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs"><code>{{ step.code }}</code></pre>
                </li>
              </ul>
            </div>
            <div>
              <h3 class="mb-2 font-semibold">{{ m.trust.removeLater }}</h3>
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
        <h2 class="text-lg font-semibold"><ShieldCheck class="mr-1 inline h-5 w-5" />{{ m.trust.serverCert }}</h2>
        <p v-if="info.warning" role="alert" class="text-sm text-amber-600 dark:text-amber-400">{{ info.warning }}</p>
        <p class="text-sm text-muted-foreground">{{ m.trust.validUntil(expires) }}</p>
        <ul class="flex flex-wrap gap-2">
          <li v-for="name in info.sans" :key="name"><Badge variant="outline">{{ name }}</Badge></li>
        </ul>
        <p v-if="info.mode === 'auto'" class="text-sm text-muted-foreground">{{ m.trust.renews[0] }}<code>hosts</code>{{ m.trust.renews[1] }}<code>[tls]</code>{{ m.trust.renews[2] }}</p>
      </Card>

      <Card v-if="!httpsOff && !customCert" class="space-y-3 p-4 sm:p-6">
        <h2 class="text-lg font-semibold"><Terminal class="mr-1 inline h-5 w-5" />{{ m.trust.cliTitle }}</h2>
        <p class="text-sm text-muted-foreground">{{ m.trust.cliText }}</p>
        <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{{ cliDownload }}
openssl x509 -in ~/{{ caFileName() }} -noout -fingerprint -sha256
curl --cacert ~/{{ caFileName() }} {{ httpsBase }}/api/health
{{ m.trust.cliClaudeComment }}
NODE_EXTRA_CA_CERTS=~/{{ caFileName() }} claude</code></pre>
      </Card>

      <Card class="space-y-2 p-4 sm:p-6">
        <h2 class="text-lg font-semibold">{{ m.trust.encryptionTitle }}</h2>
        <p class="text-sm text-muted-foreground">{{ m.trust.encryption1 }}</p>
        <p class="text-sm text-muted-foreground">{{ m.trust.encryption2[0] }}<code>piworker -tls-regenerate-ca</code>{{ m.trust.encryption2[1] }}</p>
      </Card>
    </div>
  </main>
</template>
