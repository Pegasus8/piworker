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
	// fingerprint is the CA to find before the first request when it is
	// pinned by fingerprint only.
	fingerprint *[sha256.Size]byte

	lock       chan struct{} // guards httpClient; a channel so waiting honours ctx
	httpClient *http.Client
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
		c.httpClient = newHTTPClient(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, c.dialTimeout)
		return c, nil
	}
	if fp != nil {
		c.description = "the CA with fingerprint " + formatFingerprint(*fp)
		c.fingerprint = fp
		return c, nil
	}
	c.description = "the system's trusted CAs"
	c.httpClient = newHTTPClient(&tls.Config{MinVersion: tls.VersionTLS12}, c.dialTimeout)
	return c, nil
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
	c.httpClient = newHTTPClient(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, c.dialTimeout)
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
		return nil, fmt.Errorf("cannot reach PiWorker at %s: %w", addr, err)
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
// matches the pin.
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
		return nil, fmt.Errorf("cannot download PiWorker's CA certificate from %s: %w", caURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server presented no certificate matching --ca-fingerprint and serves no CA certificate at %s (HTTP %d). PiWorker publishes its CA there with tls.mode = \"auto\"; for other certificates pass the CA with --ca-file", caURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCABytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot download PiWorker's CA certificate from %s: %w", caURL, err)
	}
	if len(body) > maxCABytes {
		return nil, fmt.Errorf("the CA certificate at %s is too large", caURL)
	}
	certs, err := parseCertificates(body)
	if err != nil {
		return nil, fmt.Errorf("the CA certificate at %s is not readable: %w", caURL, err)
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
