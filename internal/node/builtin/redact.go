package builtin

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Pegasus8/piworker/internal/redact"
)

// redactedURLError hides a secret-bearing request URL (a bot token in the
// path, a webhook token, ...) from an error that ends up in logs, the flow
// event stream and the UI. net/http embeds the full URL in every *url.Error.
type redactedURLError struct {
	cause error
	msg   string
}

func (e *redactedURLError) Error() string { return e.msg }

// Unwrap exposes the underlying cause (e.g. a net.Error or context error) but
// not the original *url.Error, which still carries the secret URL.
func (e *redactedURLError) Unwrap() error { return e.cause }

// redactURLError rewrites err so neither rawURL nor any of the extra secrets
// appear in its message. shown replaces the URL in the output.
func redactURLError(err error, rawURL, shown string, secrets ...string) error {
	if err == nil {
		return nil
	}
	var cause error
	var ue *url.Error
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	msg := err.Error()
	if ue != nil {
		// Re-format like url.Error does (it %q-quotes the URL, so a plain
		// substring replace would miss escaped characters).
		msg = fmt.Sprintf("%s %q: %v", ue.Op, shown, ue.Err)
	}
	for _, s := range append([]string{rawURL}, secrets...) {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "<redacted>")
		}
	}
	return &redactedURLError{cause: cause, msg: msg}
}

// displayURL returns scheme://host for logging, hiding the path and query
// where webhook services put their tokens.
func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<redacted url>"
	}
	return u.Scheme + "://" + u.Host + "/<redacted>"
}

// urlWithoutSecrets returns scheme://host/path, dropping userinfo, query and
// fragment so errors keep enough to debug without exposing credentials. The
// service layer redacts agent results with the same helper.
func urlWithoutSecrets(u *url.URL) string { return redact.URLWithoutSecrets(u) }
