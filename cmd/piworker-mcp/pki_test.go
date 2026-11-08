package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testCA is a throwaway certificate authority for TLS tests.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCA(t *testing.T, name string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &testCA{cert: cert, key: key}
}

func (ca *testCA) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

func (ca *testCA) fingerprint() string { return formatFingerprint(sha256.Sum256(ca.cert.Raw)) }

// file writes the CA certificate to a PEM file and returns its path.
func (ca *testCA) file(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(path, ca.pem(), 0o644))
	return path
}

// serverCert issues a server certificate for hosts (DNS names or IPs). With
// withCA the chain also carries the CA certificate.
func (ca *testCA) serverCert(t *testing.T, withCA bool, hosts ...string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: hosts[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	chain := [][]byte{der}
	if withCA {
		chain = append(chain, ca.cert.Raw)
	}
	return tls.Certificate{Certificate: chain, PrivateKey: key}
}

// seenRequest is what a test server records about each HTTP request.
type seenRequest struct {
	Method, Path       string
	Authorization      string
	ProtocolVersion    string
	MCPMethod, MCPName string
	Accept             string
	ContentType        string
}

// recorder wraps a handler and records every request that reaches it.
type recorder struct {
	mu   sync.Mutex
	seen []seenRequest
}

func (rec *recorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.seen = append(rec.seen, seenRequest{
			Method:          r.Method,
			Path:            r.URL.Path,
			Authorization:   r.Header.Get("Authorization"),
			ProtocolVersion: r.Header.Get("Mcp-Protocol-Version"),
			MCPMethod:       r.Header.Get("Mcp-Method"),
			MCPName:         r.Header.Get("Mcp-Name"),
			Accept:          r.Header.Get("Accept"),
			ContentType:     r.Header.Get("Content-Type"),
		})
		rec.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (rec *recorder) requests() []seenRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]seenRequest(nil), rec.seen...)
}

func (rec *recorder) paths() []string {
	var out []string
	for _, r := range rec.requests() {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

// newTLSServer starts an HTTPS server presenting cert, recording requests.
// It serves ca at /piworker-ca.crt, like PiWorker, when ca is not nil.
func newTLSServer(t *testing.T, cert tls.Certificate, ca *testCA, mcp http.Handler) (*httptest.Server, *recorder) {
	t.Helper()
	mux := http.NewServeMux()
	if ca != nil {
		mux.HandleFunc("GET /piworker-ca.crt", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-x509-ca-cert")
			_, _ = w.Write(ca.pem())
		})
	}
	if mcp == nil {
		mcp = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.Handle("/mcp", mcp)
	rec := &recorder{}
	srv := httptest.NewUnstartedServer(rec.wrap(mux))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.Config.ErrorLog = discardLogger()
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, rec
}
