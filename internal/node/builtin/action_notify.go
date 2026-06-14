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
	text := renderTemplate(a.message, msg)

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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		// Connection-level failures are transient and safe to retry (the POST is
		// idempotent enough for a best-effort notification).
		return nil, node.Transient(fmt.Errorf("notification request failed: %w", err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

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
				Description: "The Discord/Slack/ntfy webhook URL",
			},
			"format": {
				Type:        "string",
				Title:       "Format",
				Description: "Payload format for the destination service",
				Default:     "discord",
				Enum:        []string{"discord", "slack", "ntfy", "raw"},
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
				Type:        "boolean",
				Title:       "Allow Private Addresses",
				Description: "Allow notifying loopback/private hosts (off by default to prevent SSRF)",
				Default:     false,
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
`,
		Category: types.NodeCategoryOutput,
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
