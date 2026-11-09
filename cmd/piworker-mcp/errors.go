package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	// maxErrorBodyBytes bounds what is read of an HTTP error body.
	maxErrorBodyBytes = 64 << 10
	// maxDetailRunes bounds the server text quoted in an error.
	maxDetailRunes = 300
)

// httpError is an HTTP error status from PiWorker.
type httpError struct {
	status     int
	detail     string // the server's explanation, sanitized
	retryAfter int    // seconds; -1 when unknown
	location   string // redirect target, sanitized
	endpoint   string
}

func (e *httpError) Error() string {
	code := fmt.Sprintf("HTTP %d %s", e.status, http.StatusText(e.status))
	detail := ""
	if e.detail != "" {
		detail = ": " + e.detail
	}
	switch {
	case e.status == http.StatusUnauthorized:
		return "PiWorker rejected the agent token (" + code + "): it is wrong, expired or revoked. Create a new token under Agents in the PiWorker editor and save it in the token file"
	case e.status == http.StatusForbidden:
		return "PiWorker refused the request (" + code + ")" + detail + ". Check the token's scopes and grants under Agents, and that the host name in --url is one PiWorker accepts (allowed_hosts)"
	case e.status == http.StatusNotFound:
		return "PiWorker has no MCP endpoint at " + e.endpoint + " (" + code + "). Check --url and that MCP is enabled on PiWorker ([mcp] enabled = true in piworker.toml)"
	case e.status == http.StatusTooManyRequests:
		when := "try again later"
		if e.retryAfter >= 0 {
			when = fmt.Sprintf("try again in %d seconds", e.retryAfter)
		}
		return "PiWorker's rate limit for this token was reached (" + code + "); " + when
	case e.status == http.StatusRequestEntityTooLarge:
		return "the message is too large for PiWorker (" + code + ")"
	case e.status >= 300 && e.status < 400:
		target := ""
		if e.location != "" {
			target = " to " + e.location
		}
		return "PiWorker answered with a redirect (" + code + ")" + target + "; the bridge does not follow redirects, so check --url"
	case e.status >= 500:
		return "PiWorker failed to handle the request (" + code + ")" + detail
	default:
		return "PiWorker rejected the request (" + code + ")" + detail
	}
}

func (e *httpError) data() map[string]any {
	d := map[string]any{"httpStatus": e.status}
	if e.retryAfter >= 0 {
		d["retryAfter"] = e.retryAfter
	}
	if e.detail != "" {
		d["detail"] = e.detail
	}
	return d
}

// passthroughError carries a JSON-RPC error PiWorker's MCP layer sent with an
// HTTP error status (e.g. protocol version negotiation): the client gets it
// as is.
type passthroughError struct{ e rpcError }

func (p *passthroughError) Error() string { return p.e.Message }

// statusError turns an HTTP error response into an error for the client.
func statusError(resp *http.Response, endpoint *url.URL) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	ct := mediaType(resp.Header.Get("Content-Type"))
	status := resp.StatusCode
	explained := status == http.StatusUnauthorized || status == http.StatusForbidden ||
		status == http.StatusNotFound || status == http.StatusTooManyRequests || status >= 500
	if e, ok := jsonrpcError(body, ct); ok && !explained && status >= 400 {
		return &passthroughError{e}
	}
	e := &httpError{status: status, detail: errorDetail(body, ct), retryAfter: -1, endpoint: endpoint.String()}
	if status == http.StatusTooManyRequests {
		if s, ok := retryAfterSeconds(resp.Header.Get("Retry-After"), time.Now()); ok {
			e.retryAfter = s
		}
	}
	if status >= 300 && status < 400 {
		e.location = sanitize(resp.Header.Get("Location"))
	}
	return e
}

// jsonrpcError reads a JSON-RPC error response body.
func jsonrpcError(body []byte, ct string) (rpcError, bool) {
	if ct != "application/json" {
		return rpcError{}, false
	}
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    json.Number     `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &msg) != nil || msg.JSONRPC != "2.0" || msg.Error == nil || msg.Result != nil {
		return rpcError{}, false
	}
	code, err := msg.Error.Code.Int64()
	if err != nil || code < math.MinInt32 || code > math.MaxInt32 {
		return rpcError{}, false
	}
	e := rpcError{Code: int(code), Message: msg.Error.Message}
	if len(msg.Error.Data) > 0 {
		e.Data = msg.Error.Data
	}
	return e, true
}

// errorDetail extracts the server's explanation from an error body: a
// JSON-RPC error message, a JSON {"error": …} or {"message": …}, or the first
// line of plain text.
func errorDetail(body []byte, ct string) string {
	var text string
	switch ct {
	case "application/json":
		var v struct {
			Error   json.RawMessage `json:"error"`
			Message string          `json:"message"`
		}
		if json.Unmarshal(body, &v) != nil {
			return ""
		}
		var s string
		var obj struct {
			Message string `json:"message"`
		}
		switch {
		case json.Unmarshal(v.Error, &s) == nil:
			text = s
		case json.Unmarshal(v.Error, &obj) == nil && obj.Message != "":
			text = obj.Message
		default:
			text = v.Message
		}
	case "text/plain", "":
		text, _, _ = strings.Cut(string(body), "\n")
	}
	return sanitize(text)
}

// sanitize makes server-provided text safe to quote: valid UTF-8, no control
// or invisible formatting characters, bounded length.
func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > maxDetailRunes {
		s = string(runes[:maxDetailRunes]) + "…"
	}
	return s
}

// retryAfterSeconds reads a Retry-After header: seconds or an HTTP date.
func retryAfterSeconds(h string, now time.Time) (int, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(h); err == nil {
		return n, n >= 0
	}
	t, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	return max(0, int(math.Ceil(t.Sub(now).Seconds()))), true
}

// toRPCError explains why a request got no answer from PiWorker.
func (r *relay) toRPCError(ctx context.Context, err error) rpcError {
	var pass *passthroughError
	var herr *httpError
	var berr *bridgeError
	switch {
	case errors.As(err, &pass):
		return pass.e
	case errors.As(err, &herr):
		return rpcError{Code: codeUpstream, Message: herr.Error(), Data: herr.data()}
	case errors.As(err, &berr):
		return rpcError{Code: codeUpstream, Message: berr.msg}
	case errors.Is(err, errNoAnswer):
		return rpcError{Code: codeUpstream, Message: err.Error()}
	case ctx.Err() != nil:
		if errors.Is(context.Cause(ctx), errTimeout) {
			return rpcError{Code: codeUpstream, Message: fmt.Sprintf("PiWorker did not answer within %s", r.cfg.RequestTimeout)}
		}
		return rpcError{Code: codeUpstream, Message: errShutdown.Error()}
	}
	return rpcError{Code: codeUpstream, Message: r.describeTransportError(err)}
}

// describeTransportError explains a failure to reach or verify PiWorker. It
// never quotes what an unverified server sent: the answer goes to the agent,
// and a device impersonating PiWorker must not be able to put words there.
func (r *relay) describeTransportError(err error) string {
	c := r.cfg.Connector
	var hostErr x509.HostnameError
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var verr *tls.CertificateVerificationError
	switch {
	case errors.As(err, &hostErr):
		if errors.As(err, &verr) && c.verifies(verr.UnverifiedCertificates) {
			return fmt.Sprintf("PiWorker's certificate is not valid for %s (it is valid for %s). Use a host name or address the certificate lists in --url, or add this one to tls.hosts in piworker.toml",
				r.cfg.Endpoint.Hostname(), certificateNames(verr.UnverifiedCertificates[0]))
		}
		return c.untrusted()
	case errors.As(err, &unknown):
		return c.untrusted()
	case errors.As(err, &invalid):
		return "PiWorker's certificate is not valid: " + invalidReason(invalid)
	case errors.As(err, &verr):
		return "PiWorker's certificate cannot be verified (it uses an unsupported algorithm or extension)"
	}
	return fmt.Sprintf("cannot reach PiWorker at %s: %s", hostPort(r.cfg.Endpoint), netFailure(err))
}

// invalidReason explains a certificate rejected for its validity period or
// its use; only the expiry detail (dates Go formats) is quoted.
func invalidReason(e x509.CertificateInvalidError) string {
	switch e.Reason {
	case x509.Expired:
		return "it has expired or is not valid yet (" + sanitize(e.Detail) + "). Check the date and time of this computer and of PiWorker"
	case x509.IncompatibleUsage:
		return "it is not meant for a server"
	default:
		return "it does not chain correctly to the trusted CA"
	}
}

// netFailure says why a connection failed with text the bridge controls.
// Errors that carry no server-chosen text (dial errors, timeouts, TLS
// alerts) are quoted; anything else, such as an unreadable certificate or a
// malformed HTTP answer, is only summarized, because Go quotes the
// offending bytes and the server may not have been verified yet.
func netFailure(err error) string {
	var opErr *net.OpError
	var netErr net.Error
	var recordErr tls.RecordHeaderError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return "the connection timed out"
	case errors.As(err, &recordErr), strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"):
		return "the server there does not answer with TLS; check that --url has PiWorker's https port (8443 by default)"
	case errors.As(err, &opErr):
		return sanitize(opErr.Error())
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "the server closed the connection"
	}
	return "the connection failed: the server's answer is not valid TLS or HTTP"
}

// quietError is an error whose text leaves out its cause's, which may quote
// an unverified server.
type quietError struct {
	msg   string
	cause error
}

func (e *quietError) Error() string { return e.msg }
func (e *quietError) Unwrap() error { return e.cause }

// certificateNames lists the names and addresses a verified certificate is
// valid for. Only host-name characters are kept, even from a trusted CA.
func certificateNames(cert *x509.Certificate) string {
	var names []string
	for _, name := range cert.DNSNames {
		if isHostName(name) {
			names = append(names, name)
		}
	}
	for _, ip := range cert.IPAddresses {
		names = append(names, ip.String())
	}
	if len(names) == 0 {
		return "no name"
	}
	if len(names) > 10 {
		names = append(names[:10], "…")
	}
	return strings.Join(names, ", ")
}

// isHostName reports whether s has the shape of a DNS name in a certificate
// (letters, digits, '-', '.', a '*' wildcard; at most 253 characters).
func isHostName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '.' && c != '*' {
			return false
		}
	}
	return true
}
