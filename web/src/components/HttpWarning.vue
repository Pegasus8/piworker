<script setup lang="ts">
import { AlertTriangle } from 'lucide-vue-next'

const isHttp = window.location.protocol === 'http:'
</script>

<template>
  <aside
    v-if="isHttp"
    role="note"
    aria-label="Unencrypted connection"
    class="flex shrink-0 items-start justify-center gap-3 border-b border-amber-400/30 bg-amber-950 px-4 py-3 text-sm text-amber-100"
  >
    <AlertTriangle class="mt-0.5 h-5 w-5 shrink-0" aria-hidden="true" />
    <p class="max-w-4xl">
      <strong>Unencrypted connection (HTTP).</strong>
      Passwords and sessions can be intercepted, even on a local network.
      Use only on a network you trust. Avoid public or shared networks.
    </p>
  </aside>
</template>
