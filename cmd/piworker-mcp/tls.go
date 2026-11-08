package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// caPath is where PiWorker publishes its local CA certificate (public, no
	// credentials needed; docs/design/mcp.md §2).
	caPath = "/piworker-ca.crt"
	// maxCABytes bounds a downloaded or loaded CA certificate file.
	maxCABytes = 1 << 20
	// defaultDialTimeout bounds connecting and the TLS handshake.
	defaultDialTimeout = 10 * time.Second
	// defaultMaxInFlight bounds the HTTP requests in flight at once.
	defaultMaxInFlight = 8
)

// trustOptions say which server certificates the bridge accepts.
type trustOptions struct {
	// CAFile is a PEM (or DER) file with the CA certificate(s) to trust.
	CAFile string
	// CAFingerprint pins the CA by its SHA-256 fingerprint.
	CAFingerprint string
}

// connector hands out the HTTP client that carries the token. Every
// connection it makes is verified for the --url host name against the
// trusted CA: the pinned one (--ca-file and/or --ca-fingerprint) or the
// system roots. Only the trusted CA is accepted, never also the system roots.
type connector struct {
	endpoint    *url.URL
	description string
	dialTimeout time.Duration
	// pinned is set when the trusted CA is PiWorker's, not the system roots.
	pinned bool
	// fingerprint is the CA to find before the first request when it is
	// pinned by fingerprint only.
	fingerprint *[sha256.Size]byte

	lock       chan struct{} // guards httpClient; a channel so waiting honours ctx
	httpClient *http.Client

	anchorMu sync.Mutex
	// anchor is the trusted CA once known (nil: the system roots); see
	// verifies.
	anchor      *x509.CertPool
	anchorKnown bool
}

func newConnector(endpoint *url.URL, opts trustOptions) (*connector, error) {
	c := &connector{endpoint: endpoint, dialTimeout: defaultDialTimeout, lock: make(chan struct{}, 1)}
	if !strings.EqualFold(endpoint.Scheme, "https") {
		if opts.CAFile != "" || opts.CAFingerprint != "" {
			return nil, errors.New("--ca-file and --ca-fingerprint need an https:// --url")
		}
		c.description = "plain http on the loopback interface"
		c.httpClient = newHTTPClient(nil, c.dialTimeout)
		return c, nil
	}
	var fp *[sha256.Size]byte
	if opts.CAFingerprint != "" {
		parsed, err := parseFingerprint(opts.CAFingerprint)
		if err != nil {
			return nil, err
		}
		fp = &parsed
	}
	if opts.CAFile != "" {
		certs, err := loadCAFile(opts.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		trusted := 0
		for _, cert := range certs {
			if fp == nil || sha256.Sum256(cert.Raw) == *fp {
				pool.AddCert(cert)
				trusted++
			}
		}
		if trusted == 0 {
			return nil, fmt.Errorf("no certificate in --ca-file %s matches --ca-fingerprint %s", opts.CAFile, formatFingerprint(*fp))
		}
		c.description = "the CA in " + opts.CAFile
		if fp != nil {
			c.description += " (" + formatFingerprint(*fp) + ")"
		}
		c.pinned = true
		c.trust(pool)
		return c, nil
	}
	if fp != nil {
		c.description = "the CA with fingerprint " + formatFingerprint(*fp)
		c.pinned = true
		c.fingerprint = fp
		return c, nil
	}
	c.description = "the system's trusted CAs"
	c.trust(nil)
	return c, nil
}

// trust sets the trust anchor (nil: the system roots) and the HTTP client
// that verifies against it.
func (c *connector) trust(roots *x509.CertPool) {
	c.anchorMu.Lock()
	c.anchor, c.anchorKnown = roots, true
	c.anchorMu.Unlock()
	c.httpClient = newHTTPClient(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, c.dialTimeout)
}

// verifies reports whether chain, as a server presented it (leaf first), is
// a server certificate signed by the trusted CA, whatever names it covers.
// Go checks the host name before the signature, so a certificate that is
// "not valid for" the host may come from anyone: only what verifies here is
// worth quoting.
func (c *connector) verifies(chain []*x509.Certificate) bool {
	c.anchorMu.Lock()
	roots, known := c.anchor, c.anchorKnown
	c.anchorMu.Unlock()
	if !known || len(chain) == 0 {
		return false
	}
	intermediates := x509.NewCertPool()
	for _, cert := range chain[1:] {
		intermediates.AddCert(cert)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err == nil
}

// untrusted explains a certificate that is not signed by the trusted CA.
func (c *connector) untrusted() string {
	if c.pinned {
		return "PiWorker's certificate is not signed by a trusted CA: the bridge trusts only " + c.description +
			", so the token was not sent. If PiWorker's CA was regenerated, update --ca-fingerprint or --ca-file from Settings → Trust this device in the editor; otherwise another device is answering at this address"
	}
	return "PiWorker's certificate is not signed by a trusted CA (the bridge trusts " + c.description +
		"). Pass --ca-fingerprint with the fingerprint shown under Settings → Trust this device in the editor, or --ca-file with PiWorker's CA certificate"
}

// client returns the HTTP client for token-bearing requests. With a CA pinned
// by fingerprint only, the first call finds that CA (see discoverCA); a
// failure is returned and retried on the next call.
func (c *connector) client(ctx context.Context) (*http.Client, error) {
	select {
	case c.lock <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.lock }()
	if c.httpClient != nil {
		return c.httpClient, nil
	}
	ca, err := discoverCA(ctx, c.endpoint, *c.fingerprint, c.dialTimeout)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	c.trust(pool)
	return c.httpClient, nil
}

// discoverCA finds the certificate whose SHA-256 fingerprint is fp, without
// trusting the server: only a certificate with the pinned fingerprint is ever
// returned, and every later connection is verified against it. PiWorker
// presents its CA after its server certificate (tls.mode = "auto"), so the
// CA is normally found in the handshake and nothing is requested; when the
// server presents a root that is not the pinned CA, nothing is requested at
// all. Only a server that presents no root (e.g. tls.mode = "custom" with a
// leaf-only chain) gets a GET of the public /caPath. Neither step sends the
// token.
func discoverCA(ctx context.Context, endpoint *url.URL, fp [sha256.Size]byte, timeout time.Duration) (*x509.Certificate, error) {
	addr := hostPort(endpoint)
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config: &tls.Config{
			// Only to read the presented chain; nothing is sent over this
			// connection and the server is not trusted because of it.
			InsecureSkipVerify: true, //nolint:gosec // see above
			ServerName:         endpoint.Hostname(),
			MinVersion:         tls.VersionTLS12,
		},
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	conn, err := dialer.DialContext(dctx, "tcp", addr)
	cancel()
	if err != nil {
		return nil, &quietError{fmt.Sprintf("cannot reach PiWorker at %s: %s", addr, netFailure(err)), err}
	}
	presented := conn.(*tls.Conn).ConnectionState().PeerCertificates
	_ = conn.Close()
	if ca := withFingerprint(presented, fp); ca != nil {
		return ca, nil
	}
	if n := len(presented); n > 0 && isSelfSigned(presented[n-1]) {
		return nil, mismatchError(fp, addr, presented[n-1:])
	}
	served, err := downloadCA(ctx, endpoint, timeout)
	if err != nil {
		return nil, err
	}
	if ca := withFingerprint(served, fp); ca != nil {
		return ca, nil
	}
	return nil, mismatchError(fp, addr, served)
}

// downloadCA fetches the CA certificate PiWorker publishes. The request
// carries no credentials and the answer is only used if its fingerprint
// matches the pin. The server is not verified, so errors never quote what it
// sent.
func downloadCA(ctx context.Context, endpoint *url.URL, timeout time.Duration) ([]*x509.Certificate, error) {
	caURL := (&url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host, Path: caPath}).String()
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // the certificate is checked against the pinned fingerprint
			TLSHandshakeTimeout: timeout,
			DisableKeepAlives:   true,
		},
		CheckRedirect: noRedirects,
		Timeout:       timeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, caURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, &quietError{fmt.Sprintf("cannot download PiWorker's CA certificate from %s: %s", caURL, netFailure(err)), err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server presented no certificate matching --ca-fingerprint and serves no CA certificate at %s (HTTP %d). PiWorker publishes its CA there with tls.mode = \"auto\"; for other certificates pass the CA with --ca-file", caURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCABytes+1))
	if err != nil {
		return nil, &quietError{fmt.Sprintf("cannot download PiWorker's CA certificate from %s: %s", caURL, netFailure(err)), err}
	}
	if len(body) > maxCABytes {
		return nil, fmt.Errorf("the CA certificate at %s is too large", caURL)
	}
	certs, err := parseCertificates(body)
	if err != nil {
		return nil, &quietError{fmt.Sprintf("the CA certificate at %s is not a readable certificate; pass PiWorker's CA with --ca-file", caURL), err}
	}
	return certs, nil
}

func mismatchError(fp [sha256.Size]byte, addr string, got []*x509.Certificate) error {
	seen := make([]string, 0, len(got))
	for _, cert := range got {
		seen = append(seen, formatFingerprint(sha256.Sum256(cert.Raw)))
	}
	return fmt.Errorf("PiWorker's CA does not match --ca-fingerprint: expected %s, but the server at %s has %s. The token was not sent. If PiWorker's CA was regenerated, copy the new fingerprint from Settings → Trust this device in the editor; otherwise another device is answering at this address",
		formatFingerprint(fp), addr, strings.Join(seen, ", "))
}

func withFingerprint(certs []*x509.Certificate, fp [sha256.Size]byte) *x509.Certificate {
	for _, cert := range certs {
		if sha256.Sum256(cert.Raw) == fp {
			return cert
		}
	}
	return nil
}

func isSelfSigned(cert *x509.Certificate) bool {
	return bytes.Equal(cert.RawIssuer, cert.RawSubject) && cert.CheckSignatureFrom(cert) == nil
}

// loadCAFile reads the certificates of --ca-file (PEM, or a DER certificate).
func loadCAFile(path string) ([]*x509.Certificate, error) {
	path, err := expandHome(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read --ca-file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCABytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read --ca-file: %w", err)
	}
	if len(data) > maxCABytes {
		return nil, fmt.Errorf("--ca-file %s is too large for a CA certificate", path)
	}
	certs, err := parseCertificates(data)
	if err != nil {
		return nil, fmt.Errorf("--ca-file %s: %w", path, err)
	}
	return certs, nil
}

// parseCertificates reads every PEM CERTIFICATE block of data, or data as DER
// when it holds no PEM block.
func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	sawPEM := false
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		sawPEM = true
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	if !sawPEM {
		if der, err := x509.ParseCertificates(data); err == nil {
			certs = der
		}
	}
	if len(certs) == 0 {
		return nil, errors.New(`no certificate found (expected PEM "-----BEGIN CERTIFICATE-----" or DER)`)
	}
	return certs, nil
}

// hostPort is the endpoint's host:port, with the scheme's default port.
func hostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
		if strings.EqualFold(u.Scheme, "http") {
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// newHTTPClient builds the client for PiWorker. It never uses an HTTP proxy
// (PiWorker is on the local network) and never follows redirects, so the
// token only goes to --url.
func newHTTPClient(tlsConf *tls.Config, dialTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSClientConfig:       tlsConf,
			TLSHandshakeTimeout:   dialTimeout,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          defaultMaxInFlight,
			MaxIdleConnsPerHost:   defaultMaxInFlight,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: noRedirects,
	}
}

func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func userAgent() string { return "piworker-mcp/" + version }
