package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
)

// requestSecrets resolves the {{secret.NAME}} placeholders written in an HTTP
// node's configured URL and header templates for one request. It is used
// through renderTemplateWith, which only offers it placeholders of the
// template itself: text that came from the message is never resolved.
type requestSecrets struct {
	nonce  string   // random per request, so a payload cannot forge a sentinel
	refs   []string // secret names referenced by the URL, by sentinel index
	values []string // every resolved value, to redact them from errors
	err    error    // first missing secret
}

func newRequestSecrets() (*requestSecrets, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("cannot generate request nonce: %w", err)
	}
	return &requestSecrets{nonce: hex.EncodeToString(b)}, nil
}

// secretRef returns NAME for a {{secret.NAME}} placeholder.
func secretRef(parts []string) (string, bool) {
	if parts[0] != "secret" || len(parts) < 2 {
		return "", false
	}
	return parts[1], true
}

// lookup returns the current value of a secret, recording a missing one.
func (s *requestSecrets) lookup(name string) string {
	v, ok := secrets.DefaultStore.Get(name)
	if !ok {
		if s.err == nil {
			s.err = fmt.Errorf("secret %q is not defined (referenced as {{secret.%s}})", name, name)
		}
		return ""
	}
	s.values = append(s.values, v)
	return v
}

// headerValue resolves secret placeholders of a header template.
func (s *requestSecrets) headerValue(parts []string) (string, bool) {
	name, ok := secretRef(parts)
	if !ok {
		return "", false
	}
	return s.lookup(name), true
}

// sentinel is an unreserved-characters-only stand-in for URL secret i: it is
// never escaped and survives url.Parse in any position.
func (s *requestSecrets) sentinel(i int) string {
	return "pwsecret" + s.nonce + "n" + strconv.Itoa(i) + "z"
}

// urlSentinel stands in for a URL secret placeholder until the URL is parsed
// and the secret's position is known.
func (s *requestSecrets) urlSentinel(parts []string) (string, bool) {
	name, ok := secretRef(parts)
	if !ok {
		return "", false
	}
	s.refs = append(s.refs, name)
	return s.sentinel(len(s.refs) - 1), true
}

// requestURL renders tmpl for msg. Message values are escaped for where they
// land (urlEscaperAt); {{secret.NAME}} placeholders written in tmpl are
// resolved only in the userinfo and the query string, the places credentials
// belong. It returns the URL to request and the URL to show, in which secrets
// stay as placeholders.
func (s *requestSecrets) requestURL(tmpl string, msg *types.Message, ctx context.Context) (target, shown string, err error) {
	rendered := renderTemplateWith(tmpl, msg, ctx, urlEscaperAt, s.urlSentinel)
	if len(s.refs) == 0 {
		return rendered, rendered, nil
	}
	shown = rendered
	for i, name := range s.refs {
		shown = strings.ReplaceAll(shown, s.sentinel(i), "{{secret."+name+"}}")
	}

	u, err := url.Parse(rendered)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL %q", shown)
	}
	for i, name := range s.refs {
		sentinel := s.sentinel(i)
		for _, part := range []string{u.Scheme, u.Opaque, u.Host, u.Path, u.RawPath, u.Fragment, u.RawFragment} {
			if strings.Contains(part, sentinel) {
				return "", "", fmt.Errorf("{{secret.%s}} may only be used in the URL's user:password@ part or query string", name)
			}
		}
	}

	resolve := func(text string, escape func(string) string) string {
		for i, name := range s.refs {
			if sentinel := s.sentinel(i); strings.Contains(text, sentinel) {
				text = strings.ReplaceAll(text, sentinel, escape(s.lookup(name)))
			}
		}
		return text
	}
	raw := func(v string) string { return v }
	if u.User != nil {
		user := resolve(u.User.Username(), raw)
		if pass, ok := u.User.Password(); ok {
			u.User = url.UserPassword(user, resolve(pass, raw))
		} else {
			u.User = url.User(user)
		}
	}
	u.RawQuery = resolve(u.RawQuery, url.QueryEscape)
	if s.err != nil {
		return "", "", s.err
	}
	return u.String(), shown, nil
}

// usesSecret reports whether a configured template references {{secret.NAME}}.
func usesSecret(tmpl string) bool {
	for _, m := range templateRe.FindAllString(tmpl, -1) {
		if _, ok := secretRef(placeholderParts(m)); ok {
			return true
		}
	}
	return false
}

// errDynamicSecretDestination refuses a configuration that would let message
// data choose where secrets are sent.
var errDynamicSecretDestination = errors.New("secrets require a fixed scheme and host: " +
	"when the URL or a header uses {{secret.NAME}}, the URL must start with a literal " +
	"scheme and host (e.g. https://api.example.com/...), not with a placeholder or variable")

// checkSecretDestination is the deploy-time check behind
// errDynamicSecretDestination. Every placeholder other than a secret is
// replaced by a random marker (a payload, meta or variable value may hold any
// text, and a variable may have been set from a payload); the URL is then
// parsed, and its scheme and host must be present and free of the marker.
// Placeholders in the userinfo, path, query and fragment stay allowed: their
// values are escaped so that they cannot move the host (urlEscaperAt). A
// secret outside the userinfo and query string is refused here too, as it
// would be on every request.
func checkSecretDestination(urlTmpl string) error {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("cannot generate marker: %w", err)
	}
	marker := "pwdynamic" + hex.EncodeToString(b)
	var refs []string
	rendered := templateRe.ReplaceAllStringFunc(urlTmpl, func(m string) string {
		if name, ok := secretRef(placeholderParts(m)); ok {
			refs = append(refs, name)
			return marker + "s" + strconv.Itoa(len(refs)-1) + "z"
		}
		return marker
	})
	u, err := url.Parse(rendered)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", urlTmpl, errDynamicSecretDestination)
	}
	for i, name := range refs {
		sentinel := marker + "s" + strconv.Itoa(i) + "z"
		for _, part := range []string{u.Scheme, u.Opaque, u.Host, u.Path, u.RawPath, u.Fragment, u.RawFragment} {
			if strings.Contains(part, sentinel) {
				return fmt.Errorf("{{secret.%s}} may only be used in the URL's user:password@ part or query string", name)
			}
		}
	}
	if strings.HasPrefix(rendered, marker) || u.Scheme == "" || u.Host == "" ||
		strings.Contains(u.Scheme, marker) || strings.Contains(u.Host, marker) {
		return errDynamicSecretDestination
	}
	return nil
}

// maxRedirects matches net/http's default redirect limit.
const maxRedirects = 10

// keepSecretsOnHost returns a CheckRedirect policy that sends secrets only to
// the host of the original request: on a redirect to any other host it drops
// the headers whose configured value uses a secret and, when the URL uses one
// (in the query string), the Referer that would repeat it. Without it a fixed
// host's open redirect, reachable through a payload-chosen path, could
// forward them.
func keepSecretsOnHost(secretHeaders []string, urlSecret bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return nil
		}
		for _, name := range secretHeaders {
			req.Header.Del(name)
		}
		if urlSecret {
			req.Header.Del("Referer")
		}
		return nil
	}
}
