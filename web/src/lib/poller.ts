// Polling that pauses while the browser tab is hidden, so a background tab
// sends no requests, and refreshes at once when the tab is shown again.

export interface VisibilityDocument {
  visibilityState: string
  addEventListener(type: 'visibilitychange', listener: () => void): void
  removeEventListener(type: 'visibilitychange', listener: () => void): void
}

export interface PollerOptions {
  intervalMs: number
  document?: VisibilityDocument
  setInterval?: (fn: () => void, ms: number) => unknown
  clearInterval?: (id: any) => void
}

export interface Poller {
  start(): void
  stop(): void
}

export function createVisibilityPoller(task: () => Promise<unknown> | unknown, opts: PollerOptions): Poller {
  const doc = opts.document ?? (typeof document === 'undefined' ? undefined : document)
  const schedule = opts.setInterval ?? ((fn: () => void, ms: number) => globalThis.setInterval(fn, ms))
  const cancel = opts.clearInterval ?? ((id: any) => globalThis.clearInterval(id))
  let timer: unknown = null
  let running = false
  let inFlight = false

  async function poll() {
    if (inFlight) return
    inFlight = true
    try {
      await task()
    } catch {
      /* the next tick retries */
    } finally {
      inFlight = false
    }
  }

  function resume() {
    if (timer !== null) return
    void poll()
    timer = schedule(() => { void poll() }, opts.intervalMs)
  }

  function pause() {
    if (timer === null) return
    cancel(timer)
    timer = null
  }

  function onVisibility() {
    if (!running) return
    if (!doc || doc.visibilityState === 'visible') resume()
    else pause()
  }

  return {
    start() {
      if (running) return
      running = true
      doc?.addEventListener('visibilitychange', onVisibility)
      onVisibility()
    },
    stop() {
      if (!running) return
      running = false
      doc?.removeEventListener('visibilitychange', onVisibility)
      pause()
    }
  }
}
