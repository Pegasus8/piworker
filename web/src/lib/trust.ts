// Guidance for trusting PiWorker's local certificate authority (CA) on each
// platform. Steps are plain text plus optional commands; the view renders them
// as text, never as HTML.

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
    : `# Save ${CA_FILE} with “Download CA certificate” above, as ~/${CA_FILE}`
  return { httpsBase, caDownload }
}

const opensslVerify: TrustStep = {
  text: 'Compare the SHA-256 fingerprint of the downloaded file with the one printed on the PiWorker device:',
  code: `openssl x509 -in ${CA_FILE} -noout -fingerprint -sha256`
}

export const TRUST_GUIDES: Record<PlatformId, TrustGuide> = {
  macos: {
    label: 'macOS',
    install: [
      { text: `Download the CA certificate and double-click ${CA_FILE}. Keychain Access adds it to your login keychain.` },
      { text: 'In Keychain Access, open “PiWorker Local CA …”, expand Trust and set “When using this certificate” to “Always Trust”. Close the window and confirm with your password.' },
      { text: 'Or, for every user of this Mac, from Terminal:', code: `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/Downloads/${CA_FILE}` },
      { text: 'Restart the browser. Safari, Chrome and Edge use this trust; Firefox has its own (see Firefox).' }
    ],
    verify: [
      { text: 'Keychain Access shows the SHA-256 fingerprint in the certificate details. From Terminal:', code: `openssl x509 -in ~/Downloads/${CA_FILE} -noout -fingerprint -sha256` }
    ],
    remove: [
      { text: 'Open Keychain Access, search for “PiWorker Local CA” in the login and System keychains, and delete it.' }
    ]
  },
  windows: {
    label: 'Windows',
    install: [
      { text: `Download the CA certificate and double-click ${CA_FILE}, then choose Install Certificate… → Current User.` },
      { text: 'Choose “Place all certificates in the following store” → Browse → “Trusted Root Certification Authorities”, then Next → Finish, and confirm the security warning.' },
      { text: 'Or from PowerShell:', code: `certutil -addstore -user Root $env:USERPROFILE\\Downloads\\${CA_FILE}` },
      { text: 'Restart the browser. Edge and Chrome use this trust; Firefox has its own (see Firefox).' }
    ],
    verify: [
      { text: 'From PowerShell, compare “Cert Hash(sha256)” (shown in lower case without colons):', code: `certutil -dump $env:USERPROFILE\\Downloads\\${CA_FILE}` }
    ],
    remove: [
      { text: 'Run certmgr.msc, open Trusted Root Certification Authorities → Certificates, right-click “PiWorker Local CA …” and choose Delete.' }
    ]
  },
  linux: {
    label: 'Linux',
    install: [
      { text: 'Debian, Ubuntu and Raspberry Pi OS (system tools such as curl):', code: `sudo cp ${CA_FILE} /usr/local/share/ca-certificates/${CA_FILE} && sudo update-ca-certificates` },
      { text: 'Fedora and RHEL:', code: `sudo cp ${CA_FILE} /etc/pki/ca-trust/source/anchors/ && sudo update-ca-trust` },
      { text: 'Chrome and Chromium use their own store: Settings → Privacy and security → Security → Manage certificates → import the file as a trusted authority. Or (package libnss3-tools):', code: `certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n "PiWorker Local CA" -i ${CA_FILE}` }
    ],
    verify: [opensslVerify],
    remove: [
      { text: 'Debian, Ubuntu and Raspberry Pi OS:', code: `sudo rm /usr/local/share/ca-certificates/${CA_FILE} && sudo update-ca-certificates --fresh` },
      { text: 'Fedora and RHEL:', code: `sudo rm /etc/pki/ca-trust/source/anchors/${CA_FILE} && sudo update-ca-trust` },
      { text: 'Chrome and Chromium: delete it in Manage certificates, or:', code: 'certutil -d sql:$HOME/.pki/nssdb -D -n "PiWorker Local CA"' }
    ]
  },
  ios: {
    label: 'iPhone / iPad',
    install: [
      { text: 'Open this page in Safari (other browsers cannot install profiles), tap Download CA certificate and allow the download.' },
      { text: 'Open Settings → Profile Downloaded → Install, enter your passcode and confirm.' },
      { text: 'Then Settings → General → About → Certificate Trust Settings, and turn on full trust for “PiWorker Local CA …”.' }
    ],
    verify: [
      { text: 'Settings → General → VPN & Device Management → PiWorker Local CA → More Details shows the SHA-256 fingerprint.' }
    ],
    remove: [
      { text: 'Settings → General → VPN & Device Management → PiWorker Local CA → Remove Profile.' }
    ]
  },
  android: {
    label: 'Android',
    install: [
      { text: `Download the CA certificate (${CA_FILE}).` },
      { text: 'Open Settings → Security & privacy → More security settings → Encryption & credentials → Install a certificate → CA certificate → Install anyway, and choose the file. Menu names vary by manufacturer: search Settings for “CA certificate”.' },
      { text: 'Chrome trusts it after a restart. Many apps ignore certificates installed by the user.' }
    ],
    verify: [
      { text: 'Encryption & credentials → Trusted credentials → User → PiWorker Local CA shows the SHA-256 fingerprint.' }
    ],
    remove: [
      { text: 'Encryption & credentials → Trusted credentials → User → PiWorker Local CA → Remove.' }
    ]
  },
  firefox: {
    label: 'Firefox',
    install: [
      { text: 'Desktop Firefox has its own certificate store. Use “Open in Firefox” above, tick “Trust this CA to identify websites” and confirm.' },
      { text: 'Or Settings → Privacy & Security → Certificates → View Certificates → Authorities → Import.' },
      { text: 'On Windows and macOS you can instead let Firefox use the system trust: open about:config and set security.enterprise_roots.enabled to true.' }
    ],
    verify: [
      { text: 'In the import dialog, choose View: the SHA-256 fingerprint is listed under Fingerprints.' }
    ],
    remove: [
      { text: 'Settings → Privacy & Security → View Certificates → Authorities, select the PiWorker entry and choose Delete or Distrust.' }
    ]
  }
}
