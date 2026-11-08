package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Pegasus8/piworker/internal/tlsca"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := parseEndpoint(raw)
	require.NoError(t, err)
	return u
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// get performs an authenticated request the way the relay does, through the
// connector's client.
func get(ctx context.Context, c *connector, rawURL string) (*http.Response, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return resp, err
}

func TestFingerprintPinWithCAInChainNeedsNoRequest(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, ca.serverCert(t, true, "127.0.0.1"), nil, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	resp, err := get(testCtx(t), c, srv.URL+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, []string{"POST /mcp"}, rec.paths(), "the CA came with the chain: no download")
}

func TestFingerprintMismatchWithCAInChainSendsNothing(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA real")
	impostor := newTestCA(t, "PiWorker Local CA impostor")
	srv, rec := newTLSServer(t, impostor.serverCert(t, true, "127.0.0.1"), impostor, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "does not match --ca-fingerprint")
	assert.Contains(t, msg, ca.fingerprint())
	assert.Contains(t, msg, impostor.fingerprint(), "the CA actually presented is shown")
	assert.Empty(t, rec.requests(), "the server never saw a request")
}

func TestFingerprintPinWithLeafOnlyDownloadsTheCAWithoutCredentials(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, ca.serverCert(t, false, "127.0.0.1"), ca, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: strings.ToLower(ca.fingerprint())})
	require.NoError(t, err)
	ctx := testCtx(t)
	for range 2 {
		resp, err := get(ctx, c, srv.URL+"/mcp")
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	}
	seen := rec.requests()
	require.Len(t, seen, 3, "one CA download, then the two requests")
	assert.Equal(t, "GET /piworker-ca.crt", seen[0].Method+" "+seen[0].Path)
	assert.Empty(t, seen[0].Authorization, "the CA download carries no token")
	assert.Equal(t, "Bearer "+testToken, seen[1].Authorization)
}

func TestFingerprintMismatchWithLeafOnlySendsNoToken(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA real")
	impostor := newTestCA(t, "PiWorker Local CA impostor")
	srv, rec := newTLSServer(t, impostor.serverCert(t, false, "127.0.0.1"), impostor, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match --ca-fingerprint")
	assert.Contains(t, err.Error(), impostor.fingerprint())
	assert.Equal(t, []string{"GET /piworker-ca.crt"}, rec.paths(), "only the public CA download, never /mcp")
	for _, r := range rec.requests() {
		assert.Empty(t, r.Authorization)
	}
}

func TestFingerprintPinWithoutDownloadableCA(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, ca.serverCert(t, false, "127.0.0.1"), nil, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/piworker-ca.crt")
	assert.Contains(t, err.Error(), "--ca-file")
	assert.Equal(t, []string{"GET /piworker-ca.crt"}, rec.paths())
}

func TestFingerprintPinRejectsAWrongServerName(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	// The certificate is valid for piworker.test only; the URL uses 127.0.0.1.
	srv, rec := newTLSServer(t, ca.serverCert(t, true, "piworker.test"), ca, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.Error(t, err)
	var hostErr x509.HostnameError
	assert.True(t, errors.As(err, &hostErr), "got %v", err)
	assert.Empty(t, rec.requests(), "the handshake failed before any request")
}

func TestFingerprintPinsTheCANotJustAnyCertificate(t *testing.T) {
	// The pinned CA is presented in the chain but did not sign the leaf: the
	// chain must still verify against it.
	ca := newTestCA(t, "PiWorker Local CA real")
	other := newTestCA(t, "Other CA")
	cert := other.serverCert(t, false, "127.0.0.1")
	cert.Certificate = append(cert.Certificate, ca.cert.Raw)
	srv, rec := newTLSServer(t, cert, ca, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.Error(t, err)
	var unknown x509.UnknownAuthorityError
	assert.True(t, errors.As(err, &unknown), "got %v", err)
	assert.Empty(t, rec.requests())
}

// newPiWorkerServer serves /mcp (204) and /piworker-ca.crt like PiWorker in
// tls.mode = "auto", with a real tlsca.Manager's certificates.
func newPiWorkerServer(t *testing.T) (m *tlsca.Manager, base string, rec *recorder) {
	t.Helper()
	m, err := tlsca.NewManager(tlsca.Options{
		Dir:        t.TempDir(),
		Hostname:   func() (string, error) { return "piworker-test", nil },
		Interfaces: func() ([]net.IP, error) { return nil, nil },
	})
	require.NoError(t, err)
	rec = &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /piworker-ca.crt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(m.CACertPEM()) })
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	// Not httptest.StartTLS: it adds its own certificate, which Go serves
	// instead of GetCertificate's when the client sends no SNI (IP hosts).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: rec.wrap(mux), ErrorLog: discardLogger()}
	go func() { _ = srv.Serve(tls.NewListener(ln, m.TLSConfig())) }()
	t.Cleanup(func() { _ = srv.Close() })
	return m, "https://" + ln.Addr().String(), rec
}

func TestFingerprintPinWorksWithPiWorkerCertificates(t *testing.T) {
	m, base, rec := newPiWorkerServer(t)
	c, err := newConnector(mustURL(t, base+"/mcp"), trustOptions{CAFingerprint: m.CAFingerprint()})
	require.NoError(t, err)
	resp, err := get(testCtx(t), c, base+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, []string{"POST /mcp"}, rec.paths(), "PiWorker presents its CA: nothing is downloaded")
}

func TestWrongFingerprintAgainstPiWorkerSendsNothing(t *testing.T) {
	m, base, rec := newPiWorkerServer(t)
	pinned := newTestCA(t, "PiWorker Local CA other")
	c, err := newConnector(mustURL(t, base+"/mcp"), trustOptions{CAFingerprint: pinned.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, base+"/mcp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match --ca-fingerprint")
	assert.Contains(t, err.Error(), m.CAFingerprint(), "the CA PiWorker presented is shown")
	assert.Empty(t, rec.requests(), "not even the public CA download")
}

func TestCAFilePinsTheGivenCA(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	other := newTestCA(t, "Other CA")
	srv, rec := newTLSServer(t, ca.serverCert(t, false, "127.0.0.1"), nil, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFile: ca.file(t)})
	require.NoError(t, err)
	resp, err := get(testCtx(t), c, srv.URL+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	c, err = newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFile: other.file(t)})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	var unknown x509.UnknownAuthorityError
	require.True(t, errors.As(err, &unknown), "got %v", err)
	assert.Equal(t, []string{"POST /mcp"}, rec.paths(), "only the trusted connection got through")
}

func TestCAFileAndFingerprintMustAgree(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	other := newTestCA(t, "Other CA")
	bundle := filepath.Join(t.TempDir(), "bundle.pem")
	require.NoError(t, os.WriteFile(bundle, append(other.pem(), ca.pem()...), 0o644))
	srv, _ := newTLSServer(t, other.serverCert(t, false, "127.0.0.1"), nil, nil)

	// Only the certificate matching the fingerprint is trusted.
	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFile: bundle, CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	var unknown x509.UnknownAuthorityError
	require.True(t, errors.As(err, &unknown), "got %v", err)

	_, err = newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFile: other.file(t), CAFingerprint: ca.fingerprint()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matches --ca-fingerprint")
}

func TestCAFileErrors(t *testing.T) {
	u := mustURL(t, "https://raspberrypi.local:8443/mcp")
	_, err := newConnector(u, trustOptions{CAFile: filepath.Join(t.TempDir(), "missing.crt")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--ca-file")

	notPEM := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(notPEM, []byte("hello"), 0o644))
	_, err = newConnector(u, trustOptions{CAFile: notPEM})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no certificate")
}

func TestCAFileAcceptsDER(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, _ := newTLSServer(t, ca.serverCert(t, false, "127.0.0.1"), nil, nil)
	der := filepath.Join(t.TempDir(), "ca.cer")
	require.NoError(t, os.WriteFile(der, ca.cert.Raw, 0o644))

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFile: der})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	require.NoError(t, err)
}

func TestSystemRootsByDefault(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, ca.serverCert(t, false, "127.0.0.1"), ca, nil)

	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{})
	require.NoError(t, err)
	_, err = get(testCtx(t), c, srv.URL+"/mcp")
	var unknown x509.UnknownAuthorityError
	require.True(t, errors.As(err, &unknown), "got %v", err)
	assert.Empty(t, rec.requests())
}

func TestPinningNeedsHTTPS(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	u := mustURL(t, "http://127.0.0.1:8080/mcp")
	_, err := newConnector(u, trustOptions{CAFingerprint: ca.fingerprint()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	_, err = newConnector(u, trustOptions{CAFile: ca.file(t)})
	require.Error(t, err)

	_, err = newConnector(u, trustOptions{})
	require.NoError(t, err)
}
