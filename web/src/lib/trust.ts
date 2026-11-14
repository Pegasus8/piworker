// Guidance for trusting PiWorker's local certificate authority (CA) on each
// platform. Steps are plain text plus optional commands; the view renders them
// as text, never as HTML.
import { msg } from '@/i18n'

export type PlatformId = 'macos' | 'windows' | 'linux' | 'ios' | 'android' | 'firefox'

export const PLATFORMS: PlatformId[] = ['macos', 'windows', 'linux', 'ios', 'android', 'firefox']

export interface TrustStep {
  text: string
  code?: string
}

export interface TrustGuide {
  label: string
  install: TrustStep[]
  verify: TrustStep[]
  remove: TrustStep[]
}

const CA_FILE = 'piworker-ca.crt'

/** The file name the CA certificate is saved as. */
export function caFileName(): string {
  return CA_FILE
}

/**
 * Picks the guide that applies to a browser. Desktop Firefox keeps its own
 * certificate store, so it gets its own guide; Firefox on phones uses the
 * system store. iPadOS reports a Mac user agent but has a touch screen.
 */
export function detectPlatform(userAgent: string, maxTouchPoints = 0): PlatformId | null {
  if (/Android/i.test(userAgent)) return 'android'
  if (/iPhone|iPad|iPod/i.test(userAgent)) return 'ios'
  if (/Macintosh/i.test(userAgent) && maxTouchPoints > 1) return 'ios'
  if (/Firefox\//i.test(userAgent)) return 'firefox'
  if (/Windows/i.test(userAgent)) return 'windows'
  if (/Macintosh|Mac OS X/i.test(userAgent)) return 'macos'
  if (/Linux|X11|CrOS/i.test(userAgent)) return 'linux'
  return null
}

/** Splits "SHA256:AA:BB:…" into rows of eight bytes for reading aloud or comparing. */
export function fingerprintRows(fingerprint: string): string[] {
  const hex = fingerprint.replace(/^SHA256:/i, '')
  if (!hex) return []
  const bytes = hex.split(':')
  const rows: string[] = []
  for (let i = 0; i < bytes.length; i += 8) rows.push(bytes.slice(i, i + 8).join(':'))
  return rows
}

/** The parts of window.location the trust page builds links from. */
export interface PageLocation {
  protocol: string
  hostname: string
  host: string
  origin: string
}

/** Listener ports reported by GET /api/tls (null or missing when unknown). */
export interface ListenerPorts {
  httpsPort?: number | null
  httpPort?: number | null
}

// ADR-0011 default listeners, used while PiWorker has not reported its ports
// (GET /api/tls needs a session, so always before signing in).
const DEFAULT_HTTPS_PORT = 8443
const DEFAULT_HTTP_PORT = 8080

/**
 * Builds the HTTPS address and the CA download command shown for
 * command-line tools. Ports come from the server; without them an HTTPS page
 * keeps its own address, an HTTP page serves the CA itself, and the other
 * listener is assumed on its default port. The CA is downloaded over plain
 * HTTP (the fingerprint check that follows makes it trustworthy), never by
 * disabling TLS validation: without a plain listener the browser saves it.
 */
export function trustUrls(page: PageLocation, ports: ListenerPorts | null): { httpsBase: string; caDownload: string } {
  // Browsers keep the brackets of an IPv6 hostname; add them only if missing.
  const host = page.hostname.startsWith('[') || !page.hostname.includes(':') ? page.hostname : `[${page.hostname}]`
  const withPort = (scheme: string, port: number, defaultPort: number) =>
    `${scheme}://${host}${port === defaultPort ? '' : ':' + port}`
  // GET /api/tls reports both ports once the listeners are up: until then
  // (or signed out) neither is known, and a missing httpPort means nothing.
  const known = Boolean(ports?.httpsPort || ports?.httpPort)

  const httpsBase = page.protocol === 'https:'
    ? page.origin
    : withPort('https', ports?.httpsPort || DEFAULT_HTTPS_PORT, 443)

  let base: string | null
  if (ports?.httpPort) base = withPort('http', ports.httpPort, 80)
  else if (page.protocol === 'http:') base = page.origin
  else if (!known) base = withPort('http', DEFAULT_HTTP_PORT, 80)
  else base = null // PiWorker reported no plain listener
  const caDownload = base
    ? `curl -o ~/${CA_FILE} ${base}/${CA_FILE}`
    : msg().trust.cliSaveComment(CA_FILE)
  return { httpsBase, caDownload }
}

function guides(): Record<PlatformId, TrustGuide> {
  const t = msg().trust
  const g = t.guides
  const opensslVerify: TrustStep = {
    text: t.opensslVerify,
    code: `openssl x509 -in ${CA_FILE} -noout -fingerprint -sha256`
  }
  return {
    macos: {
      label: 'macOS',
      install: [
        { text: g.macos.download(CA_FILE) },
        { text: g.macos.alwaysTrust },
        { text: g.macos.terminal, code: `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/Downloads/${CA_FILE}` },
        { text: g.macos.restart }
      ],
      verify: [
        { text: g.macos.verify, code: `openssl x509 -in ~/Downloads/${CA_FILE} -noout -fingerprint -sha256` }
      ],
      remove: [
        { text: g.macos.remove }
      ]
    },
    windows: {
      label: 'Windows',
      install: [
        { text: g.windows.download(CA_FILE) },
        { text: g.windows.store },
        { text: g.windows.powershell, code: `certutil -addstore -user Root $env:USERPROFILE\\Downloads\\${CA_FILE}` },
        { text: g.windows.restart }
      ],
      verify: [
        { text: g.windows.verify, code: `certutil -dump $env:USERPROFILE\\Downloads\\${CA_FILE}` }
      ],
      remove: [
        { text: g.windows.remove }
      ]
    },
    linux: {
      label: 'Linux',
      install: [
        { text: g.linux.debian, code: `sudo cp ${CA_FILE} /usr/local/share/ca-certificates/${CA_FILE} && sudo update-ca-certificates` },
        { text: g.linux.fedora, code: `sudo cp ${CA_FILE} /etc/pki/ca-trust/source/anchors/ && sudo update-ca-trust` },
        { text: g.linux.chrome, code: `certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n "PiWorker Local CA" -i ${CA_FILE}` }
      ],
      verify: [opensslVerify],
      remove: [
        { text: g.linux.removeDebian, code: `sudo rm /usr/local/share/ca-certificates/${CA_FILE} && sudo update-ca-certificates --fresh` },
        { text: g.linux.fedora, code: `sudo rm /etc/pki/ca-trust/source/anchors/${CA_FILE} && sudo update-ca-trust` },
        { text: g.linux.removeChrome, code: 'certutil -d sql:$HOME/.pki/nssdb -D -n "PiWorker Local CA"' }
      ]
    },
    ios: {
      label: g.ios.label,
      install: [
        { text: g.ios.safari },
        { text: g.ios.profile },
        { text: g.ios.fullTrust }
      ],
      verify: [
        { text: g.ios.verify }
      ],
      remove: [
        { text: g.ios.remove }
      ]
    },
    android: {
      label: 'Android',
      install: [
        { text: g.android.download(CA_FILE) },
        { text: g.android.install },
        { text: g.android.chrome }
      ],
      verify: [
        { text: g.android.verify }
      ],
      remove: [
        { text: g.android.remove }
      ]
    },
    firefox: {
      label: 'Firefox',
      install: [
        { text: g.firefox.own },
        { text: g.firefox.import },
        { text: g.firefox.enterprise }
      ],
      verify: [
        { text: g.firefox.verify }
      ],
      remove: [
        { text: g.firefox.remove }
      ]
    }
  }
}

/** The guide of each platform, in the interface language at the time it is read. */
export const TRUST_GUIDES = Object.defineProperties({}, Object.fromEntries(
  PLATFORMS.map(id => [id, { enumerable: true, get: () => guides()[id] }])
)) as Record<PlatformId, TrustGuide>
