package tlsca

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
)

// FileCertificate serves an administrator-provided certificate and key
// (tls.mode = "custom"). Reload re-reads both files, typically on SIGHUP.
type FileCertificate struct {
	certFile, keyFile string

	mu   sync.RWMutex
	cert *tls.Certificate
}

// LoadFileCertificate loads the PEM certificate chain and key at the given
// paths.
func LoadFileCertificate(certFile, keyFile string) (*FileCertificate, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("tlsca: custom TLS needs both a certificate file and a key file")
	}
	fc := &FileCertificate{certFile: certFile, keyFile: keyFile}
	if err := fc.Reload(); err != nil {
		return nil, err
	}
	return fc, nil
}

// Reload replaces the served certificate with the current files. On error the
// previous certificate keeps being served.
func (f *FileCertificate) Reload() error {
	cert, err := tls.LoadX509KeyPair(f.certFile, f.keyFile)
	if err != nil {
		return fmt.Errorf("tlsca: load %s and %s: %w", f.certFile, f.keyFile, err)
	}
	f.mu.Lock()
	f.cert = &cert
	f.mu.Unlock()
	return nil
}

// TLSConfig returns a server configuration serving the loaded certificate.
func (f *FileCertificate) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		f.mu.RLock()
		defer f.mu.RUnlock()
		return f.cert, nil
	}}
}

// Status describes the loaded certificate. There is no local CA, so the
// fingerprint is empty.
func (f *FileCertificate) Status() Status {
	f.mu.RLock()
	leaf := f.cert.Leaf
	f.mu.RUnlock()
	return Status{SANs: leafSANs(leaf), NotAfter: leaf.NotAfter}
}

// CACertPEM returns nil: custom certificates are trusted through their own CA.
func (f *FileCertificate) CACertPEM() []byte { return nil }
