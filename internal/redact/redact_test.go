package redact

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLWithoutSecrets(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/v1/doors":                   "https://api.example.com/v1/doors",
		"https://user:pass@api.example.com/v1?key=abc#frag":  "https://api.example.com/v1?<redacted>",
		"https://api.example.com/v1#token=abc":               "https://api.example.com/v1",
		"http://user@host:8080/p":                            "http://host:8080/p",
		"https://api.example.com/hooks/123/secret-token?x=1": "https://api.example.com/hooks/123/secret-token?<redacted>",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, want, URLWithoutSecrets(u), raw)
	}
}

func TestURLsInText(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"no urls here":                    "no urls here",
		"plain https://a.example/x stays": "plain https://a.example/x stays",
		`Get "https://bob:hunter2@api.example.com/v1?apikey=abc": dial tcp: refused`: `Get "https://api.example.com/v1?<redacted>": dial tcp: refused`,
		"see https://api.example.com/x?token=abc.":                                   "see https://api.example.com/x?<redacted>.",
		"(mqtt://user:pw@broker.local:1883)":                                         "(mqtt://broker.local:1883)",
		"bad http://u:p%zz@host/x?k=1 end":                                           "bad http://<redacted> end",
		"two: http://a.example/?k=1 and HTTPS://u:p@b.example/y":                     "two: http://a.example/?<redacted> and https://b.example/y",
	}
	for in, want := range cases {
		assert.Equal(t, want, URLsInText(in), in)
	}
}
