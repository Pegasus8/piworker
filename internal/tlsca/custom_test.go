package tlsca

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issuedPair writes a certificate/key pair issued by a fresh local CA (as an
// administrator's own certificate would be) and returns their paths.
func issuedPair(t *testing.T, host string) (string, string) {
	t.Helper()
	f := newFixture(t)
	f.host = host
	f.manager(t)
	return filepath.Join(f.dir, ServerCertFile), filepath.Join(f.dir, ServerKeyFile)
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(to, data, 0o600))
}

func TestFileCertificateServesConfiguredPair(t *testing.T) {
	certFile, keyFile := issuedPair(t, "custom-one")
	fc, err := LoadFileCertificate(certFile, keyFile)
	require.NoError(t, err)

	cfg := fc.TLSConfig()
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	cert, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	assert.Contains(t, cert.Leaf.DNSNames, "custom-one")

	st := fc.Status()
	assert.Empty(t, st.CAFingerprint, "a custom certificate has no local CA")
	assert.Contains(t, st.SANs, "custom-one.local")
	assert.Equal(t, cert.Leaf.NotAfter, st.NotAfter)
	assert.Nil(t, fc.CACertPEM())
}

func TestFileCertificateReloadIsHotAndKeepsOldPairOnError(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	oneCert, oneKey := issuedPair(t, "first-name")
	copyFile(t, oneCert, certFile)
	copyFile(t, oneKey, keyFile)
	fc, err := LoadFileCertificate(certFile, keyFile)
	require.NoError(t, err)

	twoCert, twoKey := issuedPair(t, "second-name")
	copyFile(t, twoCert, certFile)
	copyFile(t, twoKey, keyFile)
	require.NoError(t, fc.Reload())
	cert, err := fc.TLSConfig().GetCertificate(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	assert.Contains(t, cert.Leaf.DNSNames, "second-name")

	require.NoError(t, os.WriteFile(keyFile, []byte("broken"), 0o600))
	require.Error(t, fc.Reload())
	cert, err = fc.TLSConfig().GetCertificate(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	assert.Contains(t, cert.Leaf.DNSNames, "second-name", "a failed reload keeps serving the previous pair")
}

func TestLoadFileCertificateErrors(t *testing.T) {
	certFile, keyFile := issuedPair(t, "pair")
	_, err := LoadFileCertificate(filepath.Join(t.TempDir(), "missing.pem"), keyFile)
	require.Error(t, err)
	_, err = LoadFileCertificate(certFile, certFile)
	require.Error(t, err)
}
