package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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

	timeout := time.Duration(timeoutMs) * time.Millisecond
	allowPrivate := base.GetConfigBool("allowPrivate", false)

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
		client:       newGuardedHTTPClient(timeout, allowPrivate),
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

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
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
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified()
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
	// Expand templates in URL
	expandedURL := renderTemplate(a.url, msg)

	// Validate URL: reject parse errors, non-http(s) schemes, and empty hosts.
	// url.Parse is very lenient (it accepts relative paths and exotic schemes),
	// so explicit scheme/host checks are required.
	parsedURL, err := url.Parse(expandedURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return nil, fmt.Errorf("URL has no host: %q", expandedURL)
	}

	// Prepare request body
	var bodyReader io.Reader
	if a.body != "" && (a.method == "POST" || a.method == "PUT" || a.method == "PATCH") {
		expandedBody := renderTemplate(a.body, msg)
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
		expandedValue := renderTemplate(v, msg)
		req.Header.Set(k, expandedValue)
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
	outputMsg.SetMeta("httpURL", expandedURL)

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
				Description: "Request URL (supports {{payload.field}} templates)",
				Default:     "",
			},
			"method": {
				Type:        "string",
				Title:       "Method",
				Description: "HTTP method",
				Default:     "GET",
				Enum:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
			},
			"headers": {
				Type:        "object",
				Title:       "Headers",
				Description: "HTTP headers (key-value pairs)",
			},
			"body": {
				Type:        "string",
				Title:       "Body",
				Description: "Request body (supports templates, for POST/PUT/PATCH)",
				Default:     "",
			},
			"bodyType": {
				Type:        "string",
				Title:       "Body Type",
				Description: "Content type for the request body",
				Default:     "json",
				Enum:        []string{"json", "text", "form"},
			},
			"timeout": {
				Type:        "number",
				Title:       "Timeout (ms)",
				Description: "Request timeout in milliseconds",
				Default:     30000,
				Minimum:     &minTimeout,
				Maximum:     &maxTimeout,
			},
			"outputMode": {
				Type:        "string",
				Title:       "Output Mode",
				Description: "What to return: full response, just body, or just status",
				Default:     "body",
				Enum:        []string{"body", "full", "status"},
			},
			"allowPrivate": {
				Type:        "boolean",
				Title:       "Allow Private Addresses",
				Description: "Allow requests to loopback/private/link-local addresses (needed for LAN/local services). Off by default to prevent SSRF.",
				Default:     false,
			},
			"maxResponseSize": {
				Type:        "number",
				Title:       "Max Response Size (bytes)",
				Description: "Maximum response body size to read (protects against memory exhaustion)",
				Default:     DefaultMaxResponseBytes,
			},
			"retryable": {
				Type:        "boolean",
				Title:       "Retry on Failure",
				Description: "Retry transient failures even for non-idempotent methods (POST/PATCH). May cause duplicate requests.",
				Default:     false,
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
| headers | object | {} | Custom headers (key-value pairs) |
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
- **meta.httpURL** - Expanded URL used

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
