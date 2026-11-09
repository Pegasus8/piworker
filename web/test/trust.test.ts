import { expect, test } from 'bun:test'
import { detectPlatform, fingerprintRows, TRUST_GUIDES, PLATFORMS, caFileName, trustUrls } from '../src/lib/trust'

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

const httpPage = { protocol: 'http:', hostname: 'raspberrypi.local', host: 'raspberrypi.local:8080', origin: 'http://raspberrypi.local:8080' }
const httpsPage = { protocol: 'https:', hostname: '192.168.1.10', host: '192.168.1.10:9443', origin: 'https://192.168.1.10:9443' }
// IPv6 pages as browsers report them: hostname keeps its brackets and host
// leaves out a default port.
const v6HttpPage = { protocol: 'http:', hostname: '[fd00::10]', host: '[fd00::10]', origin: 'http://[fd00::10]' }
const v6HttpsPage = { protocol: 'https:', hostname: '[fd00::10]', host: '[fd00::10]:8443', origin: 'https://[fd00::10]:8443' }

test('command-line links use the ports PiWorker reports', () => {
  const urls = trustUrls(httpPage, { httpsPort: 9443, httpPort: 9080 })
  expect(urls.httpsBase).toBe('https://raspberrypi.local:9443')
  expect(urls.caDownload).toBe('curl -o ~/piworker-ca.crt http://raspberrypi.local:9080/piworker-ca.crt')

  // An HTTPS page keeps the address the browser used.
  expect(trustUrls(httpsPage, { httpsPort: 8443, httpPort: 8080 }).httpsBase).toBe('https://192.168.1.10:9443')
  // Default ports are left out.
  const def = trustUrls(v6HttpPage, { httpsPort: 443, httpPort: 80 })
  expect(def.httpsBase).toBe('https://[fd00::10]')
  expect(def.caDownload).toBe('curl -o ~/piworker-ca.crt http://[fd00::10]/piworker-ca.crt')
})

test('IPv6 hosts are bracketed exactly once', () => {
  expect(new URL('http://[fd00::10]:8080/').hostname).toBe('[fd00::10]')
  const urls = trustUrls(v6HttpsPage, { httpsPort: 8443, httpPort: 8080 })
  expect(urls.caDownload).toBe('curl -o ~/piworker-ca.crt http://[fd00::10]:8080/piworker-ca.crt')
  expect(trustUrls(v6HttpPage, { httpsPort: 8443, httpPort: 80 }).httpsBase).toBe('https://[fd00::10]:8443')
  // A bare IPv6 hostname (not what browsers report) is bracketed too.
  expect(trustUrls({ ...v6HttpPage, hostname: 'fd00::10' }, { httpsPort: 8443, httpPort: 80 }).httpsBase).toBe('https://[fd00::10]:8443')
  expect(trustUrls(v6HttpsPage, null).caDownload).toBe('curl -o ~/piworker-ca.crt http://[fd00::10]:8080/piworker-ca.crt')
})

test('without known ports the links use the page address or the default ports', () => {
  for (const ports of [null, { httpsPort: null, httpPort: null }]) {
    const urls = trustUrls(httpsPage, ports)
    expect(urls.httpsBase).toBe('https://192.168.1.10:9443')
    // The default plain listener serves the CA without TLS; the fingerprint
    // check that follows is what makes the download trustworthy.
    expect(urls.caDownload).toBe('curl -o ~/piworker-ca.crt http://192.168.1.10:8080/piworker-ca.crt')

    const plain = trustUrls(httpPage, ports)
    // Never the plain HTTP port with the https scheme: the default HTTPS port.
    expect(plain.httpsBase).toBe('https://raspberrypi.local:8443')
    expect(plain.caDownload).toBe('curl -o ~/piworker-ca.crt http://raspberrypi.local:8080/piworker-ca.crt')
  }
  // HTTPS disabled on the plain listener: no HTTPS port, CA over the reported HTTP port.
  expect(trustUrls(httpPage, { httpsPort: null, httpPort: 8080 }).caDownload).toBe('curl -o ~/piworker-ca.crt http://raspberrypi.local:8080/piworker-ca.crt')
})

test('without a plain listener the CA is saved from the browser, never with --insecure', () => {
  const urls = trustUrls(httpsPage, { httpsPort: 9443, httpPort: null })
  expect(urls.httpsBase).toBe('https://192.168.1.10:9443')
  expect(urls.caDownload).toBe('# Save piworker-ca.crt with “Download CA certificate” above, as ~/piworker-ca.crt')
})

test('the CA download never disables TLS validation', () => {
  const pages = [httpPage, httpsPage, v6HttpPage, v6HttpsPage]
  const portSets = [null, { httpsPort: null, httpPort: null }, { httpsPort: 8443, httpPort: 8080 }, { httpsPort: 8443, httpPort: null }, { httpsPort: null, httpPort: 8080 }]
  for (const page of pages) {
    for (const ports of portSets) {
      const { caDownload, httpsBase } = trustUrls(page, ports)
      expect(caDownload).not.toMatch(/--insecure|(^|\s)-k(\s|$)/)
      expect(caDownload).not.toContain('[[')
      expect(httpsBase).not.toContain('[[')
      expect(httpsBase.startsWith('https://')).toBe(true)
    }
  }
})
