import { expect, test } from 'bun:test'
import { createVisibilityPoller } from '../src/lib/poller'

class FakeDocument extends EventTarget {
  visibilityState: 'visible' | 'hidden' = 'visible'
  show(state: 'visible' | 'hidden') {
    this.visibilityState = state
    this.dispatchEvent(new Event('visibilitychange'))
  }
}

function fakeTimers() {
  let next = 1
  const timers = new Map<number, () => void>()
  return {
    setInterval: (fn: () => void) => { const id = next++; timers.set(id, fn); return id },
    clearInterval: (id: number) => { timers.delete(id) },
    tick: () => { for (const fn of [...timers.values()]) fn() },
    get active() { return timers.size }
  }
}

const flush = () => new Promise(r => setTimeout(r, 0))

test('polls at once and then on every interval while the tab is visible', async () => {
  const doc = new FakeDocument()
  const timers = fakeTimers()
  let calls = 0
  const poller = createVisibilityPoller(async () => { calls++ }, { intervalMs: 10_000, document: doc, ...timers })
  poller.start()
  await flush()
  expect(calls).toBe(1)
  timers.tick(); await flush()
  timers.tick(); await flush()
  expect(calls).toBe(3)
  poller.stop()
})

test('does not poll while hidden and refreshes as soon as the tab is shown again', async () => {
  const doc = new FakeDocument()
  doc.visibilityState = 'hidden'
  const timers = fakeTimers()
  let calls = 0
  const poller = createVisibilityPoller(async () => { calls++ }, { intervalMs: 10_000, document: doc, ...timers })
  poller.start()
  await flush()
  expect(calls).toBe(0)
  expect(timers.active).toBe(0)
  doc.show('visible'); await flush()
  expect(calls).toBe(1)
  expect(timers.active).toBe(1)
  doc.show('hidden'); await flush()
  expect(timers.active).toBe(0)
  timers.tick(); await flush()
  expect(calls).toBe(1)
  poller.stop()
})

test('stop clears the timer and ignores later visibility changes', async () => {
  const doc = new FakeDocument()
  const timers = fakeTimers()
  let calls = 0
  const poller = createVisibilityPoller(async () => { calls++ }, { intervalMs: 10_000, document: doc, ...timers })
  poller.start()
  await flush()
  poller.stop()
  expect(timers.active).toBe(0)
  doc.show('hidden'); doc.show('visible'); await flush()
  expect(calls).toBe(1)
  expect(timers.active).toBe(0)
})

test('a slow request is never overlapped by the next tick', async () => {
  const doc = new FakeDocument()
  const timers = fakeTimers()
  let calls = 0
  let release!: () => void
  const poller = createVisibilityPoller(() => { calls++; return new Promise<void>(r => { release = r }) }, { intervalMs: 10_000, document: doc, ...timers })
  poller.start()
  timers.tick(); timers.tick(); await flush()
  expect(calls).toBe(1)
  release(); await flush()
  timers.tick(); await flush()
  expect(calls).toBe(2)
  release()
  poller.stop()
})

test('a failing request does not stop polling', async () => {
  const doc = new FakeDocument()
  const timers = fakeTimers()
  let calls = 0
  const poller = createVisibilityPoller(async () => { calls++; throw new Error('offline') }, { intervalMs: 10_000, document: doc, ...timers })
  poller.start(); await flush()
  timers.tick(); await flush()
  expect(calls).toBe(2)
  poller.stop()
})

test('starting twice keeps a single timer', async () => {
  const doc = new FakeDocument()
  const timers = fakeTimers()
  const poller = createVisibilityPoller(async () => {}, { intervalMs: 10_000, document: doc, ...timers })
  poller.start(); poller.start()
  await flush()
  expect(timers.active).toBe(1)
  poller.stop()
})
