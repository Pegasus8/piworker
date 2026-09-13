<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { Reference, ReferenceMode } from '@/lib/references'
const props = defineProps<{ id: string; modelValue: string; mode: ReferenceMode; options: Reference[]; multiline?: boolean; placeholder?: string; required?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [string] }>()
const field = ref<HTMLInputElement | HTMLTextAreaElement>()
const open = ref(false), explicit = ref(false), caret = ref(0), selected = ref(0)
const token = computed(() => {
 const left = String(props.modelValue).slice(0,caret.value)
 if (props.mode === 'template' || props.mode === 'secret') {
  const match = left.match(/\{\{[^}]*$/)
  return match ? { start:left.length-match[0].length, query:match[0].replace(/^\{\{\s*(vars\.|secret\.)?/, '') } : null
 }
 if (props.mode === 'name') return {start:0,query:left}
 const match = left.match(/vars(?:\.[\w-]*|\["[^"]*)?$|[A-Za-z_][\w.]*$/)
 return match ? {start:left.length-match[0].length,query:match[0].replace(/^vars(?:\.|\[")?/, '')} : null
})
const matches = computed(() => props.options.filter(option => explicit.value || option.label.toLowerCase().startsWith((token.value?.query || '').toLowerCase())).slice(0,30))
const visible = computed(() => !!props.mode && open.value && matches.value.length > 0)
function input(event: Event) {
 const target = event.target as HTMLInputElement
 emit('update:modelValue',target.value)
 caret.value = target.selectionStart || 0; selected.value = -1; explicit.value = false
 open.value = true
}
function show() { caret.value = field.value?.selectionStart ?? props.modelValue.length; explicit.value = true; open.value = true; selected.value = 0; field.value?.focus() }
async function choose(option: Reference) {
 const text = props.modelValue
 const start = token.value?.start ?? caret.value
 let end = field.value?.selectionEnd ?? caret.value
 if ((props.mode === 'template' || props.mode === 'secret') && text.slice(end,end+2) === '}}') end += 2
 if (props.mode === 'expression' && text.slice(end,end+2) === '"]') end += 2
 emit('update:modelValue',text.slice(0,start)+option.value+text.slice(end))
 open.value = false
 await nextTick()
 field.value?.focus();field.value?.setSelectionRange(start+option.value.length,start+option.value.length)
}
function keydown(event: KeyboardEvent) {
 if ((event.ctrlKey || event.metaKey) && event.code === 'Space') {event.preventDefault();show();return}
 if (!visible.value) return
 if (event.key === 'Escape') {event.preventDefault();event.stopPropagation();open.value=false;return}
 if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
  event.preventDefault();selected.value = (selected.value+(event.key==='ArrowDown'?1:-1)+matches.value.length)%matches.value.length
 } else if ((event.key === 'Enter' || event.key === 'Tab') && selected.value >= 0) {
  event.preventDefault();void choose(matches.value[selected.value])
 }
}
</script>
<template>
 <div>
  <component :is="multiline ? 'textarea' : 'input'" :id="id" ref="field" :value="modelValue" :placeholder="placeholder" :aria-required="required" :rows="5" spellcheck="false" autocomplete="off" :aria-autocomplete="mode ? 'list' : undefined" :aria-controls="visible ? `${id}-references` : undefined" :aria-activedescendant="visible && selected >= 0 ? `${id}-reference-${selected}` : undefined" class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" :class="multiline ? 'min-h-24 font-mono' : 'h-10'" @input="input" @keydown="keydown" @click="caret = field?.selectionStart || 0" @blur="open = false" />
  <template v-if="mode">
   <button type="button" class="mt-1 text-xs text-primary hover:underline" :aria-label="`Insert reference in ${id}`" @mousedown.prevent @click="show">Insert reference · Ctrl+Space</button>
   <p class="mt-1 text-xs text-muted-foreground">{{ mode === 'secret' ? 'Secret names only. Stop and redeploy after changing a credential.' : mode === 'name' ? 'Global name shared by all flows. You can also enter a new name.' : 'Global variables and message fields. Use ↑ ↓ and Enter to insert; Esc to dismiss.' }}</p>
   <ul v-if="visible" :id="`${id}-references`" role="listbox" aria-label="References" class="mt-2 max-h-44 overflow-auto rounded-lg border bg-card shadow-lg">
    <li v-for="(option,index) in matches" :id="`${id}-reference-${index}`" :key="option.value" role="option" :aria-label="option.label" :aria-selected="selected === index" class="cursor-pointer px-3 py-2 text-sm hover:bg-muted" :class="selected === index ? 'bg-primary/10' : ''" @mousedown.prevent @click="choose(option)"><span class="font-medium">{{ option.label }}</span><span class="ml-2 text-xs text-muted-foreground">{{ option.group }}</span><code class="block break-all text-xs text-muted-foreground">{{ option.value }}</code></li>
   </ul>
  </template>
 </div>
</template>
