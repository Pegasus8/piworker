package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const day = 24 * time.Hour

// advanceTo moves the fixture clock to t.
func (f *fixture) advanceTo(t time.Time) { f.clock.Advance(t.Sub(f.clock.Now())) }

// A server certificate issued near the end of the CA's life ends with the CA:
// a chain is only valid while every certificate in it is.
func TestServerCertificateNeverOutlivesTheCA(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	caEnd := m.Status().CANotAfter

	f.advanceTo(caEnd.Add(-100 * day))
	f.net.set("192.168.1.20")
	require.NoError(t, m.Refresh())
	leaf := serve(t, m)
	assert.True(t, hasIP(leaf, "192.168.1.20"))
	assert.True(t, leaf.NotAfter.Equal(caEnd), "capped at the CA's expiry, got %s (CA %s)", leaf.NotAfter, caEnd)
	assert.True(t, m.Status().NotAfter.Equal(caEnd))
	verifyAll(t, m, leaf, f.clock.Now())
}

// Once the leaf is capped at the CA's expiry, a new one cannot last longer:
// handshakes and refreshes must not re-issue it over and over.
func TestCappedCertificateIsNotReissuedRepeatedly(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	caEnd := m.Status().CANotAfter

	f.advanceTo(caEnd.Add(-10 * day))
	f.net.set("192.168.1.20")
	require.NoError(t, m.Refresh())
	capped := serve(t, m)
	require.True(t, capped.NotAfter.Equal(caEnd))

	f.clock.Advance(time.Hour)
	assert.Equal(t, capped.SerialNumber, serve(t, m).SerialNumber, "no renewal on handshake")
	require.NoError(t, m.Refresh())
	assert.Equal(t, capped.SerialNumber, serve(t, m).SerialNumber, "no renewal on refresh")
	again := f.manager(t)
	assert.Equal(t, capped.SerialNumber, serve(t, again).SerialNumber, "kept at the next start")
}

// A stored certificate that outlives its CA (issued before the cap existed)
// is re-issued, so Status reports when the chain really expires.
func TestCertificateOutlivingTheCAIsReissued(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	caEnd := m.Status().CANotAfter
	f.advanceTo(caEnd.Add(-200 * day))

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	sans, err := m.currentSANs()
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "raspberrypi"},
		NotBefore:    f.clock.Now().Add(-day),
		NotAfter:     caEnd.Add(30 * day),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     sans.names,
		IPAddresses:  sans.ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, m.ca, &key.PublicKey, m.caKey)
	require.NoError(t, err)
	keyPEM, err := encodeKey(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, ServerKeyFile), keyPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, ServerCertFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644))

	leaf := serve(t, f.manager(t))
	assert.NotEqual(t, 0, leaf.SerialNumber.Cmp(big.NewInt(1)), "the outliving certificate was replaced")
	assert.True(t, leaf.NotAfter.Equal(caEnd))
}

// Within 90 days of the CA's expiry, Status carries a warning that tells the
// admin to create a new CA; past it, that the CA expired.
func TestStatusWarnsBeforeTheCAExpires(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	assert.Empty(t, m.Status().Warning)
	caEnd := m.Status().CANotAfter

	f.advanceTo(caEnd.Add(-91 * day))
	assert.Empty(t, m.Status().Warning, "not yet within 90 days")

	f.advanceTo(caEnd.Add(-89 * day))
	w := m.Status().Warning
	assert.Contains(t, w, "expires on "+caEnd.Format(time.DateOnly))
	assert.Contains(t, w, "in 89 days")
	assert.Contains(t, w, "-tls-regenerate-ca")

	f.advanceTo(caEnd.Add(time.Hour))
	assert.Contains(t, m.Status().Warning, "expired on "+caEnd.Format(time.DateOnly))
}

// An expired CA cannot sign anything useful: the current certificate stays
// in place and Refresh reports why it could not re-issue.
func TestExpiredCADoesNotIssue(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	before := serve(t, m)
	caEnd := m.Status().CANotAfter

	f.advanceTo(caEnd.Add(day))
	f.net.set("192.168.1.30")
	err := m.Refresh()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
	assert.Contains(t, err.Error(), "-tls-regenerate-ca")
	assert.Equal(t, before.SerialNumber, serve(t, m).SerialNumber)
}

func TestCAExpiryWarningUsesWholeDays(t *testing.T) {
	end := time.Date(2036, 3, 1, 12, 0, 0, 0, time.UTC)
	assert.Contains(t, CAExpiryWarning(end, end.Add(-90*day)), "in 90 days")
	assert.Contains(t, CAExpiryWarning(end, end.Add(-36*time.Hour)), "in 1 day")
	assert.Contains(t, CAExpiryWarning(end, end.Add(-time.Hour)), "in less than a day")
	assert.Empty(t, CAExpiryWarning(end, end.Add(-90*day-time.Second)))
}
