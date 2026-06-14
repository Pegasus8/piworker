package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// TelegramAction sends a message via a Telegram bot.
type TelegramAction struct {
	*node.BaseNode

	botToken  string
	chatID    string
	message   string
	parseMode string // "", "Markdown", "MarkdownV2", "HTML"
	timeout   time.Duration
	client    *http.Client
}

// NewTelegramAction creates a new TelegramAction from configuration.
func NewTelegramAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	// Resolve {{secret.NAME}} so the token can be kept out of the flow blob.
	botToken := expandSecrets(base.GetConfigString("botToken", ""))
	if botToken == "" {
		return nil, fmt.Errorf("botToken is required")
	}
	chatID := base.GetConfigString("chatId", "")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}

	timeout := 15 * time.Second
	return &TelegramAction{
		BaseNode:  base,
		botToken:  botToken,
		chatID:    chatID,
		message:   base.GetConfigString("message", "{{payload}}"),
		parseMode: base.GetConfigString("parseMode", ""),
		timeout:   timeout,
		client:    newGuardedHTTPClient(timeout, false),
	}, nil
}

// Process renders the message and sends it via the Telegram Bot API.
func (a *TelegramAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	text := renderTemplate(a.message, msg)

	payload := map[string]interface{}{
		"chat_id": a.chatID,
		"text":    text,
	}
	if a.parseMode != "" {
		payload["parse_mode"] = a.parseMode
	}
	bodyBytes, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", a.botToken)

	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, node.Transient(fmt.Errorf("telegram request failed: %w", err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(respBody))
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("telegramStatus", resp.StatusCode)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *TelegramAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *TelegramAction) Validate() error {
	if a.botToken == "" {
		return fmt.Errorf("botToken is required")
	}
	if a.chatID == "" {
		return fmt.Errorf("chatId is required")
	}
	return nil
}

func telegramActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"botToken": {
				Type:        "string",
				Title:       "Bot Token",
				Description: "Telegram bot token from @BotFather",
			},
			"chatId": {
				Type:        "string",
				Title:       "Chat ID",
				Description: "Destination chat/channel ID",
			},
			"message": {
				Type:        "string",
				Title:       "Message",
				Description: "Message text (supports {{payload.field}} templates)",
				Default:     "{{payload}}",
			},
			"parseMode": {
				Type:        "string",
				Title:       "Parse Mode",
				Description: "Optional text formatting",
				Default:     "",
				Enum:        []string{"", "Markdown", "MarkdownV2", "HTML"},
			},
		},
		Required: []string{"botToken", "chatId"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (a *TelegramAction) GetConfigSchema() node.ConfigSchema {
	return telegramActionConfigSchema()
}

// TelegramActionInfo returns the node type info for registration.
func TelegramActionInfo() node.NodeTypeInfo {
	schema := telegramActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-telegram",
		Name:        "Telegram",
		Description: "Send a message via a Telegram bot",
		Documentation: `## Telegram

Sends a message to a Telegram chat using a bot. Create a bot with @BotFather to
get a token, and obtain the chat ID of the destination.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| botToken | string | - | Bot token (required) |
| chatId | string | - | Destination chat ID (required) |
| message | string | {{payload}} | Message text (supports templates) |
| parseMode | enum | (none) | Markdown / MarkdownV2 / HTML |

## Use Cases

- Alerting on sensor thresholds
- Notifying when a flow runs
- Sending command output to your phone
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "send",
	}
}

func init() {
	info := TelegramActionInfo()
	if err := node.Register(info, NewTelegramAction); err != nil {
		panic("failed to register telegram action: " + err.Error())
	}
}
