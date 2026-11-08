package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// NotifyAction sends a notification message to a webhook (Discord, Slack, ntfy).
type NotifyAction struct {
	*node.BaseNode

	url     string
	message string
	format  string // "discord", "slack", "ntfy", "raw"
	title   string
	timeout time.Duration
	client  *http.Client
}

// NewNotifyAction creates a new NotifyAction from configuration.
func NewNotifyAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	// Webhook URLs (Discord/Slack) embed a token, so resolve {{secret.NAME}}.
	url := expandSecrets(base.GetConfigString("url", ""))
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}

	format := base.GetConfigString("format", "discord")
	switch format {
	case "discord", "slack", "ntfy", "raw":
	default:
		format = "discord"
	}

	timeout := 15 * time.Second
	return &NotifyAction{
		BaseNode: base,
		url:      url,
		message:  base.GetConfigString("message", "{{payload}}"),
		format:   format,
		title:    base.GetConfigString("title", ""),
		timeout:  timeout,
		client:   newGuardedHTTPClient(timeout, base.GetConfigBool("allowPrivate", false)),
	}, nil
}

// Process renders the message and posts it to the configured webhook.
func (a *NotifyAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	text := renderTemplate(a.message, msg, ctx)

	var body io.Reader
	contentType := "application/json"
	extraHeaders := map[string]string{}

	switch a.format {
	case "slack":
		b, _ := json.Marshal(map[string]string{"text": text})
		body = bytes.NewReader(b)
	case "ntfy":
		body = strings.NewReader(text)
		contentType = "text/plain"
		if a.title != "" {
			extraHeaders["Title"] = a.title
		}
	case "raw":
		body = strings.NewReader(text)
		contentType = "text/plain"
	default: // discord
		b, _ := json.Marshal(map[string]string{"content": text})
		body = bytes.NewReader(b)
	}

	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", redactURLError(err, a.url, displayURL(a.url)))
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		// Connection-level failures are transient and safe to retry (the POST is
		// idempotent enough for a best-effort notification).
		// The webhook URL usually embeds its token, so it is redacted.
		return nil, node.Transient(fmt.Errorf("notification request failed: %w", redactURLError(err, a.url, displayURL(a.url))))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	// A rejected notification (bad webhook, revoked token, rate limit) must
	// fail the node rather than report success. 5xx and 429 are transient.
	if resp.StatusCode >= 400 {
		statusErr := fmt.Errorf("notification webhook %s returned HTTP %d", displayURL(a.url), resp.StatusCode)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, node.Transient(statusErr)
		}
		return nil, statusErr
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("notifyStatus", resp.StatusCode)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *NotifyAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *NotifyAction) Validate() error {
	if a.url == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func notifyActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"url": {
				Type:        "string",
				Title:       "Webhook URL",
				Description: "The Discord/Slack/ntfy webhook URL (it embeds a token; use {{secret.NAME}})",
				Sensitive:   true,
			},
			"format": {
				Type:         "string",
				Title:        "Format",
				Description:  "Payload format for the destination service",
				Default:      "discord",
				Enum:         []string{"discord", "slack", "ntfy", "raw"},
				AgentVisible: true,
			},
			"message": {
				Type:        "string",
				Title:       "Message",
				Description: "Message text (supports {{payload.field}} templates)",
				Default:     "{{payload}}",
			},
			"title": {
				Type:        "string",
				Title:       "Title",
				Description: "Optional title (ntfy only)",
				Default:     "",
			},
			"allowPrivate": {
				Type:         "boolean",
				Title:        "Allow Private Addresses",
				Description:  "Allow notifying loopback/private hosts (off by default to prevent SSRF)",
				Default:      false,
				AgentVisible: true,
			},
		},
		Required: []string{"url"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (a *NotifyAction) GetConfigSchema() node.ConfigSchema {
	return notifyActionConfigSchema()
}

// NotifyActionInfo returns the node type info for registration.
func NotifyActionInfo() node.NodeTypeInfo {
	schema := notifyActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-notify",
		Name:        "Notify",
		Description: "Send a notification to Discord, Slack, or ntfy",
		Documentation: `## Notify

Sends a message to a chat/push service via its incoming webhook. Pick the format
that matches your destination.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| url | string | - | Incoming webhook URL (required) |
| format | enum | discord | discord, slack, ntfy, or raw |
| message | string | {{payload}} | Message text (supports templates) |
| title | string | - | Optional title (ntfy) |
| allowPrivate | boolean | false | Allow private/loopback hosts |

## Formats

| Format | Body |
|--------|------|
| discord | ` + "`{\"content\": ...}`" + ` |
| slack | ` + "`{\"text\": ...}`" + ` |
| ntfy | raw text, optional Title header |
| raw | raw text |

## Errors

An HTTP 4xx response fails the node (permanent); 5xx, 429 and network errors
are transient and retried. The webhook URL's path and query (where services
put their token) are redacted from error messages.
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "bell",
	}
}

func init() {
	info := NotifyActionInfo()
	if err := node.Register(info, NewNotifyAction); err != nil {
		panic("failed to register notify action: " + err.Error())
	}
}
