package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// fingerprintFormat is how errors describe a valid --ca-fingerprint: the form
// GET /api/tls, the editor and the startup log show.
const fingerprintFormat = `"SHA256:AB:CD:…" (the 64 hexadecimal digits of the CA's SHA-256 fingerprint, as shown under Settings → Trust this device)`

// parseFingerprint reads a SHA-256 certificate fingerprint. It accepts what
// PiWorker shows ("SHA256:AB:CD:…") in any letter case, with or without the
// "SHA256:" prefix and with colons, spaces or nothing between the bytes.
func parseFingerprint(s string) ([sha256.Size]byte, error) {
	var fp [sha256.Size]byte
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"sha256:", "sha-256:"} {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			s = s[len(prefix):]
			break
		}
	}
	digits := strings.Map(func(r rune) rune {
		if r == ':' || r == ' ' {
			return -1
		}
		return r
	}, s)
	if len(digits) != hex.EncodedLen(sha256.Size) {
		return fp, errors.New("invalid --ca-fingerprint: expected " + fingerprintFormat)
	}
	if _, err := hex.Decode(fp[:], []byte(digits)); err != nil {
		return fp, errors.New("invalid --ca-fingerprint: expected " + fingerprintFormat)
	}
	return fp, nil
}

// parseEndpoint validates --url: https, or plain http only to a loopback IP
// literal (PiWorker on this machine with tls.mode = "off"), since the token
// would otherwise cross the network in clear. Credentials, queries and
// fragments are refused so that nothing secret ends up in the URL, and errors
// never echo the URL.
func parseEndpoint(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("--url is required, e.g. --url https://raspberrypi.local:8443/mcp")
	}
	u, err := url.Parse(raw)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("invalid --url: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
			return nil, errors.New("invalid --url: plain http would send the token in clear; use the https:// address of PiWorker (port 8443 by default). Plain http is only accepted to 127.0.0.1 or [::1]")
		}
	default:
		return nil, errors.New("invalid --url: it must start with https://, e.g. https://raspberrypi.local:8443/mcp")
	}
	switch {
	case u.Hostname() == "":
		return nil, errors.New("invalid --url: it has no host, e.g. https://raspberrypi.local:8443/mcp")
	case u.User != nil:
		return nil, errors.New("invalid --url: it must not contain credentials; the token is read from --token-file")
	case u.RawQuery != "" || u.ForceQuery:
		return nil, errors.New("invalid --url: it must not have a query")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return nil, errors.New("invalid --url: it must not have a fragment")
	}
	return u, nil
}

// formatFingerprint renders a SHA-256 digest as PiWorker does.
func formatFingerprint(fp [sha256.Size]byte) string {
	var b strings.Builder
	b.WriteString("SHA256")
	for _, c := range fp {
		b.WriteByte(':')
		b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
	}
	return b.String()
}
