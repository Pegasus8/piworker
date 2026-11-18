import { expect, test } from 'bun:test'
import { multilineField, referenceMode } from '../src/lib/references'

// The LLM nodes' templates (docs/design/llm.md §3) offer message and
// variable references; prompts are edited on several lines.
test('LLM node templates offer references', () => {
  for (const [type, field] of [['process-llm', 'prompt'], ['process-llm', 'system'], ['process-llm', 'memoryKey'], ['process-llm-classify', 'instructions'],
    ['process-llm-classify', 'input'], ['process-llm-agent', 'prompt'], ['action-llm-forget', 'memoryKey'], ['action-knowledge-add', 'text'],
    ['action-knowledge-add', 'documentId'], ['process-knowledge-search', 'query']]) {
    expect(referenceMode(type, field)).toBe('template')
  }
  expect(referenceMode('process-llm', 'model')).toBeUndefined()
  expect(referenceMode('process-llm', 'connection')).toBeUndefined()
})

test('prompts and long texts are multi-line, other settings are not', () => {
  expect(multilineField('process-llm', 'prompt')).toBe(true)
  expect(multilineField('process-llm', 'system')).toBe(true)
  expect(multilineField('process-llm-agent', 'prompt')).toBe(true)
  expect(multilineField('process-llm-classify', 'instructions')).toBe(true)
  expect(multilineField('action-knowledge-add', 'text')).toBe(true)
  expect(multilineField('process-llm', 'memoryKey')).toBe(false)
  // As before: expressions, conditions and templates by name, set-var's value.
  expect(multilineField('process-transform', 'expression')).toBe(true)
  expect(multilineField('process-template', 'template')).toBe(true)
  expect(multilineField('set-var', 'value')).toBe(true)
  expect(multilineField('action-http', 'url')).toBe(false)
})
