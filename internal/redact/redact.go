// Package redact hides credentials that URLs carry (userinfo, query string,
// fragment) in text shown outside the server: node errors, agent results.
// It is shared by the builtin nodes and the service layer.
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

// URLWithoutSecrets returns scheme://host/path, dropping userinfo, query and
// fragment so errors keep enough to debug without exposing credentials. A
// dropped query is shown as "?<redacted>".
func URLWithoutSecrets(u *url.URL) string {
	shown := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	if u.RawQuery != "" {
		return shown.String() + "?<redacted>"
	}
	return shown.String()
}

// urlInText matches a URL with a scheme up to whitespace or a quote.
var urlInText = regexp.MustCompile("(?i)\\b[a-z][a-z0-9+.-]*://[^\\s\"'<>`]+")

// URLsInText rewrites every URL in s that carries userinfo, a query string or
// a fragment with URLWithoutSecrets. Trailing sentence punctuation is kept
// out of the URL; other URLs and text are left as they are.
func URLsInText(s string) string {
	return urlInText.ReplaceAllStringFunc(s, func(m string) string {
		trimmed := strings.TrimRight(m, ".,;:!?)]}")
		tail := m[len(trimmed):]
		u, err := url.Parse(trimmed)
		if err != nil {
			// Unparseable: keep only what precedes a userinfo or query.
			if strings.ContainsAny(trimmed, "@?#") {
				return trimmed[:strings.Index(trimmed, "://")+3] + "<redacted>" + tail
			}
			return m
		}
		if u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.RawFragment == "" && !u.ForceQuery {
			return m
		}
		return URLWithoutSecrets(u) + tail
	})
}
