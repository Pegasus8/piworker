package tlsca

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a manually advanced clock shared by a Manager under test.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	// Start at the real time so certificates created with time.Now (RegenerateCA)
	// are already valid on the fake clock.
	return &fakeClock{t: time.Now().UTC().Truncate(time.Second)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeNet reports a configurable set of interface addresses.
type fakeNet struct {
	mu  sync.Mutex
	ips []net.IP
	err error
}

func (n *fakeNet) set(ips ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ips = nil
	for _, s := range ips {
		n.ips = append(n.ips, net.ParseIP(s))
	}
}

func (n *fakeNet) Interfaces() ([]net.IP, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]net.IP(nil), n.ips...), n.err
}

type fixture struct {
	dir   string
	clock *fakeClock
	net   *fakeNet
	host  string
	extra []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dir: filepath.Join(t.TempDir(), "tls"), clock: newFakeClock(), net: &fakeNet{}, host: "raspberrypi"}
	f.net.set("127.0.0.1", "192.168.1.10", "fe80::1", "fd00::10")
	return f
}

func (f *fixture) options() Options {
	return Options{
		Dir:        f.dir,
		ExtraHosts: f.extra,
		Now:        f.clock.Now,
		Interfaces: f.net.Interfaces,
		Hostname:   func() (string, error) { return f.host, nil },
	}
}

func (f *fixture) manager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(f.options())
	require.NoError(t, err)
	return m
}

func serve(t *testing.T, m *Manager) *x509.Certificate {
	t.Helper()
	cert, err := m.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: "raspberrypi.local"})
	require.NoError(t, err)
	require.NotNil(t, cert.Leaf)
	return cert.Leaf
}

func caPool(t *testing.T, m *Manager) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(m.CACertPEM()))
	return pool
}

// verifyAll checks that leaf chains to the CA for every SAN it carries.
func verifyAll(t *testing.T, m *Manager, leaf *x509.Certificate, now time.Time) {
	t.Helper()
	pool := caPool(t, m)
	names := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	require.NotEmpty(t, names)
	for _, name := range names {
		_, err := leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
		assert.NoError(t, err, "SAN %s must verify against the local CA", name)
	}
}

func hasIP(leaf *x509.Certificate, s string) bool {
	want := net.ParseIP(s)
	for _, ip := range leaf.IPAddresses {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}

func TestFirstStartCreatesCAAndServerCertificate(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)

	for name, perm := range map[string]os.FileMode{
		CACertFile: 0o644, CAKeyFile: 0o600, ServerCertFile: 0o644, ServerKeyFile: 0o600,
	} {
		info, err := os.Stat(filepath.Join(f.dir, name))
		require.NoError(t, err, name)
		assert.Equal(t, perm, info.Mode().Perm(), name)
	}
	info, err := os.Stat(f.dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "the TLS directory holds private keys")

	block, _ := pem.Decode(m.CACertPEM())
	require.NotNil(t, block)
	require.Equal(t, "CERTIFICATE", block.Type)
	ca, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.True(t, ca.IsCA)
	assert.Equal(t, "PiWorker Local CA raspberrypi", ca.Subject.CommonName)
	assert.Equal(t, x509.ECDSA, ca.PublicKeyAlgorithm)
	assert.Empty(t, ca.PermittedDNSDomains, "name constraints are not used")
	assert.True(t, ca.NotAfter.After(f.clock.Now().AddDate(9, 11, 0)), "the CA lasts ten years")

	leaf := serve(t, m)
	assert.Equal(t, x509.ECDSA, leaf.PublicKeyAlgorithm)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, leaf.ExtKeyUsage)
	assert.False(t, leaf.IsCA)
	assert.WithinDuration(t, f.clock.Now().Add(397*24*time.Hour), leaf.NotAfter, time.Minute)
	assert.LessOrEqual(t, leaf.NotAfter.Sub(leaf.NotBefore), 398*24*time.Hour, "browsers reject server certificates longer than 398 days")

	for _, name := range []string{"localhost", "raspberrypi", "raspberrypi.local"} {
		assert.Contains(t, leaf.DNSNames, name)
	}
	for _, ip := range []string{"127.0.0.1", "::1", "192.168.1.10", "fd00::10"} {
		assert.True(t, hasIP(leaf, ip), "missing IP SAN %s", ip)
	}
	assert.False(t, hasIP(leaf, "fe80::1"), "IPv6 link-local addresses need a zone and are not usable in URLs")
	verifyAll(t, m, leaf, f.clock.Now())

	// No temporary files are left behind by the atomic writes.
	entries, err := os.ReadDir(f.dir)
	require.NoError(t, err)
	assert.Len(t, entries, 4)
}

func TestTLSConfigRequiresTLS12(t *testing.T) {
	m := newFixture(t).manager(t)
	cfg := m.TLSConfig()
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.NotNil(t, cfg.GetCertificate)
	assert.Empty(t, cfg.Certificates, "certificates are served through GetCertificate so they can be re-issued hot")
}

func TestSecondStartLoadsTheSameCAAndCertificate(t *testing.T) {
	f := newFixture(t)
	first := f.manager(t)
	firstLeaf := serve(t, first)

	f.clock.Advance(24 * time.Hour)
	second := f.manager(t)
	assert.Equal(t, first.CAFingerprint(), second.CAFingerprint())
	assert.Equal(t, first.CACertPEM(), second.CACertPEM())
	assert.Equal(t, firstLeaf.SerialNumber, serve(t, second).SerialNumber, "a valid certificate is reused, not re-issued")
}

func TestCAFingerprintFormat(t *testing.T) {
	m := newFixture(t).manager(t)
	fp := m.CAFingerprint()
	require.Regexp(t, regexp.MustCompile(`^SHA256(:[0-9A-F]{2}){32}$`), fp)

	block, _ := pem.Decode(m.CACertPEM())
	sum := sha256.Sum256(block.Bytes)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	assert.Equal(t, "SHA256:"+strings.Join(parts, ":"), fp)
}

func TestIPChangeReissuesWithTheSameCA(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	before := serve(t, m)
	fingerprint := m.CAFingerprint()

	f.net.set("192.168.1.20")
	require.NoError(t, m.Refresh())

	after := serve(t, m)
	assert.NotEqual(t, before.SerialNumber, after.SerialNumber)
	assert.True(t, hasIP(after, "192.168.1.20"))
	assert.Equal(t, fingerprint, m.CAFingerprint(), "the CA is never rotated automatically")
	verifyAll(t, m, after, f.clock.Now())

	// The re-issued certificate is persisted and reused by the next start.
	again := f.manager(t)
	assert.Equal(t, after.SerialNumber, serve(t, again).SerialNumber)
}

func TestRefreshKeepsCertificateWhenSANsAreCovered(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	before := serve(t, m)

	// An address disappearing does not require a new certificate.
	f.net.set()
	f.clock.Advance(10 * time.Minute)
	require.NoError(t, m.Refresh())
	assert.Equal(t, before.SerialNumber, serve(t, m).SerialNumber)
}

func TestHostnameChangeReissues(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	f.host = "kitchen-pi"
	require.NoError(t, m.Refresh())
	leaf := serve(t, m)
	assert.Contains(t, leaf.DNSNames, "kitchen-pi")
	assert.Contains(t, leaf.DNSNames, "kitchen-pi.local")
}

func TestCertificateNearExpiryIsReissuedHot(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	before := serve(t, m)

	f.clock.Advance(before.NotAfter.Sub(f.clock.Now()) - 31*24*time.Hour)
	assert.Equal(t, before.SerialNumber, serve(t, m).SerialNumber, "not renewed while more than 30 days remain")

	f.clock.Advance(2 * 24 * time.Hour)
	after := serve(t, m) // no Refresh call: the handshake itself renews
	assert.NotEqual(t, before.SerialNumber, after.SerialNumber)
	assert.True(t, after.NotAfter.After(before.NotAfter))
	verifyAll(t, m, after, f.clock.Now())
}

func TestExpiringCertificateIsReplacedAtStart(t *testing.T) {
	f := newFixture(t)
	before := serve(t, f.manager(t))
	f.clock.Advance(before.NotAfter.Sub(f.clock.Now()) - 10*24*time.Hour)
	after := serve(t, f.manager(t))
	assert.NotEqual(t, before.SerialNumber, after.SerialNumber)
}

func TestExtraHostsAreIncluded(t *testing.T) {
	f := newFixture(t)
	f.extra = []string{"PiWorker.Example.LAN", "10.0.0.5", " pi.home "}
	leaf := serve(t, f.manager(t))
	assert.Contains(t, leaf.DNSNames, "piworker.example.lan")
	assert.Contains(t, leaf.DNSNames, "pi.home")
	assert.True(t, hasIP(leaf, "10.0.0.5"))
}

func TestInvalidExtraHostIsRejected(t *testing.T) {
	for _, bad := range []string{"https://pi.local", "pi.local:8443", "bad host", "", "-pi"} {
		f := newFixture(t)
		f.extra = []string{bad}
		_, err := NewManager(f.options())
		require.Error(t, err, bad)
	}
}

func TestStatusReportsCertificate(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	leaf := serve(t, m)
	st := m.Status()
	assert.Equal(t, m.CAFingerprint(), st.CAFingerprint)
	assert.Equal(t, leaf.NotAfter, st.NotAfter)
	assert.Contains(t, st.SANs, "raspberrypi.local")
	assert.Contains(t, st.SANs, "192.168.1.10")
	assert.True(t, st.CANotAfter.After(st.NotAfter))
}

func TestMissingCAKeyIsAnErrorNotARotation(t *testing.T) {
	f := newFixture(t)
	f.manager(t)
	original, err := os.ReadFile(filepath.Join(f.dir, CACertFile))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(f.dir, CAKeyFile)))

	_, err = NewManager(f.options())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tls-regenerate-ca")
	now, err := os.ReadFile(filepath.Join(f.dir, CACertFile))
	require.NoError(t, err)
	assert.Equal(t, original, now, "a trusted CA must never be replaced silently")
}

func TestCorruptServerCertificateIsReissued(t *testing.T) {
	f := newFixture(t)
	fingerprint := f.manager(t).CAFingerprint()
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, ServerCertFile), []byte("garbage"), 0o644))
	m := f.manager(t)
	assert.Equal(t, fingerprint, m.CAFingerprint())
	verifyAll(t, m, serve(t, m), f.clock.Now())
}

func TestRegenerateCAReplacesCAAndServerCertificate(t *testing.T) {
	f := newFixture(t)
	old := f.manager(t)
	oldLeaf := serve(t, old)

	require.NoError(t, RegenerateCA(f.dir))
	_, err := os.Stat(filepath.Join(f.dir, ServerCertFile))
	assert.True(t, os.IsNotExist(err), "the old server certificate is removed")

	m := f.manager(t)
	assert.NotEqual(t, old.CAFingerprint(), m.CAFingerprint())
	leaf := serve(t, m)
	assert.NotEqual(t, oldLeaf.SerialNumber, leaf.SerialNumber)
	verifyAll(t, m, leaf, f.clock.Now())
	info, err := os.Stat(filepath.Join(f.dir, CAKeyFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCertificateFromAnotherCAIsReissued(t *testing.T) {
	f := newFixture(t)
	f.manager(t)
	// Simulate a CA replaced by hand (or restored from a backup) while the old
	// server certificate stayed in place.
	other := newFixture(t)
	other.manager(t)
	for _, name := range []string{CACertFile, CAKeyFile} {
		data, err := os.ReadFile(filepath.Join(other.dir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(f.dir, name), data, 0o600))
	}
	m := f.manager(t)
	verifyAll(t, m, serve(t, m), f.clock.Now())
}

func TestConcurrentHandshakesAndRefresh(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if i%2 == 0 {
					f.net.set(fmt.Sprintf("192.168.1.%d", 10+j))
					assert.NoError(t, m.Refresh())
				} else {
					_, err := m.TLSConfig().GetCertificate(&tls.ClientHelloInfo{})
					assert.NoError(t, err)
					_ = m.Status()
				}
			}
		}(i)
	}
	wg.Wait()
}

// A Raspberry Pi has no real-time clock: a CA created before NTP sets the time
// would expire as soon as the clock is corrected and is never rotated.
func TestCANotCreatedWithUnsetClock(t *testing.T) {
	f := newFixture(t)
	f.clock.t = time.Date(1970, 1, 1, 0, 0, 10, 0, time.UTC)
	_, err := NewManager(f.options())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clock")
	_, statErr := os.Stat(filepath.Join(f.dir, CACertFile))
	assert.True(t, os.IsNotExist(statErr))
}

func TestRealHandshakeTrustsOnlyTheLocalCA(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", m.TLSConfig())
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.(*tls.Conn).Handshake()
			c.Close()
		}
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: caPool(t, m), ServerName: "127.0.0.1", Time: f.clock.Now})
	require.NoError(t, err)
	conn.Close()

	_, err = tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "127.0.0.1", Time: f.clock.Now})
	require.Error(t, err, "a client that does not trust the CA must fail")
}
