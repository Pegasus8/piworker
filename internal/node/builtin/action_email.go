package builtin

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// EmailAction sends an email via SMTP (STARTTLS on submission ports).
type EmailAction struct {
	*node.BaseNode

	host     string
	port     int
	username string
	password string
	from     string
	to       []string
	subject  string
	body     string
}

// NewEmailAction creates a new EmailAction from configuration.
func NewEmailAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	host := base.GetConfigString("host", "")
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	from := base.GetConfigString("from", "")
	if from == "" {
		return nil, fmt.Errorf("from is required")
	}
	to := splitRecipients(base.GetConfigString("to", ""))
	if len(to) == 0 {
		return nil, fmt.Errorf("at least one recipient (to) is required")
	}

	port := base.GetConfigInt("port", 587)
	if port <= 0 {
		port = 587
	}

	return &EmailAction{
		BaseNode: base,
		host:     host,
		port:     port,
		username: expandSecrets(base.GetConfigString("username", "")),
		password: expandSecrets(base.GetConfigString("password", "")),
		from:     from,
		to:       to,
		subject:  base.GetConfigString("subject", "PiWorker notification"),
		body:     base.GetConfigString("body", "{{payload}}"),
	}, nil
}

// Process renders the subject/body and sends the email.
func (a *EmailAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	subject := renderTemplate(a.subject, msg)
	body := renderTemplate(a.body, msg)

	var b strings.Builder
	b.WriteString("From: " + a.from + "\r\n")
	b.WriteString("To: " + strings.Join(a.to, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)

	addr := fmt.Sprintf("%s:%d", a.host, a.port)
	var auth smtp.Auth
	if a.username != "" {
		auth = smtp.PlainAuth("", a.username, a.password, a.host)
	}

	if err := smtp.SendMail(addr, auth, a.from, a.to, []byte(b.String())); err != nil {
		// SMTP failures are typically transient (greylisting, temporary outage).
		return nil, node.Transient(fmt.Errorf("failed to send email: %w", err))
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("emailTo", strings.Join(a.to, ", "))
	return []*types.Message{out}, nil
}

func splitRecipients(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Ports returns the port definitions.
func (a *EmailAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *EmailAction) Validate() error {
	if a.host == "" {
		return fmt.Errorf("host is required")
	}
	if a.from == "" {
		return fmt.Errorf("from is required")
	}
	if len(a.to) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	return nil
}

func emailActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"host":     {Type: "string", Title: "SMTP Host", Description: "SMTP server hostname"},
			"port":     {Type: "number", Title: "SMTP Port", Description: "Submission port (587 for STARTTLS)", Default: 587},
			"username": {Type: "string", Title: "Username", Description: "SMTP username (optional)", Default: ""},
			"password": {Type: "string", Title: "Password", Description: "SMTP password (optional)", Default: ""},
			"from":     {Type: "string", Title: "From", Description: "Sender address"},
			"to":       {Type: "string", Title: "To", Description: "Recipient address(es), comma-separated"},
			"subject":  {Type: "string", Title: "Subject", Description: "Subject (supports templates)", Default: "PiWorker notification"},
			"body":     {Type: "string", Title: "Body", Description: "Body (supports templates)", Default: "{{payload}}"},
		},
		Required: []string{"host", "from", "to"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (a *EmailAction) GetConfigSchema() node.ConfigSchema {
	return emailActionConfigSchema()
}

// EmailActionInfo returns the node type info for registration.
func EmailActionInfo() node.NodeTypeInfo {
	schema := emailActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-email",
		Name:        "Email",
		Description: "Send an email via SMTP",
		Documentation: `## Email

Sends an email through an SMTP server (STARTTLS on submission port 587).

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| host | string | - | SMTP host (required) |
| port | number | 587 | Submission port |
| username | string | - | SMTP username (optional) |
| password | string | - | SMTP password (optional) |
| from | string | - | Sender address (required) |
| to | string | - | Recipient(s), comma-separated (required) |
| subject | string | PiWorker notification | Subject (supports templates) |
| body | string | {{payload}} | Body (supports templates) |
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "mail",
	}
}

func init() {
	info := EmailActionInfo()
	if err := node.Register(info, NewEmailAction); err != nil {
		panic("failed to register email action: " + err.Error())
	}
}
