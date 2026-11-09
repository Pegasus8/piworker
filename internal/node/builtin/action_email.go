package builtin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

const (
	// emailDialTimeout bounds the TCP connect.
	emailDialTimeout = 15 * time.Second
	// emailSessionTimeout bounds the whole SMTP conversation when the caller's
	// context has no earlier deadline.
	emailSessionTimeout = 60 * time.Second
)

// Email transport security modes.
const (
	emailTLSAuto     = "auto"     // implicit TLS on 465, otherwise STARTTLS when offered
	emailTLSImplicit = "implicit" // TLS from the first byte (SMTPS)
	emailTLSStartTLS = "starttls" // require STARTTLS
)

// EmailAction sends an email via SMTP (implicit TLS on 465, STARTTLS
// otherwise).
type EmailAction struct {
	*node.BaseNode

	host     string
	port     int
	tlsMode  string
	username string
	password string
	from     string
	to       []string
	subject  string
	body     string

	// rootCAs overrides the system roots (tests only).
	rootCAs *x509.CertPool
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
	if strings.ContainsAny(from, "\r\n") {
		return nil, fmt.Errorf("from must not contain line breaks")
	}
	rawTo := base.GetConfigString("to", "")
	if strings.ContainsAny(rawTo, "\r\n") {
		return nil, fmt.Errorf("to must not contain line breaks")
	}
	to := splitRecipients(rawTo)
	if len(to) == 0 {
		return nil, fmt.Errorf("at least one recipient (to) is required")
	}

	port := base.GetConfigInt("port", 587)
	if port <= 0 || port > 65535 {
		port = 587
	}

	tlsMode := strings.ToLower(base.GetConfigString("tls", emailTLSAuto))
	switch tlsMode {
	case emailTLSAuto, emailTLSImplicit, emailTLSStartTLS:
	case "":
		tlsMode = emailTLSAuto
	default:
		return nil, fmt.Errorf("invalid tls mode %q (auto, implicit, starttls)", tlsMode)
	}

	return &EmailAction{
		BaseNode: base,
		host:     host,
		port:     port,
		tlsMode:  tlsMode,
		username: expandSecrets(base.GetConfigString("username", "")),
		password: expandSecrets(base.GetConfigString("password", "")),
		from:     from,
		to:       to,
		subject:  base.GetConfigString("subject", "PiWorker notification"),
		body:     base.GetConfigString("body", "{{payload}}"),
	}, nil
}

// implicitTLS reports whether the connection is TLS from the first byte.
func (a *EmailAction) implicitTLS() bool {
	return a.tlsMode == emailTLSImplicit || (a.tlsMode == emailTLSAuto && a.port == 465)
}

// sanitizeHeaderValue removes CR/LF so a rendered value cannot start a new
// header line (header injection, e.g. a templated "Bcc:").
func sanitizeHeaderValue(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' }), " ")
}

// buildEmailMessage assembles an RFC 5322 message. Header values are
// stripped of line breaks and a non-ASCII subject is RFC 2047 encoded.
func buildEmailMessage(from string, to []string, subject, body string) []byte {
	rcpts := make([]string, len(to))
	for i, r := range to {
		rcpts[i] = sanitizeHeaderValue(r)
	}
	var b strings.Builder
	b.WriteString("From: " + sanitizeHeaderValue(from) + "\r\n")
	b.WriteString("To: " + strings.Join(rcpts, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", sanitizeHeaderValue(subject)) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// Process renders the subject/body and sends the email.
func (a *EmailAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	subject := renderTemplate(a.subject, msg, ctx)
	body := renderTemplate(a.body, msg, ctx)

	if err := a.send(ctx, buildEmailMessage(a.from, a.to, subject, body)); err != nil {
		return nil, classifySMTPError(fmt.Errorf("failed to send email: %w", err))
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("emailTo", strings.Join(a.to, ", "))
	return []*types.Message{out}, nil
}

// classifySMTPError marks 4xx replies and network failures transient. 5xx
// replies (bad recipient, auth rejected, policy) are permanent: retrying them
// cannot succeed and may duplicate mail to the recipients that were accepted.
func classifySMTPError(err error) error {
	var tpErr *textproto.Error
	if errors.As(err, &tpErr) {
		if tpErr.Code >= 400 && tpErr.Code < 500 {
			return node.Transient(err)
		}
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return node.Transient(err)
	}
	return err
}

// send delivers raw over SMTP. Every network step is bounded by ctx and by
// emailSessionTimeout, so a server that never greets (e.g. an SMTPS port
// spoken to in plaintext) cannot hang the node.
func (a *EmailAction) send(ctx context.Context, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, emailSessionTimeout)
	defer cancel()

	addr := net.JoinHostPort(a.host, fmt.Sprint(a.port))
	tlsCfg := &tls.Config{ServerName: a.host, RootCAs: a.rootCAs, MinVersion: tls.VersionTLS12}

	dialer := &net.Dialer{Timeout: emailDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// Unblock any in-flight read/write as soon as ctx is cancelled.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	if a.implicitTLS() {
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = tlsConn
	}

	c, err := smtp.NewClient(conn, a.host)
	if err != nil {
		return err
	}
	defer c.Close()

	if !a.implicitTLS() {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return err
			}
		} else if a.tlsMode == emailTLSStartTLS {
			return fmt.Errorf("server does not offer STARTTLS")
		}
	}

	if a.username != "" {
		// smtp.PlainAuth refuses to send credentials over an unencrypted
		// connection to a non-localhost server.
		if err := c.Auth(smtp.PlainAuth("", a.username, a.password, a.host)); err != nil {
			return err
		}
	}
	if err := c.Mail(a.from); err != nil {
		return err
	}
	for _, rcpt := range a.to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	// The message is accepted once DATA completes; a failed QUIT must not
	// trigger a retry that would send it twice.
	_ = c.Quit()
	return nil
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
	minPort, maxPort := float64(1), float64(65535)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"host":     {Type: "string", Title: "SMTP Host", Description: "SMTP server hostname", Destination: true},
			"port":     {Type: "number", Title: "SMTP Port", Description: "Submission port (587 for STARTTLS, 465 for implicit TLS)", Default: 587, Minimum: &minPort, Maximum: &maxPort, AgentVisible: true, Destination: true},
			"tls":      {Type: "string", Title: "TLS", Description: "auto: implicit TLS on 465, STARTTLS when offered otherwise; implicit; starttls (required)", Default: "auto", Enum: []string{"auto", "implicit", "starttls"}, CaseInsensitive: true, AgentVisible: true},
			"username": {Type: "string", Title: "Username", Description: "SMTP username (optional; {{secret.NAME}} supported)", Default: ""},
			"password": {Type: "string", Title: "Password", Description: "SMTP password (optional; use {{secret.NAME}})", Default: "", Sensitive: true},
			"from":     {Type: "string", Title: "From", Description: "Sender address"},
			"to":       {Type: "string", Title: "To", Description: "Recipient address(es), comma-separated", Destination: true},
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

Sends an email through an SMTP server. Port 465 uses implicit TLS (SMTPS);
other ports upgrade with STARTTLS when the server offers it. The whole SMTP
session is bounded by a timeout (15s connect, 60s total), so an unreachable or
silent server fails the message instead of hanging the flow.

Line breaks in the rendered subject and in from/to are removed (no header
injection) and a non-ASCII subject is RFC 2047 encoded. 4xx replies and
network errors are retried as transient; 5xx replies are permanent.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| host | string | - | SMTP host (required) |
| port | number | 587 | Submission port |
| tls | enum | auto | auto, implicit or starttls (require STARTTLS) |
| username | string | - | SMTP username (optional) |
| password | string | - | SMTP password (optional) |
| from | string | - | Sender address (required) |
| to | string | - | Recipient(s), comma-separated (required) |
| subject | string | PiWorker notification | Subject (supports templates) |
| body | string | {{payload}} | Body (supports templates) |
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
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
