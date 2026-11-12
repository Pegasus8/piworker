<script setup lang="ts">
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuPortal, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from 'radix-vue'
import { MoreHorizontal } from 'lucide-vue-next'
import type { Component } from 'vue'

// An overflow menu of labelled actions (keyboard and screen-reader ready).
export interface MenuAction { label: string; icon?: Component; danger?: boolean; disabled?: boolean; separated?: boolean; run: () => void }
defineProps<{ label: string; actions: MenuAction[] }>()
</script>

<template>
  <DropdownMenuRoot :modal="false">
    <DropdownMenuTrigger :aria-label="label" :title="label"
      class="inline-flex h-9 w-9 items-center justify-center rounded-md hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50">
      <MoreHorizontal class="h-4 w-4" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent align="end" :side-offset="4" class="z-50 min-w-[12rem] rounded-md border bg-card p-1 text-sm shadow-lg">
        <template v-for="action in actions" :key="action.label">
          <DropdownMenuSeparator v-if="action.separated" class="my-1 h-px bg-border" />
          <DropdownMenuItem :disabled="action.disabled"
            class="flex cursor-pointer select-none items-center gap-2 rounded px-2 py-1.5 outline-none data-[disabled]:pointer-events-none data-[highlighted]:bg-accent data-[disabled]:opacity-50"
            :class="action.danger ? 'text-destructive' : ''" @select="action.run()">
            <component :is="action.icon" v-if="action.icon" class="h-4 w-4" aria-hidden="true" />
            {{ action.label }}
          </DropdownMenuItem>
        </template>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
