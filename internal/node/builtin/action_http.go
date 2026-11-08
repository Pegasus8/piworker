package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// templateRe matches {{variable}} / {{variable.field}} placeholders. It is
// compiled once at package init rather than on every Process call (the pattern
// is constant and the old per-call regexp.MustCompile was pure overhead on the
// hottest path).
var templateRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// DefaultMaxResponseBytes caps how much of an HTTP response body is read, so a
// hostile or runaway endpoint can't OOM a memory-constrained device.
const DefaultMaxResponseBytes int64 = 10 << 20 // 10 MiB

// HTTPAction makes HTTP requests to external APIs.
type HTTPAction struct {
	*node.BaseNode

	url          string
	method       string
	headers      map[string]string
	body         string
	bodyType     string // "json", "text", "form"
	timeout      time.Duration
	outputMode   string // "full", "body", "status"
	allowPrivate bool   // allow requests to loopback/private/link-local addresses
	maxBytes     int64  // maximum response body size to read
	retryable    bool   // opt-in: retry transient failures even for non-idempotent methods

	client *http.Client
}

// NewHTTPAction creates a new HTTPAction with the given configuration.
func NewHTTPAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	// Parse URL (required)
	urlStr := base.GetConfigString("url", "")
	if urlStr == "" {
		return nil, fmt.Errorf("url is required")
	}

	// Parse method
	method := strings.ToUpper(base.GetConfigString("method", "GET"))
	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true}
	if !validMethods[method] {
		return nil, fmt.Errorf("invalid HTTP method: %s", method)
	}

	// Parse headers
	headers := make(map[string]string)
	if headersRaw, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range headersRaw {
			if vs, ok := v.(string); ok {
				headers[k] = vs
			}
		}
	}

	// Parse timeout (ms)
	timeoutMs := base.GetConfigInt("timeout", 30000)
	if timeoutMs < 1000 {
		timeoutMs = 1000 // Minimum 1 second
	}
	if timeoutMs > 60000 {
		timeoutMs = 60000 // Maximum 60 seconds
	}

	// Parse body type
	bodyType := base.GetConfigString("bodyType", "json")
	if bodyType != "json" && bodyType != "text" && bodyType != "form" {
		bodyType = "json"
	}

	// Parse output mode
	outputMode := base.GetConfigString("outputMode", "body")
	if outputMode != "full" && outputMode != "body" && outputMode != "status" {
		outputMode = "body"
	}

	// Parse response size cap
	maxBytes := int64(base.GetConfigInt("maxResponseSize", int(DefaultMaxResponseBytes)))
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}

	// Secrets may only go to a destination written in the configuration.
	urlSecret := usesSecret(urlStr)
	var secretHeaders []string
	for k, v := range headers {
		if usesSecret(v) {
			secretHeaders = append(secretHeaders, k)
		}
	}
	if urlSecret || len(secretHeaders) > 0 {
		if err := checkSecretDestination(urlStr); err != nil {
			return nil, err
		}
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	allowPrivate := base.GetConfigBool("allowPrivate", false)
	client := newGuardedHTTPClient(timeout, allowPrivate)
	if urlSecret || len(secretHeaders) > 0 {
		client.CheckRedirect = keepSecretsOnHost(secretHeaders, urlSecret)
	}

	action := &HTTPAction{
		BaseNode:     base,
		url:          urlStr,
		method:       method,
		headers:      headers,
		body:         base.GetConfigString("body", ""),
		bodyType:     bodyType,
		timeout:      timeout,
		outputMode:   outputMode,
		allowPrivate: allowPrivate,
		maxBytes:     maxBytes,
		retryable:    base.GetConfigBool("retryable", false),
		client:       client,
	}

	return action, nil
}

// newGuardedHTTPClient builds an http.Client whose dialer rejects connections
// to non-public addresses (loopback, link-local, RFC1918/ULA private ranges)
// unless allowPrivate is set. This defends against SSRF: the request URL is
// templated from user-controlled payload, so without this guard a crafted
// payload could reach cloud metadata endpoints or internal services. The check
// runs on the resolved IP and then dials that exact IP, closing the DNS-rebinding
// gap. Redirects are covered too, since the client dials every hop through here.
func newGuardedHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}

	// With a proxy the client only dials the proxy, which then connects to the
	// (possibly internal) target itself, bypassing the IP check below. Honour
	// HTTP(S)_PROXY only when private targets are allowed anyway.
	var proxy func(*http.Request) (*url.URL, error)
	if allowPrivate {
		proxy = http.ProxyFromEnvironment
	}

	transport := &http.Transport{
		Proxy:                 proxy,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if allowPrivate {
				return dialer.DialContext(ctx, network, addr)
			}

			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}

			for _, ip := range ips {
				if isBlockedIP(ip.IP) {
					return nil, fmt.Errorf("blocked connection to non-public address %s (set allowPrivate to allow)", ip.IP)
				}
			}

			// Dial a validated IP directly to avoid a TOCTOU re-resolution.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}

	return &http.Client{Timeout: timeout, Transport: transport}
}

// isBlockedIP reports whether ip is a non-public address that SSRF protection
// should refuse to connect to.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	// 6to4 (2002::/16) embeds an IPv4 address in bits 16-48; a relay would
	// forward to it, so judge it by that address.
	if addr.Is6() && sixToFour.Contains(addr) {
		b := addr.As16()
		return isBlockedIP(net.IPv4(b[2], b[3], b[4], b[5]))
	}
	return false
}

// blockedPrefixes are special-purpose ranges (RFC 6890 and friends) that are
// not covered by the net.IP helpers above but still reach local, carrier or
// non-routable networks.
var blockedPrefixes = func() []netip.Prefix {
	cidrs := []string{
		"0.0.0.0/8",       // "this network"; 0.x reaches the local host on Linux
		"100.64.0.0/10",   // carrier-grade NAT (also Tailscale)
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved, incl. 255.255.255.255 broadcast
		"64:ff9b::/96",    // NAT64 well-known prefix (maps to any IPv4)
		"64:ff9b:1::/48",  // local-use NAT64
		"100::/64",        // discard-only
		"2001::/32",       // Teredo
		"2001:db8::/32",   // documentation
		"2001:10::/28",    // ORCHID
		"2001:20::/28",    // ORCHIDv2
	}
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}()

var sixToFour = netip.MustParsePrefix("2002::/16")

// urlEscaperAt picks the escaping for a template value placed in a URL after
// the literal text prefix:
//   - before the scheme separator (e.g. "{{payload.url}}" or "{{vars.base}}/x"):
//     raw, so a whole URL or base URL can still come from a variable;
//   - authority and path: url.PathEscape, so a value stays within one segment
//     ("/" "?" "#" are escaped);
//   - query: url.QueryEscape, so "&" and "=" can't add parameters;
//   - fragment: url.PathEscape.
func urlEscaperAt(prefix string) func(string) string {
	switch {
	case strings.Contains(prefix, "#"):
		return url.PathEscape
	case strings.Contains(prefix, "?"):
		return url.QueryEscape
	case strings.Contains(prefix, "://"):
		return url.PathEscape
	default:
		return nil
	}
}

// bodyEscaperAt picks the escaping for a template value in the request body.
// json: values inside a JSON string literal are JSON-string escaped; values
// outside strings (numbers, objects, a whole-body "{{payload}}") are inserted
// raw, as before. form: values are query-escaped unless the placeholder
// starts the body (whole-value template). text: always raw.
func (a *HTTPAction) bodyEscaperAt(prefix string) func(string) string {
	switch a.bodyType {
	case "json":
		if insideJSONString(prefix) {
			return jsonStringEscape
		}
	case "form":
		if prefix != "" {
			return url.QueryEscape
		}
	}
	return nil
}

// insideJSONString reports whether the end of the JSON fragment s lies inside
// a string literal.
func insideJSONString(s string) bool {
	in, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case in && c == '\\':
			escaped = true
		case c == '"':
			in = !in
		}
	}
	return in
}

// jsonStringEscape escapes s for use inside a JSON string literal (without
// the surrounding quotes).
func jsonStringEscape(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return ""
	}
	out := strings.TrimSuffix(b.String(), "\n")
	return out[1 : len(out)-1]
}

// isIdempotent reports whether the HTTP method is safe to retry on a transient
// failure without risking duplicate side effects.
func (a *HTTPAction) isIdempotent() bool {
	switch a.method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// Process makes the HTTP request and returns the response.
func (a *HTTPAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	// Secrets written in the URL (userinfo, query) and header templates are
	// resolved now, at request time, so a rotated secret applies without a
	// redeploy. Text that came from the message never resolves them.
	sec, err := newRequestSecrets()
	if err != nil {
		return nil, err
	}

	// Expand templates in URL, escaping each value for where it lands.
	// shownURL keeps secrets as placeholders, for errors and metadata.
	expandedURL, shownURL, err := sec.requestURL(a.url, msg, ctx)
	if err != nil {
		return nil, err
	}

	// Validate URL: reject parse errors, non-http(s) schemes, and empty hosts.
	// url.Parse is very lenient (it accepts relative paths and exotic schemes),
	// so explicit scheme/host checks are required.
	parsedURL, err := url.Parse(expandedURL)
	if err != nil {
		if len(sec.values) > 0 { // the error would quote the resolved URL
			return nil, fmt.Errorf("invalid URL %q", shownURL)
		}
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return nil, fmt.Errorf("URL has no host: %q", shownURL)
	}

	// Prepare request body
	var bodyReader io.Reader
	if a.body != "" && (a.method == "POST" || a.method == "PUT" || a.method == "PATCH") {
		expandedBody := renderTemplateEscaped(a.body, msg, ctx, a.bodyEscaperAt)
		bodyReader = strings.NewReader(expandedBody)
	}

	// Create request with context
	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, a.method, expandedURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default content type based on body type
	if a.body != "" {
		switch a.bodyType {
		case "json":
			req.Header.Set("Content-Type", "application/json")
		case "form":
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		case "text":
			req.Header.Set("Content-Type", "text/plain")
		}
	}

	// Set custom headers (can override content type)
	for k, v := range a.headers {
		req.Header.Set(k, renderTemplateWith(v, msg, ctx, nil, sec.headerValue))
	}
	if sec.err != nil {
		return nil, sec.err
	}

	// Execute request
	startTime := time.Now()
	resp, err := a.client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		// Connection-level failures (DNS, refused, reset, timeout) are transient.
		// Retry them only for idempotent methods, or when the user opted in via
		// `retryable`, so non-idempotent requests (POST/PATCH) are never re-sent
		// and duplicated by the runtime's retry logic.
		// Hide the query string and userinfo, where API keys usually live.
		err = redactURLError(err, req.URL.String(), urlWithoutSecrets(req.URL), append([]string{req.URL.RawQuery}, sec.values...)...)
		wrapped := fmt.Errorf("HTTP request failed: %w", err)
		if a.isIdempotent() || a.retryable {
			return nil, node.Transient(wrapped)
		}
		return nil, wrapped
	}
	defer resp.Body.Close()

	// Read the response body with a hard size cap so a huge or streaming
	// response can't exhaust memory. Read one extra byte to detect overflow.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, a.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if int64(len(respBody)) > a.maxBytes {
		return nil, fmt.Errorf("response body exceeds maximum allowed size of %d bytes", a.maxBytes)
	}

	// Build output based on outputMode
	var output interface{}
	var outputType types.DataType

	switch a.outputMode {
	case "full":
		// Return full response details
		respHeaders := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				respHeaders[k] = v[0]
			}
		}

		// Try to parse body as JSON
		var bodyParsed interface{}
		if json.Unmarshal(respBody, &bodyParsed) != nil {
			bodyParsed = string(respBody)
		}

		output = map[string]interface{}{
			"status":     resp.StatusCode,
			"statusText": resp.Status,
			"headers":    respHeaders,
			"body":       bodyParsed,
			"duration":   duration.Milliseconds(),
		}
		outputType = types.DataTypeObject

	case "status":
		// Return only status code
		output = resp.StatusCode
		outputType = types.DataTypeNumber

	default: // "body"
		// Try to parse as JSON, otherwise return as string
		var bodyParsed interface{}
		if json.Unmarshal(respBody, &bodyParsed) == nil {
			output = bodyParsed
			outputType = types.DataTypeObject
		} else {
			output = string(respBody)
			outputType = types.DataTypeString
		}
	}

	// Create output message
	outputMsg := msg.Clone()
	outputMsg.Payload = output
	outputMsg.PayloadType = outputType
	outputMsg.SourcePort = "output"
	outputMsg.SetMeta("httpStatus", resp.StatusCode)
	outputMsg.SetMeta("httpDuration", duration.Milliseconds())
	outputMsg.SetMeta("httpURL", shownURL)

	return []*types.Message{outputMsg}, nil
}

// Ports returns the port definitions for this node.
func (a *HTTPAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{
		{
			ID:       "input",
			Name:     "Input",
			DataType: types.DataTypeAny,
			Required: true,
		},
	}, []types.Port{
		{
			ID:       "output",
			Name:     "Output",
			DataType: types.DataTypeAny,
			Multiple: true,
		},
	}
}

// Validate checks if the configuration is valid.
func (a *HTTPAction) Validate() error {
	if a.url == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (a *HTTPAction) GetConfigSchema() node.ConfigSchema {
	return httpActionConfigSchema()
}

func httpActionConfigSchema() node.ConfigSchema {
	minTimeout := float64(1000)
	maxTimeout := float64(60000)

	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"url": {
				Type:        "string",
				Title:       "URL",
				Description: "Request URL (supports {{payload.field}} templates; {{secret.NAME}} only in the user:password@ part and the query string, and only when the URL starts with a literal scheme and host)",
				Default:     "",
			},
			"method": {
				Type:            "string",
				Title:           "Method",
				Description:     "HTTP method",
				Default:         "GET",
				Enum:            []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
				CaseInsensitive: true,
				AgentVisible:    true,
			},
			"headers": {
				Type:        "object",
				Title:       "Headers",
				Description: "HTTP headers (name → value pairs); values support {{secret.NAME}} and message templates",
				Sensitive:   true,
			},
			"body": {
				Type:        "string",
				Title:       "Body",
				Description: "Request body (supports templates, for POST/PUT/PATCH)",
				Default:     "",
			},
			"bodyType": {
				Type:         "string",
				Title:        "Body Type",
				Description:  "Content type for the request body",
				Default:      "json",
				Enum:         []string{"json", "text", "form"},
				AgentVisible: true,
			},
			"timeout": {
				Type:         "number",
				Title:        "Timeout (ms)",
				Description:  "Request timeout, in milliseconds (1000-60000)",
				Default:      30000,
				Minimum:      &minTimeout,
				Maximum:      &maxTimeout,
				Unit:         "milliseconds",
				AgentVisible: true,
			},
			"outputMode": {
				Type:         "string",
				Title:        "Output Mode",
				Description:  "What to return: full response, just body, or just status",
				Default:      "body",
				Enum:         []string{"body", "full", "status"},
				AgentVisible: true,
			},
			"allowPrivate": {
				Type:         "boolean",
				Title:        "Allow Private Addresses",
				Description:  "Allow requests to loopback/private/link-local/CGNAT and other reserved addresses (needed for LAN/local services). Off by default to prevent SSRF; while off, HTTP(S)_PROXY settings are ignored.",
				Default:      false,
				AgentVisible: true,
			},
			"maxResponseSize": {
				Type:         "number",
				Title:        "Max Response Size (bytes)",
				Description:  "Maximum response body size to read, in bytes (protects against memory exhaustion)",
				Default:      DefaultMaxResponseBytes,
				Unit:         "bytes",
				AgentVisible: true,
			},
			"retryable": {
				Type:         "boolean",
				Title:        "Retry on Failure",
				Description:  "Retry transient failures even for non-idempotent methods (POST/PATCH). May cause duplicate requests.",
				Default:      false,
				AgentVisible: true,
			},
		},
		Required: []string{"url"},
	}
}

// HTTPActionInfo returns the node type info for registration.
func HTTPActionInfo() node.NodeTypeInfo {
	schema := httpActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-http",
		Name:        "HTTP Request",
		Description: "Makes HTTP requests to external APIs",
		Documentation: `## HTTP Request

Makes HTTP requests to external APIs and services. Supports all common HTTP methods and template variables.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| url | string | - | Request URL (required, supports templates) |
| method | enum | GET | HTTP method |
| headers | object | {} | Custom headers (key-value pairs; values support secrets) |
| body | string | - | Request body (for POST/PUT/PATCH) |
| bodyType | enum | json | Content type: json, text, form |
| timeout | number | 30000 | Timeout in ms (1000-60000) |
| outputMode | enum | body | What to return |

## Template Variables

Use ` + "`{{variable}}`" + ` in URL, headers, and body:

| Variable | Description |
|----------|-------------|
| {{payload}} | Entire payload as JSON |
| {{payload.field}} | Specific field |
| {{payload.nested.field}} | Nested field |
| {{meta.key}} | Metadata value |
| {{topic}} | Message topic |
| {{id}} | Message ID |
| {{timestamp}} | ISO timestamp |

## Escaping

Values are escaped for the place they are inserted, so payload data can't
change the structure of the request:

| Where | Escaping |
|-------|----------|
| URL, before ` + "`://`" + ` (e.g. ` + "`{{payload.url}}`" + `, ` + "`{{vars.base}}/x`" + `) | none: a whole/base URL may come from a variable |
| URL path segment | percent-encoded; ` + "`/ ? #`" + ` stay inside the segment |
| URL query string | query-encoded; ` + "`& =`" + ` can't add parameters |
| JSON body, inside a "string" | JSON string escaping (quotes, backslashes, newlines) |
| JSON body, outside strings | inserted as is (numbers, objects, whole ` + "`{{payload}}`" + `) |
| form body | query-encoded, except a whole-body placeholder |
| text body, headers | none |

## Secrets

Write ` + "`{{secret.NAME}}`" + ` (managed under **Secrets**) instead of a
credential:

- in **header values**, e.g. ` + "`Authorization: Bearer {{secret.api_token}}`" + `;
- in the URL's **user:password@** part, e.g.
  ` + "`https://{{secret.user}}:{{secret.pass}}@api.example.com/`" + `;
- in the URL's **query string**, e.g. ` + "`?apikey={{secret.api_key}}`" + `
  (the value is query-encoded).

Secrets are read when each request is sent, so rotating one takes effect
without redeploying. They are only resolved where they are written in this
node's configuration, never inside message data, and never in the body. A
secret in the scheme, host, path or fragment of the URL fails the deploy; a
secret that does not exist fails the request before anything is sent.
**meta.httpURL** and error messages show the placeholder, not the value.

When the URL or a header uses a secret, the URL must start with a literal
scheme and host (` + "`https://api.example.com/...`" + `): a URL that starts with a
placeholder (` + "`{{payload.url}}`" + `, ` + "`{{flowVars.base}}/x`" + `) or takes part of
its scheme, host or port from one fails the deploy with *secrets require a
fixed scheme and host*, because message data could then send the secret
anywhere. Placeholders in the path, query string and fragment stay allowed.
On a redirect to another host, headers that carry secrets and a Referer that
would repeat a query secret are dropped.

## Output Modes

| Mode | Returns |
|------|---------|
| body | Parsed response body (JSON→object, text→string) |
| full | Object with status, headers, body, duration |
| status | HTTP status code only (number) |

## Output Metadata

All modes add:

- **meta.httpStatus** - HTTP status code
- **meta.httpDuration** - Request duration in ms
- **meta.httpURL** - Expanded URL used (secrets shown as placeholders)

## Examples

**GET with URL parameter:**
` + "```" + `
url: https://api.example.com/users/{{payload.userId}}
method: GET
` + "```" + `

**POST with JSON body:**
` + "```" + `
url: https://api.example.com/orders
method: POST
body: {"product": "{{payload.product}}", "qty": {{payload.quantity}}}
` + "```" + `

**Auth header:**
` + "```" + `
headers:
  Authorization: Bearer {{meta.token}}
` + "```" + `

## Use Cases

- REST API integration
- Webhook notifications
- External service calls
- Data synchronization
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
		Inputs: []types.Port{
			{
				ID:       "input",
				Name:     "Input",
				DataType: types.DataTypeAny,
				Required: true,
			},
		},
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeAny,
				Multiple: true,
			},
		},
		Icon:   "globe",
		Config: &schema,
	}
}

func init() {
	info := HTTPActionInfo()
	if err := node.Register(info, NewHTTPAction); err != nil {
		panic("failed to register HTTP action: " + err.Error())
	}
}
