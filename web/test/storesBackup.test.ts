import { expect, test } from 'bun:test'
import { bundleSummary, importResult, importResultText } from '../src/lib/storesBackup'
import { setLocale } from '../src/i18n'

test('a chosen file is summarised before importing, never its sealed part', () => {
  setLocale('en')
  const file = {
    format: 'piworker-stores', version: 1, createdAt: '2026-11-13T10:00:00Z',
    variables: { a: 1, b: 2 }, flowVariables: [{ flowId: 'f', flowName: 'Heating', variables: { c: 3 } }],
    connections: [{ name: 'home' }], secrets: ['api_key', 7], sealed: { data: 'xxx' }
  }
  const r = bundleSummary(file)
  expect('summary' in r && r.summary).toEqual({
    createdAt: '2026-11-13T10:00:00Z', variables: 2, flowVariables: [{ flowName: 'Heating', count: 1 }],
    connections: ['home'], secrets: ['api_key'], sealed: true
  })
  expect(bundleSummary({ format: 'flow' })).toEqual({ error: expect.stringContaining('not a PiWorker') })
  expect(bundleSummary({ format: 'piworker-stores', version: 9 })).toEqual({ error: expect.stringContaining('9') })
})

test('the import outcome reads as lines and flags what needs attention', () => {
  setLocale('en')
  const ok = importResult({ report: { variables: { added: 2, replaced: 0, skipped: 1 } }, restarted: [{ id: 'x', name: 'Lights' }] })
  const text = importResultText(ok)
  expect(text.kind).toBe('success')
  expect(text.lines[0]).toContain('Global variables: 2 added, 1 kept')
  expect(text.lines.join(' ')).toContain('“Lights”')
  const partial = importResultText(importResult({ report: { secretsNotRestored: ['api_key'], unmatchedFlows: [{ id: 'f', name: 'Gone' }] }, failed: [{ id: 'y', name: 'Heat', error: 'boom' }] }))
  expect(partial.kind).toBe('warning')
  expect(partial.lines.join(' ')).toContain('api_key')
  expect(partial.lines.join(' ')).toContain('Gone')
  expect(partial.lines.join(' ')).toContain('Heat: boom')
  setLocale('es')
  expect(importResultText(importResult({})).lines).toEqual(['El archivo no tenía nada que importar.'])
  setLocale('en')
})

test('Spanish counts agree with the noun', () => {
  setLocale('es')
  const lines = importResultText(importResult({ report: { secrets: { added: 1 }, connections: { added: 2, skipped: 1 } } })).lines
  expect(lines).toContain('Secretos: 1 añadido.')
  expect(lines).toContain('Conexiones a brokers: 2 añadidas, 1 conservada.')
  setLocale('en')
})
