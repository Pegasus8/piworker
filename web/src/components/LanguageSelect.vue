<script setup lang="ts">
import { Languages } from 'lucide-vue-next'
import { LOCALES, locale, setLocale, messages as m, type Locale } from '@/i18n'
</script>

<template>
  <label class="flex items-center gap-2 text-sm text-muted-foreground">
    <Languages class="h-4 w-4 shrink-0" aria-hidden="true" />
    <span class="sr-only">{{ m.common.language }}</span>
    <select :value="locale" :aria-label="m.common.language"
      class="h-8 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      @change="setLocale(($event.target as HTMLSelectElement).value as Locale)">
      <option v-for="(name, code) in LOCALES" :key="code" :value="code" :lang="code">{{ name }}</option>
    </select>
  </label>
</template>
