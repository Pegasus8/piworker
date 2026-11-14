package builtin

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMTP is a tiny SMTP server that accepts one message per session and
// records the DATA section.
type fakeSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	data     []string
	auths    []string // AUTH command lines
	rcptCode string   // reply to RCPT TO (default 250)
}

func newFakeSMTP(t *testing.T, tlsCfg *tls.Config) *fakeSMTP {
	t.Helper()
	var ln net.Listener
	var err error
	if tlsCfg != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	require.NoError(t, err)
	s := &fakeSMTP{ln: ln, rcptCode: "250 ok"}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.session(c)
		}
	}()
	return s
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.data...)
}

func (s *fakeSMTP) authLines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...)
}

func (s *fakeSMTP) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250-fake")
			w("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.auths = append(s.auths, strings.TrimSpace(line))
			s.mu.Unlock()
			w("235 ok")
		case strings.HasPrefix(cmd, "MAIL"):
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			s.mu.Lock()
			code := s.rcptCode
			s.mu.Unlock()
			w(code)
		case cmd == "DATA":
			w("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = append(s.data, b.String())
			s.mu.Unlock()
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func selfSignedTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, pool
}

func newEmailAction(t *testing.T, cfg map[string]interface{}) *EmailAction {
	t.Helper()
	base := map[string]interface{}{"host": "127.0.0.1", "from": "pi@example.com", "to": "me@example.com"}
	for k, v := range cfg {
		base[k] = v
	}
	n, err := NewEmailAction(base)
	require.NoError(t, err)
	return n.(*EmailAction)
}

func TestEmailActionSendsPlain(t *testing.T) {
	s := newFakeSMTP(t, nil)
	a := newEmailAction(t, map[string]interface{}{"port": s.port(), "subject": "hello", "body": "temp {{payload}}"})

	_, err := a.Process(context.Background(), types.NewMessage("21", types.DataTypeString))
	require.NoError(t, err)
	msgs := s.messages()
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "Subject: hello\r\n")
	assert.Contains(t, msgs[0], "temp 21")
}

// Port 465 is implicit TLS: the server waits for a TLS ClientHello and never
// sends a plaintext greeting. smtp.SendMail used to hang there forever.
func TestEmailActionImplicitTLS(t *testing.T) {
	srvCfg, pool := selfSignedTLS(t)
	s := newFakeSMTP(t, srvCfg)
	a := newEmailAction(t, map[string]interface{}{
		"port": s.port(), "tls": "implicit", "username": "u", "password": "p",
	})
	a.rootCAs = pool

	_, err := a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	require.Len(t, s.messages(), 1)
}

func TestEmailActionPort465DefaultsToImplicitTLS(t *testing.T) {
	a := newEmailAction(t, map[string]interface{}{"port": 465})
	assert.True(t, a.implicitTLS())
	a = newEmailAction(t, map[string]interface{}{"port": 587})
	assert.False(t, a.implicitTLS())
}

func TestEmailActionRespectsContextOnSilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		var conns []net.Conn
		defer func() {
			for _, c := range conns {
				_ = c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c) // accept, never greet
		}
	}()

	a := newEmailAction(t, map[string]interface{}{"port": ln.Addr().(*net.TCPAddr).Port})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := a.Process(ctx, types.NewMessage("x", types.DataTypeString))
		done <- err
	}()
	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("email send ignored the context and hung on a silent server")
	}
}

func TestEmailActionPermanentRejectIsNotRetried(t *testing.T) {
	s := newFakeSMTP(t, nil)
	s.rcptCode = "550 no such user"
	a := newEmailAction(t, map[string]interface{}{"port": s.port()})
	_, err := a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.False(t, node.IsRetryable(err), "5xx replies are permanent")

	s.rcptCode = "451 try later"
	_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.True(t, node.IsRetryable(err), "4xx replies are transient")
}

func TestBuildEmailMessageBlocksHeaderInjection(t *testing.T) {
	raw := string(buildEmailMessage("pi@example.com\r\nBcc: a@evil",
		[]string{"me@example.com\nBcc: b@evil"},
		"hi\r\nBcc: c@evil", "body"))
	headers := raw[:strings.Index(raw, "\r\n\r\n")]
	for _, line := range strings.Split(headers, "\r\n") {
		assert.False(t, strings.HasPrefix(strings.ToLower(line), "bcc:"), "injected header line %q", line)
	}
	assert.Contains(t, headers, "Subject: hi Bcc: c@evil")
}

func TestBuildEmailMessageEncodesNonASCIISubject(t *testing.T) {
	raw := string(buildEmailMessage("pi@example.com", []string{"me@example.com"}, "Température élevée", "b"))
	assert.Contains(t, raw, "Subject: =?UTF-8?q?")
	assert.NotContains(t, raw, "Température")
	plain := string(buildEmailMessage("pi@example.com", []string{"me@example.com"}, "plain", "b"))
	assert.Contains(t, plain, "Subject: plain\r\n")
}

func TestEmailActionRejectsRecipientWithNewline(t *testing.T) {
	_, err := NewEmailAction(map[string]interface{}{
		"host": "h", "from": "pi@example.com", "to": "a@example.com\r\nRCPT TO:<x@evil>",
	})
	assert.Error(t, err)
}
