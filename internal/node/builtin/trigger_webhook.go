package builtin

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// WebhookTrigger fires a message when an HTTP request hits its webhook path.
// It registers itself on the shared webhook hub when started.
type WebhookTrigger struct {
	*node.BaseNode

	path   string
	method string // optional method filter ("" = any)
	token  string // optional shared secret; may reference {{secret.NAME}}

	hub *webhook.Hub
	log zerolog.Logger

	// tokenUnresolved is set while the token's secret is missing or empty, so
	// that state is logged once when it starts and once when it ends instead
	// of on every rejected request.
	tokenUnresolved atomic.Bool

	mu      sync.Mutex
	running bool
	out     chan<- *types.Message
	ctx     context.Context
	count   uint64
}

// NewWebhookTrigger creates a new WebhookTrigger from configuration.
func NewWebhookTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	path := strings.Trim(base.GetConfigString("path", ""), "/")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	return &WebhookTrigger{
		BaseNode: base,
		path:     path,
		method:   strings.ToUpper(strings.TrimSpace(base.GetConfigString("method", ""))),
		token:    base.GetConfigString("token", ""),
		hub:      webhook.DefaultHub,
		log:      log.Logger,
	}, nil
}

// Process is unused for trigger nodes but must be implemented.
func (t *WebhookTrigger) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// Ports returns the port definitions for this node.
func (t *WebhookTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{
		{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true},
	}
}

// Validate checks the configuration.
func (t *WebhookTrigger) Validate() error {
	if t.path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// Start registers the webhook path on the hub. A token referencing an
// undefined secret fails the start (and so the deploy).
func (t *WebhookTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	if t.token != "" {
		if _, err := expandSecretsStrict(ctx, t.token); err != nil {
			return fmt.Errorf("webhook token: %w", err)
		}
	}

	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger already running")
	}
	t.running = true
	t.out = out
	t.ctx = ctx
	t.mu.Unlock()

	if err := t.hub.Register(t.path, t.handle); err != nil {
		t.mu.Lock()
		t.running = false
		t.out = nil
		t.ctx = nil
		t.mu.Unlock()
		return err
	}
	return nil
}

// Stop unregisters the webhook path.
func (t *WebhookTrigger) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	path := t.path
	t.out = nil
	t.ctx = nil
	t.mu.Unlock()

	t.hub.Unregister(path)
	return nil
}

// handle is invoked by the hub for each incoming request on this path. Its
// result becomes the HTTP status the caller sees.
func (t *WebhookTrigger) handle(req webhook.Request) webhook.Result {
	t.mu.Lock()
	out := t.out
	ctx := t.ctx
	running := t.running
	t.mu.Unlock()

	if !running || out == nil {
		return webhook.Dropped
	}

	// Optional method filter.
	if t.method != "" && req.Method != t.method {
		return webhook.MethodNotAllowed
	}

	// Optional shared-secret check (the hub extracts ?token=... or the
	// X-Webhook-Token header into req.Token). Secrets are resolved per request
	// so a rotated secret applies at once; one that is missing or empty
	// rejects every request instead of disabling the check. Constant-time
	// comparison so response timing doesn't leak how much of a guess was right.
	if t.token != "" {
		expected, err := expandSecretsStrict(ctx, t.token) // ctx: the one Start was given
		if err != nil || expected == "" {
			if t.tokenUnresolved.CompareAndSwap(false, true) {
				t.log.Warn().Err(err).Str("path", t.path).Msg("webhook token resolves to nothing; rejecting every request until its secret is set")
			}
			return webhook.Unauthorized
		}
		if t.tokenUnresolved.CompareAndSwap(true, false) {
			t.log.Info().Str("path", t.path).Msg("webhook token resolves again; accepting requests with the right token")
		}
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(expected)) != 1 {
			return webhook.Unauthorized
		}
	}

	t.mu.Lock()
	t.count++
	count := t.count
	t.mu.Unlock()

	payload := map[string]interface{}{
		"method":    req.Method,
		"path":      req.Path,
		"query":     flattenQuery(req.Query),
		"headers":   req.Headers,
		"body":      req.Body,
		"count":     count,
		"timestamp": time.Now().UnixMilli(),
	}

	msg := types.NewMessage(payload, types.DataTypeObject)
	msg.SourcePort = "output"
	msg.Topic = "webhook"

	select {
	case <-ctx.Done():
		return webhook.Dropped
	case out <- msg:
		return webhook.Accepted
	default:
		metrics.IncrementMessagesDropped()
		t.log.Warn().Str("path", t.path).Msg("webhook message dropped: output channel full")
		return webhook.Dropped
	}
}

// flattenQuery collapses single-value query params to scalars for nicer payloads.
func flattenQuery(q map[string][]string) map[string]interface{} {
	out := make(map[string]interface{}, len(q))
	for k, v := range q {
		if len(v) == 1 {
			out[k] = v[0]
		} else {
			out[k] = v
		}
	}
	return out
}

// webhookTriggerConfigSchema is shared by GetConfigSchema and the node info so
// the UI can render a configuration form for this node.
func webhookTriggerConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"path": {
				Type:        "string",
				Title:       "Path",
				Description: "URL path segment under /webhooks/ (e.g. 'github-push')",
			},
			"method": {
				Type:         "string",
				Title:        "Method Filter",
				Description:  "Only accept this HTTP method; leave empty to accept any",
				Default:      "",
				AgentVisible: true,
			},
			"token": {
				Type:        "string",
				Title:       "Secret Token",
				Description: "Optional shared secret, required as ?token=... or the X-Webhook-Token header ({{secret.NAME}} supported)",
				Default:     "",
				Sensitive:   true,
			},
		},
		Required: []string{"path"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (t *WebhookTrigger) GetConfigSchema() node.ConfigSchema {
	return webhookTriggerConfigSchema()
}

// WebhookTriggerInfo returns the node type info for registration.
func WebhookTriggerInfo() node.NodeTypeInfo {
	schema := webhookTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-webhook",
		Name:        "Webhook",
		Description: "Triggers when an HTTP request hits its webhook URL",
		Documentation: `## Webhook

Starts a flow when an HTTP request is received at its dedicated URL. Ideal for
integrations: GitHub/GitLab hooks, IFTTT, Zapier, payment callbacks, or any
service that can POST to a URL.

## URL

` + "```" + `
http(s)://<host>/webhooks/<path>
` + "```" + `

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| path | string | - | Path segment under /webhooks/ (required, unique per flow) |
| method | string | (any) | Only accept this HTTP method |
| token | string | - | Optional shared secret (` + "`?token=`" + ` or ` + "`X-Webhook-Token`" + ` header); ` + "`{{secret.NAME}}`" + ` supported |

## Output

Each request produces a message with:

- **payload.method** - HTTP method
- **payload.path** - the webhook path
- **payload.query** - query parameters
- **payload.headers** - request headers
- **payload.body** - parsed JSON body (or raw string)
- **payload.count** - requests received since start
- **topic** - always "webhook"

## Security

The webhook endpoint is public (no login session). Set a **token** to require a shared
secret, and avoid exposing destructive flows without one.

Prefer storing the token under **Secrets** and writing ` + "`{{secret.NAME}}`" + `
here. The secret is read on every request, so rotating it applies at once.
Deploying fails if the secret does not exist; if it is later deleted or emptied,
every request is rejected with 401 (the check is never disabled).

The token, the ` + "`token`" + ` query parameter and the Cookie, Authorization,
X-Webhook-Token and X-PiWorker-* headers are never included in the payload.

## Responses

| Status | Meaning |
|--------|---------|
| 200 | Accepted into the flow |
| 400 | Request body could not be read |
| 401 | Missing or wrong token |
| 404 | No webhook on this path (flow not deployed) |
| 405 | Method doesn't match the method filter |
| 413 | Body larger than 1 MiB |
| 503 | Flow busy (queue full) or stopping; retry later |
`,
		Category: types.NodeCategoryInput,
		Risk:     node.RiskSafe,
		Inputs:   nil,
		Outputs: []types.Port{
			{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true},
		},
		Config: &schema,
		Icon:   "webhook",
	}
}

func init() {
	info := WebhookTriggerInfo()
	if err := node.Register(info, NewWebhookTrigger); err != nil {
		panic("failed to register webhook trigger: " + err.Error())
	}
}
