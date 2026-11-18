package main

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Pegasus8/piworker/internal/tlsca"
)

func TestParseFingerprintAcceptsTheFormatsPiWorkerShows(t *testing.T) {
	der := []byte("a certificate")
	want := sha256.Sum256(der)
	canonical := tlsca.Fingerprint(der) // what GET /api/tls and the editor report
	bare := strings.ReplaceAll(strings.TrimPrefix(canonical, "SHA256:"), ":", "")

	for name, in := range map[string]string{
		"canonical":            canonical,
		"lower case":           strings.ToLower(canonical),
		"without colons":       "SHA256:" + bare,
		"without prefix":       strings.TrimPrefix(canonical, "SHA256:"),
		"bare hex":             strings.ToLower(bare),
		"spaces around":        "  " + canonical + "\n",
		"sha-256 prefix":       "SHA-256:" + bare,
		"space separated hex":  strings.ReplaceAll(strings.TrimPrefix(canonical, "SHA256:"), ":", " "),
		"mixed case and colon": "sha256:" + strings.ToLower(strings.TrimPrefix(canonical, "SHA256:")),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseFingerprint(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestParseFingerprintRejectsMalformedValues(t *testing.T) {
	full := strings.Repeat("AB", 32)
	for name, in := range map[string]string{
		"empty":        "",
		"too short":    "SHA256:" + full[:62],
		"too long":     "SHA256:" + full + "AB",
		"not hex":      "SHA256:" + strings.Repeat("ZZ", 32),
		"other digest": "SHA1:" + full[:40],
		"other prefix": "MD5:" + full,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseFingerprint(in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "SHA256:AB:CD", "the error shows the expected format")
		})
	}
}

func TestParseEndpointAcceptsHTTPSAndLoopbackHTTP(t *testing.T) {
	for raw, host := range map[string]string{
		"https://raspberrypi.local:8443/mcp": "raspberrypi.local",
		"https://192.168.1.20:8443/mcp":      "192.168.1.20",
		"HTTPS://Pi.Example:8443/mcp":        "Pi.Example",
		"http://127.0.0.1:8080/mcp":          "127.0.0.1",
		"http://[::1]:8080/mcp":              "::1",
	} {
		t.Run(raw, func(t *testing.T) {
			u, err := parseEndpoint(raw)
			require.NoError(t, err)
			assert.Equal(t, host, u.Hostname())
			assert.Equal(t, "/mcp", u.Path)
		})
	}
}

func TestParseEndpointRejectsInsecureOrAmbiguousURLs(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                     "--url is required",
		"raspberrypi.local:8443/mcp":           "https://",
		"ftp://raspberrypi.local/mcp":          "https://",
		"http://raspberrypi.local:8080/mcp":    "plain http",
		"http://192.168.1.20:8080/mcp":         "plain http",
		"http://localhost:8080/mcp":            "plain http",
		"https:///mcp":                         "host",
		"https://user:secret@pi.local/mcp":     "credentials",
		"https://pi.local:8443/mcp#fragment":   "fragment",
		"https://pi.local:8443/mcp?token=pwk_": "query",
		"https://pi.local:8443/%zz":            "invalid --url",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseEndpoint(raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)
			assert.NotContains(t, err.Error(), "secret", "credentials are never echoed")
		})
	}
}

func TestFormatFingerprintMatchesPiWorker(t *testing.T) {
	der := []byte("another certificate")
	assert.Equal(t, tlsca.Fingerprint(der), formatFingerprint(sha256.Sum256(der)))
}
