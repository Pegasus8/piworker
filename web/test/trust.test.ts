import { expect, test } from 'bun:test'
import { detectPlatform, fingerprintRows, TRUST_GUIDES, PLATFORMS, caFileName } from '../src/lib/trust'

const UA = {
  mac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15',
  windows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36',
  linux: 'Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36',
  iphone: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
  android: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36',
  firefox: 'Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0',
  firefoxAndroid: 'Mozilla/5.0 (Android 14; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0'
}

test('detects the platform whose trust store applies', () => {
  expect(detectPlatform(UA.mac)).toBe('macos')
  expect(detectPlatform(UA.windows)).toBe('windows')
  expect(detectPlatform(UA.linux)).toBe('linux')
  expect(detectPlatform(UA.iphone)).toBe('ios')
  expect(detectPlatform(UA.android)).toBe('android')
  // Desktop Firefox keeps its own certificate store on every OS.
  expect(detectPlatform(UA.firefox)).toBe('firefox')
  // Firefox for Android uses the Android steps.
  expect(detectPlatform(UA.firefoxAndroid)).toBe('android')
  // iPadOS reports a Mac user agent but has touch support.
  expect(detectPlatform(UA.mac, 5)).toBe('ios')
  expect(detectPlatform('curl/8.0')).toBeNull()
})

test('every platform has install, removal and fingerprint steps', () => {
  expect(PLATFORMS).toEqual(['macos', 'windows', 'linux', 'ios', 'android', 'firefox'])
  for (const id of PLATFORMS) {
    const guide = TRUST_GUIDES[id]
    expect(guide.label.length).toBeGreaterThan(0)
    expect(guide.install.length).toBeGreaterThan(0)
    expect(guide.remove.length).toBeGreaterThan(0)
    expect(guide.verify.length).toBeGreaterThan(0)
  }
})

test('fingerprints are split into readable rows without losing bytes', () => {
  const bytes = Array.from({ length: 32 }, (_, i) => i.toString(16).toUpperCase().padStart(2, '0'))
  const rows = fingerprintRows('SHA256:' + bytes.join(':'))
  expect(rows).toHaveLength(4)
  expect(rows[0]).toBe(bytes.slice(0, 8).join(':'))
  expect(rows.join(':')).toBe(bytes.join(':'))
  expect(fingerprintRows('')).toEqual([])
})

test('the downloaded file keeps the server name', () => {
  expect(caFileName()).toBe('piworker-ca.crt')
})
