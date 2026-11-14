import { expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

// The health answers in order: the old process, then gone, then the new one.
let answers: (string | null)[] = []
const apiClient = {
  get: async (url: string) => {
    if (url !== '/health') throw new Error('unexpected ' + url)
    const next = answers.length > 1 ? answers.shift() : answers[0]
    if (next === null || next === undefined) throw Object.assign(new Error('down'), { response: undefined })
    return { data: { status: 'healthy', instance: next } }
  }
}
mock.module('../src/composables/useApi', () => ({ apiClient, useApi: () => ({}) }))
const { useBackupsStore } = await import('../src/stores/backups')

const noSleep = async () => {}

test('waitForRestart sees a new instance even when the restart was too quick to notice', async () => {
  setActivePinia(createPinia())
  const store = useBackupsStore()
  answers = ['old']
  expect(await store.instance()).toBe('old')
  answers = ['old', 'new']
  expect(await store.waitForRestart('old', { sleep: noSleep, timeoutMs: 1000 })).toBe(true)
  answers = ['old', null, null, 'new']
  expect(await store.waitForRestart('old', { sleep: noSleep, timeoutMs: 1000 })).toBe(true)
})

test('waitForRestart gives up when the same process keeps answering', async () => {
  setActivePinia(createPinia())
  const store = useBackupsStore()
  answers = ['old']
  expect(await store.waitForRestart('old', { sleep: () => new Promise(r => setTimeout(r, 5)), timeoutMs: 50 })).toBe(false)
})
