<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { AlertTriangle } from 'lucide-vue-next'
import { messages as m } from '@/i18n'

// Plain HTTP: the full warning until it is acknowledged on this browser,
// then a one-line reminder that still links to the HTTPS set-up.
const isHttp = window.location.protocol === 'http:'
const KEY = 'piworker-http-warning-acknowledged'
function read() { try { return localStorage.getItem(KEY) === '1' } catch { return false } }
const acknowledged = ref(read())
function acknowledge() {
  acknowledged.value = true
  try { localStorage.setItem(KEY, '1') } catch { /* private mode: only for this page */ }
}
</script>

<template>
  <aside
    v-if="isHttp && !acknowledged"
    role="note"
    :aria-label="m.http.label"
    class="flex shrink-0 items-start justify-center gap-3 border-b border-amber-400/30 bg-amber-950 px-4 py-3 text-sm text-amber-100"
  >
    <AlertTriangle class="mt-0.5 h-5 w-5 shrink-0" aria-hidden="true" />
    <p class="max-w-4xl">
      <strong>{{ m.http.title }}</strong>
      {{ m.http.body }}
      <RouterLink to="/trust" class="font-semibold underline underline-offset-2">{{ m.http.setUp }}</RouterLink>
    </p>
    <button type="button" class="shrink-0 rounded px-2 py-0.5 text-xs font-semibold ring-1 ring-amber-200/40 hover:bg-amber-900" @click="acknowledge">{{ m.http.acknowledge }}</button>
  </aside>
  <aside
    v-else-if="isHttp"
    role="note"
    :aria-label="m.http.label"
    class="flex shrink-0 items-center justify-center gap-2 border-b border-amber-400/30 bg-amber-950/80 px-4 py-1 text-xs text-amber-100"
  >
    <AlertTriangle class="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
    <span>{{ m.http.short }}</span>
    <RouterLink to="/trust" class="font-semibold underline underline-offset-2">{{ m.http.setUp }}</RouterLink>
  </aside>
</template>
