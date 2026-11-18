package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInterfaces is a device whose addresses change during a test.
type fakeInterfaces struct {
	mu  sync.Mutex
	ips []net.IP
}

func (f *fakeInterfaces) set(ips ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ips = nil
	for _, s := range ips {
		f.ips = append(f.ips, net.ParseIP(s))
	}
}

func (f *fakeInterfaces) get() ([]net.IP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ips), nil
}

func newWatchedManager(t *testing.T, ifaces *fakeInterfaces) *tlsca.Manager {
	t.Helper()
	m, err := tlsca.NewManager(tlsca.Options{
		Dir:        t.TempDir(),
		Interfaces: ifaces.get,
		Hostname:   func() (string, error) { return "watchpi", nil },
	})
	require.NoError(t, err)
	return m
}

// startWatch runs watchTLS until the test ends and waits for it to stop.
func startWatch(t *testing.T, src tlsSource, interval time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := watchTLS(ctx, src, interval)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watchTLS did not stop after its context was cancelled")
		}
	})
}

// sendSIGHUP delivers SIGHUP to this process. A channel of the test's own
// stays subscribed while it does, so a regression in watchTLS fails the test
// instead of terminating the test binary.
func sendSIGHUP(t *testing.T) {
	t.Helper()
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGHUP)
	defer signal.Stop(guard)
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGHUP))
	select {
	case <-guard:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP was not delivered")
	}
}

// servedLeaf is the certificate a client connecting now would receive.
func servedLeaf(conf *tls.Config) (*x509.Certificate, error) {
	cert, err := conf.GetCertificate(&tls.ClientHelloInfo{ServerName: "watchpi.local"})
	if err != nil {
		return nil, err
	}
	if cert.Leaf != nil {
		return cert.Leaf, nil
	}
	return x509.ParseCertificate(cert.Certificate[0])
}

func servesIP(conf *tls.Config, ip string) bool {
	leaf, err := servedLeaf(conf)
	return err == nil && slices.ContainsFunc(leaf.IPAddresses, func(a net.IP) bool { return a.Equal(net.ParseIP(ip)) })
}

// writeCustomCertificate writes a self-signed certificate and key, as an
// administrator would for tls.mode = "custom", and returns its serial number.
func writeCustomCertificate(t testing.TB, certFile, keyFile, name string) *big.Int {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return serial
}

func newWatchedFileCertificate(t testing.TB, dir string) (fc *tlsca.FileCertificate, certFile, keyFile string) {
	t.Helper()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeCustomCertificate(t, certFile, keyFile, "first.example")
	fc, err := tlsca.LoadFileCertificate(certFile, keyFile)
	require.NoError(t, err)
	return fc, certFile, keyFile
}

func TestWatchTLSRefreshesTheLocalCertificatePeriodically(t *testing.T) {
	ifaces := &fakeInterfaces{}
	ifaces.set("192.168.1.10")
	m := newWatchedManager(t, ifaces)
	conf := m.TLSConfig()
	require.False(t, servesIP(conf, "192.168.1.77"))

	startWatch(t, m, 20*time.Millisecond)
	ifaces.set("192.168.1.10", "192.168.1.77")

	assert.Eventually(t, func() bool { return servesIP(conf, "192.168.1.77") }, 5*time.Second, 10*time.Millisecond,
		"the served certificate does not cover the new address")
}

func TestWatchTLSRefreshesTheLocalCertificateOnSIGHUP(t *testing.T) {
	ifaces := &fakeInterfaces{}
	ifaces.set("192.168.1.10")
	m := newWatchedManager(t, ifaces)
	conf := m.TLSConfig()

	startWatch(t, m, time.Hour)
	ifaces.set("192.168.1.10", "192.168.1.77")
	sendSIGHUP(t)

	assert.Eventually(t, func() bool { return servesIP(conf, "192.168.1.77") }, 5*time.Second, 10*time.Millisecond,
		"SIGHUP did not refresh the certificate")
}

func TestWatchTLSReloadsTheCustomCertificateOnSIGHUP(t *testing.T) {
	fc, certFile, keyFile := newWatchedFileCertificate(t, t.TempDir())
	conf := fc.TLSConfig()

	startWatch(t, fc, time.Hour)
	serial := writeCustomCertificate(t, certFile, keyFile, "second.example")
	sendSIGHUP(t)

	assert.Eventually(t, func() bool {
		leaf, err := servedLeaf(conf)
		return err == nil && leaf.SerialNumber.Cmp(serial) == 0
	}, 5*time.Second, 10*time.Millisecond, "SIGHUP did not swap the served certificate")
	assert.Equal(t, []string{"second.example"}, fc.Status().SANs)
}

func TestWatchTLSKeepsTheCustomCertificateWhenReloadFails(t *testing.T) {
	fc, certFile, _ := newWatchedFileCertificate(t, t.TempDir())
	conf := fc.TLSConfig()
	before, err := servedLeaf(conf)
	require.NoError(t, err)

	startWatch(t, fc, time.Hour)
	require.NoError(t, os.WriteFile(certFile, []byte("not a certificate"), 0o600))
	sendSIGHUP(t)

	// Nothing observable changes; give the watcher time to act.
	time.Sleep(100 * time.Millisecond)
	after, err := servedLeaf(conf)
	require.NoError(t, err)
	assert.Equal(t, before.SerialNumber, after.SerialNumber)
}

func TestWatchTLSWithoutTLSIsDone(t *testing.T) {
	select {
	case <-watchTLS(context.Background(), nil, time.Hour):
	default:
		t.Fatal("watchTLS without a certificate source must not start a watcher")
	}
}

const watchChildEnv = "PIWORKER_TEST_WATCHTLS_CHILD"

// Exit codes of the child process.
const (
	watchChildSkipped = 3 // SIGHUP was ignored when the child started
	watchChildStuck   = 4 // watchTLS did not stop
	watchChildAlive   = 5 // SIGHUP after cancel was still captured
)

// After its context ends, watchTLS must give SIGHUP back: the default action
// (terminate) applies again. This can only be observed in a separate process.
func TestWatchTLSReleasesSIGHUPWhenCancelled(t *testing.T) {
	if os.Getenv(watchChildEnv) == "1" {
		watchTLSChild(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWatchTLSReleasesSIGHUPWhenCancelled$")
	cmd.Env = append(os.Environ(), watchChildEnv+"=1")
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the child survived SIGHUP: %s", out)
	status := exitErr.Sys().(syscall.WaitStatus)
	if status.Exited() && status.ExitStatus() == watchChildSkipped {
		t.Skip("SIGHUP is ignored by this process (started with nohup?)")
	}
	require.True(t, status.Signaled(), "child exit status %d, want termination by SIGHUP: %s", status.ExitStatus(), out)
	assert.Equal(t, syscall.SIGHUP, status.Signal())
}

func watchTLSChild(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		os.Exit(watchChildSkipped)
	}
	// The process ends by signal, so no test cleanup runs.
	dir, err := os.MkdirTemp("", "piworker-watchtls-")
	require.NoError(t, err)
	fc, _, _ := newWatchedFileCertificate(t, dir)
	require.NoError(t, os.RemoveAll(dir))
	ctx, cancel := context.WithCancel(context.Background())
	done := watchTLS(ctx, fc, time.Hour)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		os.Exit(watchChildStuck)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	time.Sleep(5 * time.Second)
	os.Exit(watchChildAlive)
}
