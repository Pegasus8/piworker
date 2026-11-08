// Package tlsca provides PiWorker's native HTTPS certificates: a local
// certificate authority generated on first start and a server certificate
// signed by it. The server certificate is re-issued automatically (and served
// hot through tls.Config.GetCertificate) when it nears expiry or when the
// device's names or addresses change, so clients that trust the CA once never
// notice. The CA itself is only replaced explicitly, with RegenerateCA.
package tlsca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Files stored in Options.Dir.
const (
	CACertFile     = "ca.crt"
	CAKeyFile      = "ca.key"
	ServerCertFile = "server.crt"
	ServerKeyFile  = "server.key"
)

const (
	// RefreshInterval is how often the caller should run Refresh to pick up
	// address and hostname changes.
	RefreshInterval = 10 * time.Minute
	// RenewBefore is how long before expiry the server certificate is replaced.
	RenewBefore = 30 * 24 * time.Hour
	// ServerValidity keeps server certificates under the 398-day limit that
	// browsers enforce.
	ServerValidity = 397 * 24 * time.Hour
	// CAWarnBefore is how long before the local CA expires Status warns.
	CAWarnBefore = 90 * 24 * time.Hour

	caValidityYears = 10
	// backdate tolerates small clock differences between server and clients.
	backdate = time.Hour
)

// minCATime guards against creating a CA with a clock that has obviously not
// been set yet (a Raspberry Pi has no real-time clock): such a CA would be
// expired as soon as the clock is corrected, and it is never rotated
// automatically.
var minCATime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// Options configures a Manager. Now, Interfaces and Hostname default to the
// system clock, the machine's interface addresses and os.Hostname; tests
// replace them.
type Options struct {
	Dir        string
	ExtraHosts []string
	Now        func() time.Time
	Interfaces func() ([]net.IP, error)
	Hostname   func() (string, error)
}

// Status describes the certificate currently served.
type Status struct {
	CAFingerprint string    `json:"caFingerprint"`
	SANs          []string  `json:"sans"`
	NotAfter      time.Time `json:"notAfter"`
	CANotAfter    time.Time `json:"caNotAfter"`
	// Warning is set when the local CA is within CAWarnBefore of its expiry
	// (or expired): see CAExpiryWarning.
	Warning string `json:"warning,omitempty"`
}

// CAExpiryWarning tells the administrator to replace a CA that expires at
// notAfter, when now is within CAWarnBefore of it or past it; "" otherwise.
// The CA is never rotated automatically (every device has to trust the new
// one), so this is the only notice before the server stops being trusted.
func CAExpiryWarning(notAfter, now time.Time) string {
	left := notAfter.Sub(now)
	date := notAfter.UTC().Format(time.DateOnly)
	switch {
	case left <= 0:
		return fmt.Sprintf("The local CA expired on %s: browsers and agents no longer trust this server. Run piworker -tls-regenerate-ca and trust the new CA on every device.", date)
	case left > CAWarnBefore:
		return ""
	}
	in := "in less than a day"
	if days := int(left / (24 * time.Hour)); days == 1 {
		in = "in 1 day"
	} else if days > 1 {
		in = fmt.Sprintf("in %d days", days)
	}
	return fmt.Sprintf("The local CA expires on %s (%s). Run piworker -tls-regenerate-ca and trust the new CA on every device before then.", date, in)
}

// Manager owns the local CA and the server certificate it signs.
type Manager struct {
	opts       Options
	extraNames []string
	extraIPs   []net.IP

	ca          *x509.Certificate
	caKey       *ecdsa.PrivateKey
	caPEM       []byte
	fingerprint string

	// issueMu serializes re-issuing and writing the server certificate.
	issueMu sync.Mutex
	mu      sync.RWMutex
	cert    *tls.Certificate
}

// sanSet is a normalized (sorted, de-duplicated) set of names and addresses.
type sanSet struct {
	names []string
	ips   []net.IP
}

// NewManager loads the CA and server certificate from opts.Dir, creating the
// CA on first start and re-issuing the server certificate when it is missing,
// invalid, close to expiry or does not cover the current names and addresses.
func NewManager(opts Options) (*Manager, error) {
	if opts.Dir == "" {
		return nil, errors.New("tlsca: a directory is required")
	}
	opts = withDefaults(opts)
	names, ips, err := parseHosts(opts.ExtraHosts)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("tlsca: create %s: %w", opts.Dir, err)
	}
	m := &Manager{opts: opts, extraNames: names, extraIPs: ips}
	if err := m.loadOrCreateCA(); err != nil {
		return nil, err
	}
	sans, err := m.currentSANs()
	if err != nil {
		return nil, err
	}
	if cert := m.loadServer(); cert != nil && m.usable(cert.Leaf, sans) {
		m.cert = cert
		return m, nil
	}
	if err := m.replace(sans); err != nil {
		return nil, err
	}
	return m, nil
}

func withDefaults(opts Options) Options {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Interfaces == nil {
		opts.Interfaces = systemInterfaceIPs
	}
	if opts.Hostname == nil {
		opts.Hostname = os.Hostname
	}
	return opts
}

// systemInterfaceIPs returns every address assigned to a local interface.
func systemInterfaceIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			ips = append(ips, n.IP)
		}
	}
	return ips, nil
}

// TLSConfig returns a server configuration that serves the current certificate
// and renews it during the handshake once it is within RenewBefore of expiry.
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: m.getCertificate}
}

func (m *Manager) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := m.current()
	if m.expiring(cert.Leaf) {
		// A failure keeps the current (still valid) certificate; the periodic
		// Refresh reports the error.
		_ = m.Refresh()
		cert = m.current()
	}
	return cert, nil
}

// CACertPEM returns the CA certificate in PEM form, for clients to trust.
func (m *Manager) CACertPEM() []byte { return slices.Clone(m.caPEM) }

// CAFingerprint returns the SHA-256 fingerprint of the CA certificate as
// "SHA256:AB:CD:…", the form browsers and openssl display.
func (m *Manager) CAFingerprint() string { return m.fingerprint }

// Status describes the certificate being served.
func (m *Manager) Status() Status {
	leaf := m.current().Leaf
	return Status{
		CAFingerprint: m.fingerprint,
		SANs:          leafSANs(leaf),
		NotAfter:      leaf.NotAfter,
		CANotAfter:    m.ca.NotAfter,
		Warning:       CAExpiryWarning(m.ca.NotAfter, m.opts.Now()),
	}
}

// Refresh re-computes the names and addresses of this device and re-issues the
// server certificate when they are not all covered or when it is close to
// expiry. The new certificate is served even if writing it to disk fails; that
// error is still returned.
func (m *Manager) Refresh() error {
	sans, err := m.currentSANs()
	if err != nil {
		return err
	}
	m.issueMu.Lock()
	defer m.issueMu.Unlock()
	if m.usable(m.current().Leaf, sans) {
		return nil
	}
	return m.replace(sans)
}

// RegenerateCA replaces the CA in dir with a new one and removes the server
// certificate, so the next start issues one signed by the new CA. Every device
// that trusted the old CA must trust the new one.
func RegenerateCA(dir string) error {
	if dir == "" {
		return errors.New("tlsca: a directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tlsca: create %s: %w", dir, err)
	}
	m := &Manager{opts: withDefaults(Options{Dir: dir})}
	if err := m.createCA(); err != nil {
		return err
	}
	for _, name := range []string{ServerCertFile, ServerKeyFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tlsca: remove old %s: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) current() *tls.Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert
}

func (m *Manager) path(name string) string { return filepath.Join(m.opts.Dir, name) }

// expiring reports whether leaf must be replaced because of its validity
// period. A chain is only valid while its CA is, so a leaf that outlives the
// CA (issued before certificates were capped) is replaced by a capped one,
// and a leaf already ending with the CA is kept: a new one could not last
// longer, so renewing it would only repeat on every handshake (Status warns
// about the CA instead).
func (m *Manager) expiring(leaf *x509.Certificate) bool {
	caEnd := m.ca.NotAfter
	if leaf.NotAfter.After(caEnd) {
		return true
	}
	if !leaf.NotAfter.Before(caEnd) {
		return false
	}
	return !m.opts.Now().Before(leaf.NotAfter.Add(-RenewBefore))
}

// usable reports whether leaf can keep being served for sans.
func (m *Manager) usable(leaf *x509.Certificate, sans sanSet) bool {
	return leaf != nil && leaf.CheckSignatureFrom(m.ca) == nil && !m.expiring(leaf) && covers(leaf, sans)
}

func covers(leaf *x509.Certificate, sans sanSet) bool {
	for _, name := range sans.names {
		if !slices.Contains(leaf.DNSNames, name) {
			return false
		}
	}
	for _, ip := range sans.ips {
		if !slices.ContainsFunc(leaf.IPAddresses, ip.Equal) {
			return false
		}
	}
	return true
}

func leafSANs(leaf *x509.Certificate) []string {
	out := slices.Clone(leaf.DNSNames)
	for _, ip := range leaf.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// loadOrCreateCA loads the CA, creating it only when neither file exists: a
// CA that devices may already trust is never replaced implicitly.
func (m *Manager) loadOrCreateCA() error {
	certPEM, certErr := os.ReadFile(m.path(CACertFile))
	keyPEM, keyErr := os.ReadFile(m.path(CAKeyFile))
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		return m.createCA()
	}
	const hint = "restore it from a backup, or run piworker -tls-regenerate-ca to create a new CA (every device must trust it again)"
	if certErr != nil {
		return fmt.Errorf("tlsca: read %s: %w; %s", m.path(CACertFile), certErr, hint)
	}
	if keyErr != nil {
		return fmt.Errorf("tlsca: read %s: %w; %s", m.path(CAKeyFile), keyErr, hint)
	}
	ca, err := parseCertPEM(certPEM)
	if err != nil || !ca.IsCA {
		return fmt.Errorf("tlsca: %s is not a valid CA certificate; %s", m.path(CACertFile), hint)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return fmt.Errorf("tlsca: %s: %w; %s", m.path(CAKeyFile), err, hint)
	}
	if pub, ok := ca.PublicKey.(*ecdsa.PublicKey); !ok || !pub.Equal(&key.PublicKey) {
		return fmt.Errorf("tlsca: %s does not match %s; %s", m.path(CAKeyFile), m.path(CACertFile), hint)
	}
	if !m.opts.Now().Before(ca.NotAfter) {
		return fmt.Errorf("tlsca: the local CA expired on %s; run piworker -tls-regenerate-ca (every device must trust the new CA)", ca.NotAfter.Format(time.DateOnly))
	}
	m.setCA(ca, key, certPEM)
	return nil
}

func (m *Manager) createCA() error {
	now := m.opts.Now()
	if now.Before(minCATime) {
		return fmt.Errorf("tlsca: the system clock (%s) looks unset; set the time (for example with NTP) before creating the local CA", now.Format(time.RFC3339))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("tlsca: generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return fmt.Errorf("tlsca: %w", err)
	}
	cn := "PiWorker Local CA"
	if host := m.hostname(); host != "" {
		cn += " " + host
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		// The random unit keeps CAs of identically named devices distinct.
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"PiWorker"}, OrganizationalUnit: []string{hex.EncodeToString(id)}},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("tlsca: create CA certificate: %w", err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("tlsca: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// The key goes first: a crash in between leaves a mismatch that is
	// reported at start instead of a certificate without its key.
	if err := writeFileAtomic(m.path(CAKeyFile), keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(m.path(CACertFile), certPEM, 0o644); err != nil {
		return err
	}
	m.setCA(ca, key, certPEM)
	return nil
}

func (m *Manager) setCA(ca *x509.Certificate, key *ecdsa.PrivateKey, certPEM []byte) {
	m.ca, m.caKey, m.caPEM = ca, key, certPEM
	m.fingerprint = Fingerprint(ca.Raw)
}

// Fingerprint formats the SHA-256 digest of a DER certificate as
// "SHA256:AB:CD:…".
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return "SHA256:" + strings.Join(parts, ":")
}

// loadServer returns the stored server certificate, or nil when it is missing
// or unreadable (it is then simply re-issued).
func (m *Manager) loadServer() *tls.Certificate {
	certPEM, err := os.ReadFile(m.path(ServerCertFile))
	if err != nil {
		return nil
	}
	keyPEM, err := os.ReadFile(m.path(ServerKeyFile))
	if err != nil {
		return nil
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || cert.Leaf == nil {
		return nil
	}
	return &cert
}

// replace issues a certificate for sans, serves it and persists it.
func (m *Manager) replace(sans sanSet) error {
	cert, keyPEM, err := m.issue(sans)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cert = cert
	m.mu.Unlock()
	if err := writeFileAtomic(m.path(ServerKeyFile), keyPEM, 0o600); err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	return writeFileAtomic(m.path(ServerCertFile), certPEM, 0o644)
}

func (m *Manager) issue(sans sanSet) (*tls.Certificate, []byte, error) {
	now := m.opts.Now()
	if !now.Before(m.ca.NotAfter) {
		return nil, nil, fmt.Errorf("tlsca: the local CA expired on %s, so it cannot issue a server certificate; run piworker -tls-regenerate-ca (every device must trust the new CA)", m.ca.NotAfter.UTC().Format(time.DateOnly))
	}
	// The certificate never outlives the CA that signs it.
	notAfter := now.Add(ServerValidity)
	if notAfter.After(m.ca.NotAfter) {
		notAfter = m.ca.NotAfter
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("tlsca: generate server key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	cn := "localhost"
	if host := m.hostname(); host != "" {
		cn = host
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"PiWorker"}},
		NotBefore:             now.Add(-backdate),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              sans.names,
		IPAddresses:           sans.ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, m.ca, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("tlsca: create server certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("tlsca: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, keyPEM, nil
}

// hostname returns the normalized host name, or "" when it is unavailable or
// not a usable DNS name.
func (m *Manager) hostname() string {
	host, err := m.opts.Hostname()
	if err != nil {
		return ""
	}
	host = normalizeName(host)
	if !validName(host) {
		return ""
	}
	return host
}

// currentSANs computes the names and addresses this device answers to:
// localhost, loopback, the host name and its mDNS (.local) name, every usable
// interface address and the configured extra hosts.
func (m *Manager) currentSANs() (sanSet, error) {
	names := []string{"localhost"}
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if host := m.hostname(); host != "" {
		short, _, _ := strings.Cut(host, ".")
		names = append(names, host, short+".local")
	}
	addrs, err := m.opts.Interfaces()
	if err != nil {
		return sanSet{}, fmt.Errorf("tlsca: list interface addresses: %w", err)
	}
	for _, ip := range addrs {
		if usableIP(ip) {
			ips = append(ips, ip)
		}
	}
	names = append(names, m.extraNames...)
	ips = append(ips, m.extraIPs...)
	return normalizeSANs(names, ips), nil
}

// usableIP excludes addresses a browser cannot reach this device at. IPv6
// link-local addresses need a zone, which URLs and certificates cannot carry.
func usableIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return ip.To4() != nil || !ip.IsLinkLocalUnicast()
}

func normalizeSANs(names []string, ips []net.IP) sanSet {
	slices.Sort(names)
	names = slices.Compact(names)
	norm := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		norm = append(norm, ip)
	}
	slices.SortFunc(norm, func(a, b net.IP) int { return bytes.Compare(a.To16(), b.To16()) })
	norm = slices.CompactFunc(norm, net.IP.Equal)
	return sanSet{names: names, ips: norm}
}

var labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

func validName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if !labelRE.MatchString(label) {
			return false
		}
	}
	return true
}

// parseHosts splits configured extra hosts into DNS names and IP addresses.
func parseHosts(hosts []string) ([]string, []net.IP, error) {
	var names []string
	var ips []net.IP
	for _, h := range hosts {
		s := normalizeName(h)
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
			continue
		}
		if !validName(s) {
			return nil, nil, fmt.Errorf("tlsca: invalid TLS host %q: use a DNS name or an IP address, without scheme or port", h)
		}
		names = append(names, s)
	}
	return names, ips, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("tlsca: serial number: %w", err)
	}
	return serial.Add(serial, big.NewInt(1)), nil
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("tlsca: encode key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func parseCertPEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("no PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if ec, ok := key.(*ecdsa.PrivateKey); ok {
			return ec, nil
		}
		return nil, errors.New("the CA key must be an ECDSA key")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// writeFileAtomic replaces path with data so readers never see a partial file.
// The temporary file is created private and only then given perm.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("tlsca: write %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			err = fmt.Errorf("tlsca: write %s: %w", path, err)
		}
	}()
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
