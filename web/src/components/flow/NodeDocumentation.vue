<script setup lang="ts">
import { ref, computed } from 'vue'
import { marked } from 'marked'
import { Button, Dialog, ScrollArea } from '@/components/ui'
import { HelpCircle, X } from 'lucide-vue-next'

interface Props {
  title: string
  documentation: string
}

const props = defineProps<Props>()

const isOpen = ref(false)

// Configure marked for better code block rendering
marked.setOptions({
  breaks: true,
  gfm: true
})

const renderedMarkdown = computed(() => {
  if (!props.documentation) return ''
  return marked(props.documentation) as string
})
</script>

<template>
  <div>
    <!-- Help Button -->
    <Button
      variant="ghost"
      size="icon"
      class="h-8 w-8"
      title="View documentation"
      @click="isOpen = true"
    >
      <HelpCircle class="h-4 w-4" />
    </Button>

    <!-- Documentation Dialog -->
    <Dialog v-model:open="isOpen" class="max-w-2xl max-h-[80vh]">
      <template #default="{ close }">
        <!-- Header -->
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-xl font-semibold">{{ title }}</h2>
          <Button variant="ghost" size="icon" @click="close">
            <X class="h-4 w-4" />
          </Button>
        </div>

        <!-- Markdown Content -->
        <ScrollArea class="max-h-[60vh] pr-4">
          <div
            class="prose prose-sm dark:prose-invert max-w-none
                   prose-headings:mt-4 prose-headings:mb-2 prose-headings:font-semibold
                   prose-h2:text-lg prose-h2:border-b prose-h2:pb-1
                   prose-p:my-2
                   prose-ul:my-2 prose-li:my-0.5
                   prose-table:text-sm prose-th:px-3 prose-th:py-2 prose-td:px-3 prose-td:py-1.5
                   prose-pre:bg-muted prose-pre:p-3 prose-pre:rounded-md prose-pre:overflow-x-auto
                   prose-code:bg-muted prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:text-sm
                   prose-code:before:content-none prose-code:after:content-none"
            v-html="renderedMarkdown"
          />
        </ScrollArea>
      </template>
    </Dialog>
  </div>
</template>
