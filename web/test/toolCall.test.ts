import { expect, test } from 'bun:test'
import { argsTextFromSchema, callErrorText, callResultText, defaultArgs, parseToolArgs } from '../src/lib/toolCall'

const schema = {
  type: 'object',
  required: ['door'],
  properties: {
    door: { type: 'string', enum: ['left', 'right'] },
    seconds: { type: 'integer', minimum: 1, maximum: 60, default: 10 },
    loud: { type: 'boolean', default: false },
    label: { type: 'string', maxLength: 32 },
    opts: {
      type: 'object',
      properties: { speed: { type: 'number', default: 0.5 }, mode: { type: 'string' } }
    },
    empty: { type: 'object', properties: { x: { type: 'string' } } }
  }
}

test('arguments are prefilled from the schema defaults', () => {
  expect(defaultArgs(schema)).toEqual({ seconds: 10, loud: false, opts: { speed: 0.5 } })
  expect(argsTextFromSchema(schema)).toBe(JSON.stringify({ seconds: 10, loud: false, opts: { speed: 0.5 } }, null, 2))
})

test('a missing, text or odd schema prefills nothing', () => {
  expect(defaultArgs(undefined)).toEqual({})
  expect(defaultArgs('{"type":"object"}')).toEqual({})
  expect(defaultArgs({ type: 'object', properties: [] })).toEqual({})
  expect(argsTextFromSchema(null)).toBe('{}')
})

test('a schema saved as text is read too', () => {
  expect(defaultArgs(JSON.stringify(schema))).toEqual({ seconds: 10, loud: false, opts: { speed: 0.5 } })
})

test('the arguments are one JSON object; blank means none', () => {
  expect(parseToolArgs('')).toEqual({ value: {} })
  expect(parseToolArgs('  ')).toEqual({ value: {} })
  expect(parseToolArgs('{"door":"left"}')).toEqual({ value: { door: 'left' } })
  expect(parseToolArgs('[1]').error).toBeTruthy()
  expect(parseToolArgs('{').error).toContain('Invalid JSON')
})

test('a call result is shown as plain text', () => {
  const text = callResultText({ status: 'completed', runId: 'run-1', result: { moved: true, door: '<b>left</b>' }, durationMs: 12 })
  expect(text).toBe([
    'Status: completed',
    'Run: run-1',
    'Duration: 12 ms',
    'Result:',
    JSON.stringify({ moved: true, door: '<b>left</b>' }, null, 2)
  ].join('\n'))

  expect(callResultText({ status: 'completed_no_reply', runId: 'r', durationMs: 3 })).toContain('The run ended without an Agent Reply')
  expect(callResultText({ status: 'running', runId: 'r', durationMs: 10000 })).toContain('still running')
  expect(callResultText({ status: 'error', runId: 'r', durationMs: 1, error: { nodeId: 'cmd', nodeType: 'action-command', message: 'exit 1' } }))
    .toContain('Error in cmd (action-command): exit 1')
  expect(callResultText({ status: 'error', runId: 'r', durationMs: 1, result: { reason: 'jammed' } })).toContain('"reason": "jammed"')
})

test('a refused call explains why', () => {
  const invalid = { response: { status: 400, data: { success: false, error: 'the arguments do not match the tool\'s input schema', data: { code: 'invalid_arguments', issues: [{ pointer: '/door', message: 'must be one of left, right' }, { pointer: '', message: 'missing door' }] } } } }
  expect(callErrorText(invalid)).toBe([
    'the arguments do not match the tool\'s input schema',
    '/door: must be one of left, right',
    '(root): missing door'
  ].join('\n'))

  const cooldown = { response: { status: 429, data: { success: false, error: 'the tool is cooling down', data: { code: 'cooldown', retryAfterMs: 1500 } } } }
  expect(callErrorText(cooldown)).toBe('the tool is cooling down\nTry again in 2 s.')

  expect(callErrorText({ response: { status: 404, data: { success: false, error: 'unknown tool "f/x"' } } })).toBe('unknown tool "f/x"')
  expect(callErrorText(new Error('Network Error'))).toBe('The call failed: Network Error')
  expect(callErrorText('weird')).toBe('The call failed.')
})
